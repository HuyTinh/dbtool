package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/pitr"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

var (
	pitrRestoreProfile         string
	pitrRestoreTo              string
	pitrRestoreTargetProfile   string
	pitrRestoreTimeline        int
	pitrRestoreCreateIfMissing bool
	pitrRestoreClean           bool
	pitrRestoreDryRun          bool
)

var pitrRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore database to a specific point in time",
	Long: `Restore a PostgreSQL database to a specific point in time using PITR.

This command will:
  1. Find a suitable base backup before the target time
  2. Validate WAL archive coverage
  3. Stop PostgreSQL (if running)
  4. Extract base backup and configure recovery
  5. Start PostgreSQL and monitor recovery progress
  6. Verify recovery completion`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPITRRestore(cmd.Context())
	},
}

func init() {
	pitrRestoreCmd.Flags().StringVar(&pitrRestoreProfile, "profile", "", "Source profile with PITR configured (required)")
	pitrRestoreCmd.Flags().StringVar(&pitrRestoreTo, "to", "", "Target timestamp (required, format: '2024-01-15 14:30:00')")
	pitrRestoreCmd.Flags().StringVar(&pitrRestoreTargetProfile, "target-profile", "", "Target profile (default: same as source)")
	pitrRestoreCmd.Flags().IntVar(&pitrRestoreTimeline, "timeline", 0, "Timeline ID (default: latest)")
	pitrRestoreCmd.Flags().BoolVar(&pitrRestoreCreateIfMissing, "create-if-missing", false, "Create target database if it doesn't exist")
	pitrRestoreCmd.Flags().BoolVar(&pitrRestoreClean, "clean", false, "Drop objects before restore")
	pitrRestoreCmd.Flags().BoolVar(&pitrRestoreDryRun, "dry-run", false, "Show recovery plan without executing")

	_ = pitrRestoreCmd.MarkFlagRequired("profile")
	_ = pitrRestoreCmd.MarkFlagRequired("to")

	pitrCmd.AddCommand(pitrRestoreCmd)
}

