package cmd

import (
	"fmt"
	"os"
	"sort"
	"time"

	"dbtool/internal/pitr"

	"github.com/spf13/cobra"
)

var (
	pitrCleanupProfile string
	pitrCleanupDryRun  bool
	pitrCleanupForce   bool
)

var pitrCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove old backups and WAL files according to retention policy",
	Long: `Remove old base backups and WAL archive files based on the configured retention policy.

Retention policy:
- Keep the N most recent base backups (keep_base_backups)
- Delete WAL files older than keep_wal_days

Examples:
  # Preview what would be cleaned up
  dbtool pitr cleanup --profile mydb --dry-run

  # Run cleanup with confirmation prompt
  dbtool pitr cleanup --profile mydb

  # Run cleanup without confirmation
  dbtool pitr cleanup --profile mydb --force`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ExecutePITRCleanup(pitrCleanupProfile, pitrCleanupDryRun, pitrCleanupForce)
	},
}

func ExecutePITRCleanup(profileName string, dryRun, force bool) error {
	// 1. Load PITR configuration
	pitrConfig, err := pitr.LoadPITRConfig(profileName)
	if err != nil {
		return err
	}

	// 2. List base backups
	backups, err := pitr.ListBackups(pitrConfig.BaseBackupDir)
	if err != nil {
		return fmt.Errorf("cannot list backups: %w", err)
	}

	// 3. Sort backups by start time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].StartTime.After(backups[j].StartTime)
	})

	// 4. Determine which backups to delete
	var backupsToDelete []*pitr.BaseBackupMetadata
	if len(backups) > pitrConfig.Retention.KeepBaseBackups {
		backupsToDelete = backups[pitrConfig.Retention.KeepBaseBackups:]
	}

	// 5. List WAL files and determine which to delete
	walFiles, err := pitr.ListWALFiles(pitrConfig.ArchiveDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot list WAL files: %w", err)
	}

	cutoffTime := time.Now().AddDate(0, 0, -pitrConfig.Retention.KeepWALDays)
	var walFilesToDelete []*pitr.WALFileInfo
	for _, wal := range walFiles {
		if wal.ModTime.Before(cutoffTime) {
			walFilesToDelete = append(walFilesToDelete, wal)
		}
	}

	// 6. Report findings
	fmt.Printf("\033[1;36m=== PITR Cleanup for Profile: %s ===\033[0m\n\n", profileName)
	fmt.Printf("\033[1;33mRetention Policy:\033[0m\n")
	fmt.Printf("  Keep base backups:  %d\n", pitrConfig.Retention.KeepBaseBackups)
	fmt.Printf("  Keep WAL days:      %d\n\n", pitrConfig.Retention.KeepWALDays)

	fmt.Printf("\033[1;33mBase Backups:\033[0m\n")
	if len(backups) == 0 {
		fmt.Printf("  No base backups found.\n")
	} else {
		fmt.Printf("  Total: %d (keeping %d, deleting %d)\n", len(backups), len(backups)-len(backupsToDelete), len(backupsToDelete))
		if len(backupsToDelete) > 0 {
			fmt.Printf("\n  \033[1;31mBackups to DELETE:\033[0m\n")
			for _, backup := range backupsToDelete {
				age := time.Since(backup.EndTime)
				fmt.Printf("    - %s (%s old, %s)\n", backup.ID, formatAge(age), formatBytes(backup.Size))
			}
		}
	}
	fmt.Println()

	fmt.Printf("\033[1;33mWAL Archive:\033[0m\n")
	if len(walFiles) == 0 {
		fmt.Printf("  No WAL files found.\n")
	} else {
		fmt.Printf("  Total: %d (deleting %d older than %d days)\n", len(walFiles), len(walFilesToDelete), pitrConfig.Retention.KeepWALDays)
		if len(walFilesToDelete) > 0 {
			var totalWALSize int64
			for _, wal := range walFilesToDelete {
				totalWALSize += wal.Size
			}
			fmt.Printf("  Space to reclaim: %s\n", formatBytes(totalWALSize))
		}
	}
	fmt.Println()

	// 7. Check if there's anything to clean up
	if len(backupsToDelete) == 0 && len(walFilesToDelete) == 0 {
		fmt.Printf("\033[1;32m✓ Nothing to clean up.\033[0m\n")
		return nil
	}

	// 8. Dry run mode
	if dryRun {
		fmt.Printf("\033[1;34mℹ Dry run mode - no files will be deleted.\033[0m\n")
		fmt.Printf("  Remove --dry-run to perform cleanup.\n")
		return nil
	}

	// 9. Confirmation
	if !force {
		fmt.Printf("\033[1;31m⚠ Warning: This will permanently delete %d backup(s) and %d WAL file(s).\033[0m\n", len(backupsToDelete), len(walFilesToDelete))
		if !confirmPrompt("Are you sure you want to proceed?") {
			fmt.Println("Cleanup cancelled.")
			return nil
		}
	}

	// 10. Delete old base backups
	for _, backup := range backupsToDelete {
		backupDir := pitrConfig.BaseBackupDir + "/" + backup.ID
		fmt.Printf("Deleting backup %s...\n", backup.ID)
		if err := os.RemoveAll(backupDir); err != nil {
			fmt.Printf("  \033[1;31m✗ Failed to delete: %v\033[0m\n", err)
		} else {
			fmt.Printf("  \033[1;32m✓ Deleted\033[0m\n")
		}
	}

	// 11. Delete old WAL files
	deletedWAL := 0
	for _, wal := range walFilesToDelete {
		if err := os.Remove(wal.FullPath); err != nil {
			fmt.Printf("  \033[1;31m✗ Failed to delete %s: %v\033[0m\n", wal.Filename, err)
		} else {
			deletedWAL++
		}
	}
	if len(walFilesToDelete) > 0 {
		fmt.Printf("Deleted %d WAL file(s).\n", deletedWAL)
	}

	fmt.Printf("\n\033[1;32m✓ Cleanup completed.\033[0m\n")
	return nil
}

func init() {
	pitrCleanupCmd.Flags().StringVar(&pitrCleanupProfile, "profile", "", "Profile name")
	pitrCleanupCmd.Flags().BoolVar(&pitrCleanupDryRun, "dry-run", false, "Show what would be deleted without actually deleting")
	pitrCleanupCmd.Flags().BoolVar(&pitrCleanupForce, "force", false, "Skip confirmation prompt")
	_ = pitrCleanupCmd.MarkFlagRequired("profile")

	pitrCmd.AddCommand(pitrCleanupCmd)
}
