package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/pitr"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

var (
	pitrSetupProfile         string
	pitrSetupArchiveDir      string
	pitrSetupBaseBackupDir   string
	pitrSetupRetentionBackup int
	pitrSetupRetentionDays   int
	pitrSetupDryRun          bool
)

var pitrSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure WAL archiving for a profile",
	Long: `Configure Point-in-Time Recovery for a PostgreSQL profile.

This command will:
  1. Validate the profile and PostgreSQL connection
  2. Check PostgreSQL WAL configuration
  3. Create directories for WAL archive and base backups
  4. Generate archive_command for postgresql.conf
  5. Provide instructions for enabling PITR`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPITRSetup(cmd.Context())
	},
}

func init() {
	pitrSetupCmd.Flags().StringVar(&pitrSetupProfile, "profile", "", "Profile name (required)")
	pitrSetupCmd.Flags().StringVar(&pitrSetupArchiveDir, "archive-dir", "", "WAL archive directory (default: ~/.config/dbtool/pitr/<profile>/wal)")
	pitrSetupCmd.Flags().StringVar(&pitrSetupBaseBackupDir, "base-backup-dir", "", "Base backup directory (default: ~/.config/dbtool/pitr/<profile>/base)")
	pitrSetupCmd.Flags().IntVar(&pitrSetupRetentionBackup, "retention-backups", 3, "Number of base backups to keep")
	pitrSetupCmd.Flags().IntVar(&pitrSetupRetentionDays, "retention-days", 7, "Number of days to keep WAL files")
	pitrSetupCmd.Flags().BoolVar(&pitrSetupDryRun, "dry-run", false, "Show what would be configured without making changes")

	_ = pitrSetupCmd.MarkFlagRequired("profile")

	pitrCmd.AddCommand(pitrSetupCmd)
}