func runPITRRestore(ctx context.Context) error {
	// Step 1: Parse and validate input
	targetTime, err := parseTimestamp(pitrRestoreTo)
	if err != nil {
		return fmt.Errorf("invalid timestamp format: %s\nSuggestion: Use format '2024-01-15 14:30:00'", pitrRestoreTo)
	}

	if targetTime.After(time.Now()) {
		return fmt.Errorf("target time is in the future: %s", targetTime.Format("2006-01-02 15:04:05"))
	}

	if targetTime.Before(time.Now().AddDate(-10, 0, 0)) {
		fmt.Printf("⚠ Target time is very old (> 10 years), please verify\n")
	}

	// Load PITR config
	pitrConfig, err := pitr.LoadPITRConfig(pitrRestoreProfile)
	if err != nil {
		return err
	}

	// Step 2: Find suitable base backup
	fmt.Printf("Searching for base backup before %s...\n", targetTime.Format("2006-01-02 15:04:05"))

	backups, err := pitr.ListBackups(pitrConfig.BaseBackupDir)
	if err != nil {
		return fmt.Errorf("cannot list backups: %w", err)
	}

	if len(backups) == 0 {
		return fmt.Errorf("no base backups found\nSuggestion: Run 'dbtool pitr backup --profile %s'", pitrRestoreProfile)
	}

	// Sort by start time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].StartTime.After(backups[j].StartTime)
	})

	// Find the most recent backup before target time
	var selectedBackup *pitr.BaseBackupMetadata
	for _, backup := range backups {
		if !backup.StartTime.After(targetTime) {
			selectedBackup = backup
			break
		}
	}

	if selectedBackup == nil {
		fmt.Printf("\nNo backup found before %s\n", targetTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("\nAvailable backups:\n")
		for _, backup := range backups {
			fmt.Printf("  %s - %s\n", backup.ID, backup.StartTime.Format("2006-01-02 15:04:05"))
		}
		return fmt.Errorf("no suitable backup found\nSuggestion: Run a new backup or choose a later target time")
	}

	fmt.Printf("✓ Selected backup: %s (%s)\n", selectedBackup.ID, selectedBackup.StartTime.Format("2006-01-02 15:04:05"))

	// Validate WAL range
	fmt.Printf("Validating WAL archive coverage...\n")
	walFiles, err := pitr.ListWALFiles(pitrConfig.ArchiveDir)
	if err != nil {
		return fmt.Errorf("cannot list WAL files: %w", err)
	}

	// Find WAL files in range
	walInRange, err := pitr.FindWALFilesInRange(walFiles, selectedBackup.WALStart, selectedBackup.WALEnd, selectedBackup.Timeline)
	if err != nil {
		return fmt.Errorf("cannot find WAL files: %w", err)
	}

	if len(walInRange) == 0 {
		return fmt.Errorf("no WAL files found for range %s - %s\nSuggestion: Check WAL archive or run a new backup",
			selectedBackup.WALStart, selectedBackup.WALEnd)
	}

	// Calculate total WAL size
	var walSize int64
	for _, wal := range walInRange {
		walSize += wal.Size
	}

	fmt.Printf("✓ WAL files: %d files (%s)\n", len(walInRange), formatBytes(walSize))

	// Step 3: Determine target profile
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("cannot load config: %w", err)
	}

	sourceProfile, ok := cfg.GetProfile(pitrRestoreProfile)
	if !ok {
		return fmt.Errorf("source profile '%s' not found", pitrRestoreProfile)
	}

	targetProfileName := pitrRestoreTargetProfile
	if targetProfileName == "" {
		targetProfileName = pitrRestoreProfile
		fmt.Printf("\n⚠ Will OVERWRITE database for profile '%s'\n", pitrRestoreProfile)
		if !confirmPrompt("Continue?") {
			fmt.Println("Cancelled")
			return nil
		}
	}

	targetProfile, ok := cfg.GetProfile(targetProfileName)
	if !ok {
		return fmt.Errorf("target profile '%s' not found", targetProfileName)
	}

	if targetProfile.Driver != sourceProfile.Driver {
		return fmt.Errorf("cross-driver PITR not supported (source: %s, target: %s)",
			sourceProfile.Driver, targetProfile.Driver)
	}

	if targetProfile.Runtime != nil && targetProfile.Runtime.Type == "docker" {
		return fmt.Errorf("PITR restore into Docker PostgreSQL is not supported yet: dbtool still manages the data directory and service lifecycle via the local host filesystem/service manager. PITR setup now supports Docker archive_command generation, but restore still requires a local/native PostgreSQL target")
	}

	if targetProfileName == pitrRestoreProfile {
		// Already confirmed above
	} else if targetProfileName != pitrRestoreProfile {
		fmt.Printf("Target profile: %s\n", targetProfileName)
	}

	// Step 4: Safety check on target
	fmt.Printf("\nConnecting to target database...\n")
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		targetProfile.User, targetProfile.Password, targetProfile.Host, targetProfile.Port, targetProfile.Database)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		if pitrRestoreCreateIfMissing {
			fmt.Printf("Target database doesn't exist, creating...\n")
			// TODO: Implement EnsureDatabaseExists
			return fmt.Errorf("create-if-missing not yet implemented")
		}
		return fmt.Errorf("cannot connect to target database: %w\nSuggestion: Use --create-if-missing to create the database", err)
	}
	defer conn.Close(ctx)

	// Check if database has data
	var tableCount int
	err = conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema')`).Scan(&tableCount)
	if err != nil {
		return fmt.Errorf("cannot check database content: %w", err)
	}

	if tableCount > 0 {
		if pitrRestoreClean {
			fmt.Printf("\n⚠ --clean will DROP all %d objects in target database\n", tableCount)
			if !confirmPrompt("Continue?") {
				fmt.Println("Cancelled")
				return nil
			}
		} else {
			fmt.Printf("\n⚠ Target database has %d tables\n", tableCount)
			if !confirmPrompt("Overwrite?") {
				fmt.Println("Cancelled")
				return nil
			}
		}
	}

	// Step 5: Display recovery plan
	estimatedTime := int(float64(selectedBackup.Size+walSize)/(50*1024*1024)) + 30 // 50MB/s + overhead

	fmt.Println("\nRecovery Plan:")
	fmt.Printf("  Source profile: %s\n", pitrRestoreProfile)
	fmt.Printf("  Target profile: %s\n", targetProfileName)
	fmt.Printf("  Base backup: %s (%s)\n", selectedBackup.ID, selectedBackup.StartTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Target time: %s\n", targetTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Timeline: %d\n", selectedBackup.Timeline)
	fmt.Printf("\n  Base backup size: %s\n", formatBytes(selectedBackup.Size))
	fmt.Printf("  WAL files to apply: %d files (%s)\n", len(walInRange), formatBytes(walSize))
	fmt.Printf("  Estimated time: ~%d seconds\n", estimatedTime)
	fmt.Println("\n⚠ This will OVERWRITE target database")

	if pitrRestoreDryRun {
		fmt.Println("\n(Dry run - recovery not executed)")
		return nil
	}

	if !confirmPrompt("Continue?") {
		fmt.Println("Cancelled")
		return nil
	}

	// Step 6: Stop PostgreSQL (if running)
	fmt.Printf("\nStopping PostgreSQL...\n")
	if err := stopPostgreSQL(ctx, targetProfile); err != nil {
		return fmt.Errorf("cannot stop PostgreSQL: %w", err)
	}

	// Wait for PostgreSQL to stop
	time.Sleep(5 * time.Second)

	// Step 7: Get target data directory
	dataDir, err := getDataDirectory(ctx, targetProfile)
	if err != nil {
		return fmt.Errorf("cannot get data directory: %w", err)
	}

	// Backup current data directory
	backupDataDir := dataDir + ".backup_" + time.Now().Format("20060102_150405")
	fmt.Printf("Backing up data directory: %s → %s\n", dataDir, backupDataDir)
	if err := os.Rename(dataDir, backupDataDir); err != nil {
		return fmt.Errorf("cannot backup data directory: %w", err)
	}

	// Step 8: Extract base backup
	fmt.Printf("Extracting base backup...\n")
	if err := extractBaseBackup(selectedBackup.ID, pitrConfig.BaseBackupDir, dataDir, selectedBackup.Compressed); err != nil {
		// Restore backup
		os.RemoveAll(dataDir)
		os.Rename(backupDataDir, dataDir)
		return fmt.Errorf("cannot extract base backup: %w", err)
	}

	// Step 9: Configure recovery
	fmt.Printf("Configuring recovery...\n")
	if err := configureRecovery(targetProfile, dataDir, pitrConfig.ArchiveDir, targetTime, selectedBackup.Timeline); err != nil {
		// Restore backup
		os.RemoveAll(dataDir)
		os.Rename(backupDataDir, dataDir)
		return fmt.Errorf("cannot configure recovery: %w", err)
	}

	// Step 10: Start PostgreSQL
	fmt.Printf("Starting PostgreSQL...\n")
	if err := startPostgreSQL(ctx, targetProfile); err != nil {
		return fmt.Errorf("cannot start PostgreSQL: %w", err)
	}

	// Step 11: Monitor recovery
	fmt.Printf("Monitoring recovery progress...\n")
	if err := monitorRecovery(ctx, targetProfile, targetTime); err != nil {
		return fmt.Errorf("recovery failed: %w", err)
	}

	// Step 12: Cleanup
	fmt.Printf("Cleaning up backup data directory...\n")
	os.RemoveAll(backupDataDir)

	fmt.Println("\n✓ PITR recovery completed")
	fmt.Printf("  Target time: %s\n", targetTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Timeline: %d\n", selectedBackup.Timeline)
	fmt.Printf("  Database: %s\n", targetProfile.Database)
	fmt.Println("\nVerify data:")
	fmt.Printf("  psql -h %s -p %d -U %s -d %s\n",
		targetProfile.Host, targetProfile.Port, targetProfile.User, targetProfile.Database)

	return nil
}

