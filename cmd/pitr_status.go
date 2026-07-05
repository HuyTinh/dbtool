package cmd

import (
	"context"
	"fmt"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/pitr"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

var pitrStatusProfile string

var pitrStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display PITR status, backups, and WAL archive information",
	Long: `Display the current PITR status for a profile, including:
- PostgreSQL archive_mode status
- Base backup list with metadata
- WAL archive statistics (count, size, coverage)
- Recovery range (earliest to latest point)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ExecutePITRStatus(cmd.Context(), pitrStatusProfile)
	},
}

func ExecutePITRStatus(ctx context.Context, profileName string) error {
	// 1. Load PITR configuration
	pitrConfig, err := pitr.LoadPITRConfig(profileName)
	if err != nil {
		return err
	}

	// 2. Load main profile configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("cannot load config: %w", err)
	}

	profile, ok := cfg.GetProfile(profileName)
	if !ok {
		return fmt.Errorf("profile %q not found", profileName)
	}

	// 3. Check PostgreSQL archive settings
	fmt.Printf("\033[1;36m=== PITR Status for Profile: %s ===\033[0m\n\n", profileName)

	archiveInfo := getArchiveInfo(ctx, profile)
	fmt.Printf("\033[1;33mPostgreSQL Configuration:\033[0m\n")
	fmt.Printf("  %-20s %s\n", "Archive Mode:", archiveInfo.ArchiveMode)
	fmt.Printf("  %-20s %s\n", "WAL Level:", archiveInfo.WALLevel)
	fmt.Printf("  %-20s %s\n", "Archive Command:", archiveInfo.ArchiveCommand)
	if archiveInfo.ArchiveMode == "off" {
		fmt.Printf("\n\033[1;31m⚠ Warning: Archive mode is OFF. Run 'dbtool pitr setup --profile %s' to configure.\033[0m\n", profileName)
	}
	fmt.Println()

	// 4. List base backups
	fmt.Printf("\033[1;33mBase Backups:\033[0m\n")
	backups, err := pitr.ListBackups(pitrConfig.BaseBackupDir)
	if err != nil {
		fmt.Printf("  Cannot list backups: %v\n", err)
	} else if len(backups) == 0 {
		fmt.Printf("  No base backups found. Run 'dbtool pitr backup --profile %s' to create one.\n", profileName)
	} else {
		for i, backup := range backups {
			age := time.Since(backup.EndTime)
			ageStr := formatAge(age)
			fmt.Printf("  [%d] %s\n", i+1, backup.ID)
			fmt.Printf("      Time:       %s (%s ago)\n", backup.EndTime.Format("2006-01-02 15:04:05"), ageStr)
			fmt.Printf("      Timeline:   %d\n", backup.Timeline)
			fmt.Printf("      Size:       %s\n", formatBytes(backup.Size))
			fmt.Printf("      WAL Range:  %s → %s\n", backup.WALStart, backup.WALEnd)
			fmt.Printf("      Duration:   %ds\n", backup.Duration)
			fmt.Println()
		}
	}

	// 5. WAL archive statistics
	fmt.Printf("\033[1;33mWAL Archive:\033[0m\n")
	walFiles, err := pitr.ListWALFiles(pitrConfig.ArchiveDir)
	if err != nil {
		fmt.Printf("  Cannot list WAL files: %v\n", err)
	} else if len(walFiles) == 0 {
		fmt.Printf("  No WAL files archived yet.\n")
	} else {
		var totalSize int64
		for _, wal := range walFiles {
			totalSize += wal.Size
		}

		firstWAL := walFiles[0]
		lastWAL := walFiles[len(walFiles)-1]
		coverageAge := time.Since(lastWAL.ModTime)

		fmt.Printf("  %-20s %d files\n", "Total Files:", len(walFiles))
		fmt.Printf("  %-20s %s\n", "Total Size:", formatBytes(totalSize))
		fmt.Printf("  %-20s %s (timeline %d)\n", "First WAL:", firstWAL.Filename, firstWAL.Timeline)
		fmt.Printf("  %-20s %s (timeline %d)\n", "Last WAL:", lastWAL.Filename, lastWAL.Timeline)
		fmt.Printf("  %-20s %s ago\n", "Last Archived:", formatAge(coverageAge))
		fmt.Println()

		// 6. Calculate recovery range
		fmt.Printf("\033[1;33mRecovery Range:\033[0m\n")
		if len(backups) > 0 {
			oldestBackup := backups[0]
			newestBackup := backups[len(backups)-1]

			fmt.Printf("  Earliest Point:   %s (backup: %s)\n", oldestBackup.StartTime.Format("2006-01-02 15:04:05"), oldestBackup.ID)
			fmt.Printf("  Latest Point:     %s (last WAL)\n", lastWAL.ModTime.Format("2006-01-02 15:04:05"))
			fmt.Printf("  Coverage:         %s\n", formatAge(time.Since(oldestBackup.StartTime)))
			fmt.Println()

			// Show example restore commands
			fmt.Printf("\033[1;33mExample Restore Commands:\033[0m\n")
			fmt.Printf("  # Restore to latest point:\n")
			fmt.Printf("  dbtool pitr restore --profile %s --to '%s'\n\n", profileName, lastWAL.ModTime.Format("2006-01-02 15:04:05"))
			fmt.Printf("  # Restore to specific backup time:\n")
			fmt.Printf("  dbtool pitr restore --profile %s --to '%s'\n", profileName, newestBackup.EndTime.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("  Cannot determine recovery range (no base backups)\n")
		}
	}

	fmt.Println()
	return nil
}

// ArchiveInfo holds PostgreSQL archive configuration
type ArchiveInfo struct {
	ArchiveMode    string
	WALLevel       string
	ArchiveCommand string
}

// getArchiveInfo retrieves archive configuration from PostgreSQL
func getArchiveInfo(ctx context.Context, profile config.Profile) ArchiveInfo {
	info := ArchiveInfo{
		ArchiveMode:    "unknown",
		WALLevel:       "unknown",
		ArchiveCommand: "unknown",
	}

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?connect_timeout=3",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database,
	)

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return info
	}
	defer conn.Close(ctx)

	var archiveMode, walLevel, archiveCommand string
	err = conn.QueryRow(ctx, "SHOW archive_mode").Scan(&archiveMode)
	if err == nil {
		info.ArchiveMode = archiveMode
	}

	err = conn.QueryRow(ctx, "SHOW wal_level").Scan(&walLevel)
	if err == nil {
		info.WALLevel = walLevel
	}

	err = conn.QueryRow(ctx, "SHOW archive_command").Scan(&archiveCommand)
	if err == nil {
		if archiveCommand == "" {
			info.ArchiveCommand = "(not set)"
		} else {
			info.ArchiveCommand = archiveCommand
		}
	}

	return info
}

// formatAge formats a duration into a human-readable string
func formatAge(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", minutes)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func init() {
	pitrStatusCmd.Flags().StringVar(&pitrStatusProfile, "profile", "", "Profile name")
	_ = pitrStatusCmd.MarkFlagRequired("profile")

	pitrCmd.AddCommand(pitrStatusCmd)
}
