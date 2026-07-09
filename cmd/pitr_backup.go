package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/pitr"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

var (
	pitrBackupProfile    string
	pitrBackupJobs       int
	pitrBackupCheckpoint string
	pitrBackupNoCompress bool
	pitrBackupDryRun     bool
)

var pitrBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Create a base backup for PITR",
	Long: `Create a base backup using pg_basebackup.

The backup will be stored in the configured base backup directory.
Metadata will be extracted and saved for future restore operations.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPITRBackup(cmd.Context())
	},
}

func init() {
	pitrBackupCmd.Flags().StringVar(&pitrBackupProfile, "profile", "", "Profile name (required)")
	pitrBackupCmd.Flags().IntVar(&pitrBackupJobs, "jobs", 2, "Number of parallel jobs")
	pitrBackupCmd.Flags().StringVar(&pitrBackupCheckpoint, "checkpoint", "fast", "Checkpoint mode (fast or spread)")
	pitrBackupCmd.Flags().BoolVar(&pitrBackupNoCompress, "no-compress", false, "Disable gzip compression")
	pitrBackupCmd.Flags().BoolVar(&pitrBackupDryRun, "dry-run", false, "Show what would run without executing")

	_ = pitrBackupCmd.MarkFlagRequired("profile")

	pitrCmd.AddCommand(pitrBackupCmd)
}

func runPITRBackup(ctx context.Context) error {
	// Step 1: Load and validate PITR config
	pitrConfig, err := pitr.LoadPITRConfig(pitrBackupProfile)
	if err != nil {
		return err
	}

	// Validate directories exist
	if _, err := os.Stat(pitrConfig.ArchiveDir); os.IsNotExist(err) {
		return fmt.Errorf("archive directory does not exist: %s\nSuggestion: Run 'dbtool pitr setup --profile %s'", pitrConfig.ArchiveDir, pitrBackupProfile)
	}

	if _, err := os.Stat(pitrConfig.BaseBackupDir); os.IsNotExist(err) {
		return fmt.Errorf("base backup directory does not exist: %s\nSuggestion: Run 'dbtool pitr setup --profile %s'", pitrConfig.BaseBackupDir, pitrBackupProfile)
	}

	// Step 2: Check PostgreSQL archive_mode
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("cannot load config: %w", err)
	}

	profile, ok := cfg.GetProfile(pitrBackupProfile)
	if !ok {
		return fmt.Errorf("profile '%s' not found", pitrBackupProfile)
	}

	fmt.Printf("Checking PostgreSQL configuration...\n")

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("cannot connect to PostgreSQL: %w", err)
	}
	defer conn.Close(ctx)

	var archiveMode string
	err = conn.QueryRow(ctx, "SELECT setting FROM pg_settings WHERE name = 'archive_mode'").Scan(&archiveMode)
	if err != nil {
		return fmt.Errorf("cannot query archive_mode: %w", err)
	}

	if archiveMode == "off" {
		return fmt.Errorf("archive_mode = off, WAL archiving is not enabled\nSuggestion: Edit postgresql.conf and restart PostgreSQL\nSee: dbtool pitr setup --profile %s", pitrBackupProfile)
	}

	fmt.Printf("✓ archive_mode = %s\n", archiveMode)

	// Check if WAL archive is working
	walFiles, err := pitr.ListWALFiles(pitrConfig.ArchiveDir)
	if err != nil {
		fmt.Printf("⚠ Cannot list WAL files: %v\n", err)
	} else if len(walFiles) == 0 {
		fmt.Printf("⚠ No WAL files in archive, archive_command may not be working\n")
	} else {
		latestWAL := walFiles[len(walFiles)-1]
		age := time.Since(latestWAL.ModTime)
		if age > time.Hour {
			fmt.Printf("⚠ Latest WAL file is %v old, archive may be delayed\n", age.Round(time.Minute))
		} else {
			fmt.Printf("✓ WAL archive active (latest: %v ago)\n", age.Round(time.Minute))
		}
	}

	// Step 3: Generate backup ID
	backupID := time.Now().Format("20060102_150405")
	backupDir := filepath.Join(pitrConfig.BaseBackupDir, backupID)

	// Check for duplicate ID (retry up to 3 times)
	for i := 0; i < 3; i++ {
		if _, err := os.Stat(backupDir); os.IsNotExist(err) {
			break
		}
		if i == 2 {
			return fmt.Errorf("backup ID already exists: %s", backupID)
		}
		time.Sleep(time.Second)
		backupID = time.Now().Format("20060102_150405")
		backupDir = filepath.Join(pitrConfig.BaseBackupDir, backupID)
	}

	fmt.Printf("\nBackup ID: %s\n", backupID)

	// Step 4: Estimate backup size
	var dbSize int64
	err = conn.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&dbSize)
	if err != nil {
		fmt.Printf("⚠ Cannot estimate database size: %v\n", err)
	} else {
		estimatedSize := dbSize
		if !pitrBackupNoCompress {
			estimatedSize = int64(float64(dbSize) * 0.3) // estimate 70% compression
		}
		fmt.Printf("Database size: %s (estimated backup: %s)\n",
			formatBytes(dbSize), formatBytes(estimatedSize))
	}

	// Step 5: Run pg_basebackup
	if pitrBackupDryRun {
		fmt.Println("\n(Dry run - backup not executed)")
		return nil
	}

	fmt.Printf("\nStarting base backup...\n")

	opts := pitr.BackupOptions{
		Profile:    profile,
		OutputDir:  backupDir,
		Jobs:       pitrBackupJobs,
		Checkpoint: pitrBackupCheckpoint,
		NoCompress: pitrBackupNoCompress,
		Verbose:    Verbose,
	}

	metadata, err := pitr.RunBaseBackup(ctx, opts)
	if err != nil {
		// Cleanup on failure
		os.RemoveAll(backupDir)
		return fmt.Errorf("base backup failed: %w", err)
	}

	// Step 6: Save metadata
	if err := pitr.SaveBackupMetadata(backupDir, metadata); err != nil {
		fmt.Printf("⚠ Cannot save metadata: %v\n", err)
	}

	// Step 7: Apply retention policy
	backups, err := pitr.ListBackups(pitrConfig.BaseBackupDir)
	if err == nil && len(backups) > pitrConfig.Retention.KeepBaseBackups {
		// Sort by ID (newest first)
		sort.Slice(backups, func(i, j int) bool {
			return backups[i].ID > backups[j].ID
		})

		// Delete old backups
		for _, backup := range backups[pitrConfig.Retention.KeepBaseBackups:] {
			backupPath := filepath.Join(pitrConfig.BaseBackupDir, backup.ID)
			fmt.Printf("Removing old backup: %s\n", backup.ID)
			if err := os.RemoveAll(backupPath); err != nil {
				fmt.Printf("⚠ Cannot delete old backup: %v\n", err)
			}
		}
	}

	// Step 8: Output results
	fmt.Println("\n✓ Base backup completed")
	fmt.Printf("  ID: %s\n", metadata.ID)
	fmt.Printf("  Size: %s\n", formatBytes(metadata.Size))
	fmt.Printf("  WAL range: %s - %s\n", metadata.WALStart, metadata.WALEnd)
	fmt.Printf("  Timeline: %d\n", metadata.Timeline)
	fmt.Printf("  Duration: %ds\n", metadata.Duration)
	if metadata.Compressed {
		fmt.Printf("  Format: %s (compressed)\n", metadata.Format)
	} else {
		fmt.Printf("  Format: %s\n", metadata.Format)
	}

	fmt.Println("\nRecovery range:")
	fmt.Printf("  From: %s\n", metadata.StartTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("  To: (latest WAL in archive)\n")

	return nil
}