// parseTimestamp tries multiple timestamp formats
func parseTimestamp(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse timestamp")
}

// confirmPrompt asks for user confirmation
func confirmPrompt(message string) bool {
	fmt.Printf("%s [y/N]: ", message)
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// stopPostgreSQL stops the PostgreSQL service
func stopPostgreSQL(ctx context.Context, profile config.Profile) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.CommandContext(ctx, "sudo", "systemctl", "stop", "postgresql")
	case "darwin":
		cmd = exec.CommandContext(ctx, "brew", "services", "stop", "postgresql")
	case "windows":
		cmd = exec.CommandContext(ctx, "net", "stop", "postgresql-x64-15")
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	if Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}

// startPostgreSQL starts the PostgreSQL service
func startPostgreSQL(ctx context.Context, profile config.Profile) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.CommandContext(ctx, "sudo", "systemctl", "start", "postgresql")
	case "darwin":
		cmd = exec.CommandContext(ctx, "brew", "services", "start", "postgresql")
	case "windows":
		cmd = exec.CommandContext(ctx, "net", "start", "postgresql-x64-15")
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	if Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}

// getDataDirectory gets the PostgreSQL data directory
func getDataDirectory(ctx context.Context, profile config.Profile) (string, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)

	var dataDir string
	err = conn.QueryRow(ctx, "SELECT setting FROM pg_settings WHERE name = 'data_directory'").Scan(&dataDir)
	return dataDir, err
}

