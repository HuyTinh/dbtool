package cmd

import (
	"fmt"
	"strings"

	"dbtool/internal/backup"

	"github.com/spf13/cobra"
)

var backupKeep int

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Verify and catalog local database dump artifacts",
	Long: `Read-only utilities for dump artifacts.

The commands in this group never create, modify, upload, or delete backup files.`,
}

var backupVerifyCmd = &cobra.Command{
	Use:   "verify [dump_file]",
	Short: "Verify a dump artifact against its checksum sidecar",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		artifact, err := backup.VerifyArtifact(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Artifact: %s\nSize: %s\nChecksum: %s\n", artifact.Path, formatBytes(artifact.Size), strings.ToUpper(string(artifact.Checksum)))
		if artifact.Checksum == backup.ChecksumMismatch || artifact.Checksum == backup.ChecksumError {
			return fmt.Errorf("backup verification failed")
		}
		if artifact.Checksum == backup.ChecksumMissing {
			fmt.Println("No checksum sidecar found; artifact was not cryptographically verified.")
			return nil
		}
		fmt.Println("✓ Checksum verified.")
		return nil
	},
}

var backupListCmd = &cobra.Command{
	Use:   "list [directory]",
	Short: "List local dump artifacts newest first",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		artifacts, err := backup.ScanCatalog(args[0])
		if err != nil {
			return err
		}
		if len(artifacts) == 0 {
			fmt.Println("No supported dump artifacts found.")
			return nil
		}
		fmt.Printf("%-20s %-12s %-10s %s\n", "MODIFIED", "SIZE", "CHECKSUM", "PATH")
		for _, artifact := range artifacts {
			fmt.Printf("%-20s %-12s %-10s %s\n", artifact.ModifiedAt.Local().Format("2006-01-02 15:04"), formatBytes(artifact.Size), strings.ToUpper(string(artifact.Checksum)), artifact.Path)
		}
		return nil
	},
}

var backupRetentionPreviewCmd = &cobra.Command{
	Use:   "retention-preview [directory]",
	Short: "Preview dump artifacts eligible for removal by count retention",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		artifacts, err := backup.ScanCatalog(args[0])
		if err != nil {
			return err
		}
		plan := backup.PlanRetention(artifacts, backupKeep)
		fmt.Printf("Retention preview: keep newest %d of %d artifact(s)\n", plan.Keep, len(artifacts))
		if len(plan.Candidates) == 0 {
			fmt.Println("No artifacts are eligible for removal.")
			return nil
		}
		fmt.Printf("Candidates (%d, %s reclaimable):\n", len(plan.Candidates), formatBytes(plan.ReclaimBytes))
		for _, artifact := range plan.Candidates {
			fmt.Printf("  - %s (%s)\n", artifact.Path, formatBytes(artifact.Size))
		}
		fmt.Println("Preview only: no files were deleted.")
		return nil
	},
}

func init() {
	backupRetentionPreviewCmd.Flags().IntVar(&backupKeep, "keep", 5, "Number of newest artifacts to retain")
	backupCmd.AddCommand(backupVerifyCmd, backupListCmd, backupRetentionPreviewCmd)
	RootCmd.AddCommand(backupCmd)
}