func runPITRSetup(ctx context.Context) error {
	// Step 1: Load and validate profile
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("cannot load config: %w", err)
	}

	profile, ok := cfg.GetProfile(pitrSetupProfile)
	if !ok {
		return fmt.Errorf("profile '%s' not found", pitrSetupProfile)
	}

	if profile.Driver != "postgres" {
		return fmt.Errorf("PITR only supports PostgreSQL (profile driver: %s)", profile.Driver)
	}

	// Step 2: Connect to PostgreSQL and check configuration
	fmt.Printf("Connecting to PostgreSQL at %s:%d...\n", profile.Host, profile.Port)

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("cannot connect to PostgreSQL: %w\nSuggestion: Check host/port/user/password in profile", err)
	}
	defer conn.Close(ctx)

	// Query PostgreSQL settings
	query := `SELECT name, setting FROM pg_settings WHERE name IN ('wal_level', 'archive_mode', 'archive_command', 'data_directory')`
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("cannot query pg_settings: %w", err)
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var name, setting string
		if err := rows.Scan(&name, &setting); err != nil {
			continue
		}
		settings[name] = setting
	}

	walLevel := settings["wal_level"]
	archiveMode := settings["archive_mode"]
	archiveCommand := settings["archive_command"]
	dataDirectory := settings["data_directory"]

	// Step 3: Create PITR directories
	profilePITRDir, err := pitr.GetProfilePITRDir(pitrSetupProfile)
	if err != nil {
		return err
	}

	archiveDir := pitrSetupArchiveDir
	if archiveDir == "" {
		archiveDir = filepath.Join(profilePITRDir, "wal")
	}

	baseBackupDir := pitrSetupBaseBackupDir
	if baseBackupDir == "" {
		baseBackupDir = filepath.Join(profilePITRDir, "base")
	}

	for _, dir := range []string{profilePITRDir, archiveDir, baseBackupDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("cannot create directory %s: %w", dir, err)
		}
	}

	testFile := filepath.Join(archiveDir, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("archive directory is not writable: %w", err)
	}
	os.Remove(testFile)

	fmt.Printf("✓ Archive directory: %s\n", archiveDir)
	fmt.Printf("✓ Base backup directory: %s\n", baseBackupDir)

	// Step 4: Generate archive_command
	var generatedArchiveCommand string
	if runtime.GOOS == "windows" {
		generatedArchiveCommand = fmt.Sprintf(`copy "%%p" "%s\%%f"`, archiveDir)
	} else {
		generatedArchiveCommand = fmt.Sprintf("cp %%p %s/%%f", archiveDir)
	}

	// Step 5: Dry run
	if pitrSetupDryRun {
		fmt.Println("\n\033[1;34mℹ Dry run mode — no changes will be made.\033[0m\n")
		fmt.Println("Current PostgreSQL settings:")
		fmt.Printf("  wal_level = %s\n", walLevel)
		fmt.Printf("  archive_mode = %s\n", archiveMode)
		if archiveCommand == "" {
			fmt.Printf("  archive_command = (not set)\n")
		} else {
			fmt.Printf("  archive_command = %s\n", archiveCommand)
		}
		fmt.Println("\nChanges that would be applied:")

		if walLevel == "minimal" {
			fmt.Println("  ALTER SYSTEM SET wal_level = 'replica'")
		} else {
			fmt.Printf("  wal_level = %s (already OK)\n", walLevel)
		}

		if archiveMode == "off" || archiveMode == "" {
			fmt.Println("  ALTER SYSTEM SET archive_mode = 'on'")
		} else {
			fmt.Printf("  archive_mode = %s (already OK)\n", archiveMode)
		}

		fmt.Printf("  ALTER SYSTEM SET archive_command = '%s'\n", generatedArchiveCommand)
		fmt.Println("\n(Dry run — configuration not saved)")
		return nil
	}

	// Step 6: Apply configuration via ALTER SYSTEM SET
	needRestart := false
	changes := 0

	fmt.Println("\nConfiguring PostgreSQL...")

	// wal_level
	if walLevel == "minimal" {
		fmt.Printf("  Setting wal_level = replica...\n")
		_, err = conn.Exec(ctx, "ALTER SYSTEM SET wal_level = 'replica'")
		if err != nil {
			return fmt.Errorf("cannot set wal_level: %w", err)
		}
		needRestart = true
		changes++
	} else {
		fmt.Printf("  ✓ wal_level = %s (already OK)\n", walLevel)
	}

	// archive_mode
	if archiveMode == "off" || archiveMode == "" {
		fmt.Printf("  Setting archive_mode = on...\n")
		_, err = conn.Exec(ctx, "ALTER SYSTEM SET archive_mode = 'on'")
		if err != nil {
			return fmt.Errorf("cannot set archive_mode: %w", err)
		}
		needRestart = true
		changes++
	} else {
		fmt.Printf("  ✓ archive_mode = %s (already OK)\n", archiveMode)
	}

	// archive_command
	archiveCommandChanged := archiveCommand == "" || archiveCommand == "(disabled)" || archiveCommand != generatedArchiveCommand
	if archiveCommandChanged {
		fmt.Printf("  Setting archive_command...\n")
		_, err = conn.Exec(ctx, "ALTER SYSTEM SET archive_command = $1", generatedArchiveCommand)
		if err != nil {
			return fmt.Errorf("cannot set archive_command: %w", err)
		}
		changes++
	} else {
		fmt.Printf("  ✓ archive_command (already configured)\n")
	}

	// Reload config (archive_command takes effect on reload)
	if !needRestart {
		_, err = conn.Exec(ctx, "SELECT pg_reload_conf()")
		if err != nil {
			fmt.Printf("  ⚠ Warning: could not reload PostgreSQL config: %v\n", err)
		} else {
			fmt.Println("  ✓ PostgreSQL configuration reloaded")
		}
	}

	// Step 7: Report results
	fmt.Println()
	if changes == 0 {
		fmt.Println("✓ PITR is already configured for profile", pitrSetupProfile)
	} else {
		fmt.Printf("✓ Applied %d configuration change(s) via ALTER SYSTEM SET\n", changes)
	}

	if needRestart {
		fmt.Println()
		fmt.Println("\033[1;33m⚠ wal_level and/or archive_mode changed — PostgreSQL restart required.\033[0m")
		fmt.Println()
		fmt.Printf("  Data directory: %s\n", dataDirectory)
		fmt.Println()
		fmt.Println("  Restart PostgreSQL:")
		fmt.Println("    Linux:   sudo systemctl restart postgresql")
		fmt.Println("    macOS:   brew services restart postgresql")
		fmt.Println("    Windows: net stop postgresql-x64-15 && net start postgresql-x64-15")
		fmt.Println()
		fmt.Println("  After restart, verify:")
		fmt.Printf("    dbtool pitr status --profile %s\n", pitrSetupProfile)
		fmt.Println()
		fmt.Println("  Then create your first base backup:")
		fmt.Printf("    dbtool pitr backup --profile %s\n", pitrSetupProfile)
	} else {
		fmt.Println()
		fmt.Println("✓ No restart needed — archive_command applied via pg_reload_conf()")
		fmt.Println()
		fmt.Println("Next step — create your first base backup:")
		fmt.Printf("  dbtool pitr backup --profile %s\n", pitrSetupProfile)
	}

	// Step 8: Save PITR config
	pitrConfig := &pitr.PITRConfig{
		ProfileName:   pitrSetupProfile,
		Driver:        profile.Driver,
		ArchiveDir:    archiveDir,
		BaseBackupDir: baseBackupDir,
		Retention: pitr.RetentionPolicy{
			KeepBaseBackups: pitrSetupRetentionBackup,
			KeepWALDays:     pitrSetupRetentionDays,
		},
		CreatedAt: time.Now(),
	}

	if err := pitr.SavePITRConfig(pitrConfig); err != nil {
		return fmt.Errorf("cannot save PITR config: %w", err)
	}

	fmt.Printf("\n✓ PITR configuration saved to %s\n", filepath.Join(profilePITRDir, "config.yaml"))
	return nil
}