// extractBaseBackup extracts the base backup to the data directory
func extractBaseBackup(backupID, baseBackupDir, dataDir string, compressed bool) error {
	baseFile := filepath.Join(baseBackupDir, backupID, "base.tar")
	if compressed {
		baseFile = baseFile + ".gz"
	}

	// Create data directory
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}

	// Extract tar
	var cmd *exec.Cmd
	if compressed {
		cmd = exec.Command("sh", "-c", fmt.Sprintf("gunzip -c %s | tar -xf - -C %s", baseFile, dataDir))
	} else {
		cmd = exec.Command("tar", "-xf", baseFile, "-C", dataDir)
	}

	if Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}

// configureRecovery configures PostgreSQL recovery settings
func configureRecovery(profile config.Profile, dataDir, archiveDir string, targetTime time.Time, timeline int) error {
	// Detect PostgreSQL version (simplified - assume >= 12)
	// In production, you'd query pg_ctl --version

	// Create recovery.signal file
	signalFile := filepath.Join(dataDir, "recovery.signal")
	if err := os.WriteFile(signalFile, []byte(""), 0644); err != nil {
		return err
	}

	// Generate restore_command for the PostgreSQL runtime
	restoreCommand, _, err := pitr.BuildRestoreCommand(profile, archiveDir)
	if err != nil {
		return err
	}

	// Append to postgresql.auto.conf
	autoConfPath := filepath.Join(dataDir, "postgresql.auto.conf")
	f, err := os.OpenFile(autoConfPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	config := fmt.Sprintf(`
# PITR Recovery Configuration
restore_command = '%s'
recovery_target_time = '%s'
recovery_target_action = 'promote'
`, restoreCommand, targetTime.Format("2006-01-02 15:04:05"))

	_, err = f.WriteString(config)
	return err
}

// monitorRecovery monitors the recovery progress
func monitorRecovery(ctx context.Context, profile config.Profile, targetTime time.Time) error {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database)

	// Wait for PostgreSQL to be ready
	fmt.Printf("Waiting for PostgreSQL to start...\n")
	var conn *pgx.Conn
	var err error
	for i := 0; i < 30; i++ { // 60 seconds timeout
		conn, err = pgx.Connect(ctx, dsn)
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		return fmt.Errorf("PostgreSQL not ready after 60s: %w", err)
	}
	defer conn.Close(ctx)

	// Monitor recovery
	fmt.Printf("Recovery in progress...\n")
	timeout := time.After(30 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("recovery timeout (> 30 minutes)")
		case <-ticker.C:
			var inRecovery bool
			err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery)
			if err != nil {
				continue
			}

			if !inRecovery {
				fmt.Printf("✓ Recovery completed\n")
				return nil
			}
		}
	}
}
