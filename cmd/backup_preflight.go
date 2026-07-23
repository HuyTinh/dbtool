package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"dbtool/internal/backup"
	"dbtool/internal/config"
	"dbtool/internal/driver"
	"dbtool/internal/integrity"

	"github.com/spf13/cobra"
)

var backupPreflightProfile, backupPreflightOutput string

var backupRestorePreflightCmd = &cobra.Command{
	Use:   "restore-preflight [dump_file]",
	Short: "Read-only restore readiness assessment for one dump artifact",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithTimeout(cmd, cmd.Context(), func(ctx context.Context) error {
			return executeBackupRestorePreflight(ctx, args[0])
		})
	},
}

func executeBackupRestorePreflight(ctx context.Context, path string) error {
	artifact, err := backup.VerifyArtifact(path)
	if err != nil {
		return err
	}
	entries, version, tocErr := integrity.ParseTOC(path)
	if tocErr != nil {
		entries = nil
		version = ""
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	profile, ok := cfg.GetProfile(backupPreflightProfile)
	if !ok {
		return fmt.Errorf("profile %q not found", backupPreflightProfile)
	}
	reachable, targetErr := preflightTargetReachable(ctx, profile)
	plan := backup.BuildRestorePreflight(artifact, entries, version, reachable, targetErr)
	if backupPreflightOutput == "json" {
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	if backupPreflightOutput != "table" {
		return fmt.Errorf("unsupported output %q (want table or json)", backupPreflightOutput)
	}
	fmt.Printf("Restore Preflight: %s → %s\n", artifact.Path, profile.Name)
	fmt.Printf("Checksum: %s\nArchive listable: %t\nArchive version: %s\nInventory: %d schema(s), %d table(s)\nTarget reachable: %t\n", strings.ToUpper(string(artifact.Checksum)), plan.ArchiveListable, plan.ArchiveVersion, plan.SchemaCount, plan.TableCount, plan.TargetReachable)
	for _, blocker := range plan.Blockers {
		fmt.Printf("BLOCKER: %s\n", blocker)
	}
	for _, warning := range plan.Warnings {
		fmt.Printf("WARNING: %s\n", warning)
	}
	for _, info := range plan.Info {
		fmt.Printf("INFO: %s\n", info)
	}
	fmt.Println("Preflight only: archive listability and metadata do not prove recoverability; use an isolated restore drill.")
	return nil
}

func preflightTargetReachable(ctx context.Context, profile config.Profile) (bool, string) {
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		return false, err.Error()
	}
	collector, ok := drv.(driver.HealthCollector)
	if !ok {
		return false, fmt.Sprintf("driver %q has no read-only connectivity check", profile.Driver)
	}
	if _, err := collector.CollectHealth(ctx, profile, 0); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func init() {
	backupRestorePreflightCmd.Flags().StringVar(&backupPreflightProfile, "profile", "", "Target PostgreSQL profile (read-only connectivity check)")
	backupRestorePreflightCmd.Flags().StringVar(&backupPreflightOutput, "output", "table", "Output format: table or json")
	_ = backupRestorePreflightCmd.MarkFlagRequired("profile")
	backupCmd.AddCommand(backupRestorePreflightCmd)
}
