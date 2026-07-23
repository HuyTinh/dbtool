package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"dbtool/internal/backup"
	"dbtool/internal/config"
	"dbtool/internal/driver"
	"dbtool/internal/history"
	"dbtool/internal/integrity"

	"github.com/spf13/cobra"
)

var backupRestoreDrillProfile, backupRestoreDrillOutput, backupRestoreDrillPolicy string
var backupRestoreDrillConfirm bool

var backupRestoreDrillCmd = &cobra.Command{
	Use:   "restore-drill [dump_file]",
	Short: "Restore a checksum-verified artifact into an opted-in sandbox and verify it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithTimeout(cmd, cmd.Context(), func(ctx context.Context) error {
			return executeBackupRestoreDrill(ctx, args[0])
		})
	},
}

func executeBackupRestoreDrill(ctx context.Context, path string) error {
	artifact, err := backup.VerifyArtifact(path)
	if err != nil {
		return err
	}
	checksum, err := integrity.ReadChecksumFile(path)
	if err != nil {
		return err
	}
	manifestWarnings, err := backup.ValidateManifest(path, checksum, backup.VerificationPolicy(backupRestoreDrillPolicy))
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	profile, ok := cfg.GetProfile(backupRestoreDrillProfile)
	if !ok {
		return fmt.Errorf("profile %q not found", backupRestoreDrillProfile)
	}
	plan, err := backup.BuildRestoreDrillPlan(backup.RestoreDrillRequest{
		Artifact:      artifact,
		TargetProfile: profile.Name,
		TargetSandbox: profile.RestoreDrillSandbox,
		Confirmed:     backupRestoreDrillConfirm,
	})
	if err != nil {
		return err
	}
	plan.ManifestWarnings = manifestWarnings
	if backupRestoreDrillOutput != "table" && backupRestoreDrillOutput != "json" {
		return fmt.Errorf("unsupported output %q (want table or json)", backupRestoreDrillOutput)
	}
	if !plan.Executable {
		return printRestoreDrillPreview(plan)
	}

	drv, err := driver.Get(profile.Driver)
	if err != nil {
		return err
	}
	format, err := drv.DetectFormat(path)
	if err != nil {
		return fmt.Errorf("detect restore drill artifact format: %w", err)
	}
	startedAt := time.Now()
	progress, err := drv.Restore(ctx, driver.RestoreOptions{
		Profile:  profile,
		FilePath: path,
		Format:   format,
		Jobs:     defaultJobs(),
		Clean:    plan.Clean,
	})
	if err != nil {
		return fmt.Errorf("start restore drill: %w", err)
	}
	for update := range progress {
		if update.Err != nil {
			return recordRestoreDrill(path, profile.Name, false, startedAt, update.Err)
		}
	}
	verification, err := drv.VerifyRestore(ctx, driver.VerifyRestoreOptions{Profile: profile, FilePath: path, Format: format})
	if err != nil {
		return recordRestoreDrill(path, profile.Name, false, startedAt, fmt.Errorf("verify restore drill: %w", err))
	}
	if !verification.Verified {
		return recordRestoreDrill(path, profile.Name, false, startedAt, fmt.Errorf("restore drill verification failed: %v", verification.Errors))
	}
	manifest, err := backup.ReadManifest(path)
	if err != nil {
		return recordRestoreDrill(path, profile.Name, false, startedAt, fmt.Errorf("read restore-drill manifest: %w", err))
	}
	if len(manifest.RowCounts) > 0 {
		collector, ok := drv.(driver.RowCountCollector)
		if !ok {
			return recordRestoreDrill(path, profile.Name, false, startedAt, fmt.Errorf("driver %q does not support manifest row-count verification", profile.Driver))
		}
		tables := make([]driver.CatalogTable, 0, len(manifest.RowCounts))
		for _, expected := range manifest.RowCounts {
			tables = append(tables, driver.CatalogTable{Schema: expected.Schema, Name: expected.Table})
		}
		counts, err := collector.CollectRowCounts(ctx, profile, tables)
		if err != nil {
			return recordRestoreDrill(path, profile.Name, false, startedAt, fmt.Errorf("collect restored row counts: %w", err))
		}
		actual := make([]backup.TableRowCount, 0, len(counts))
		for _, count := range counts {
			actual = append(actual, backup.TableRowCount{Schema: count.Schema, Table: count.Table, RowCount: count.RowCount})
		}
		if err := backup.ValidateRowCountBaseline(*manifest, actual); err != nil {
			return recordRestoreDrill(path, profile.Name, false, startedAt, err)
		}
	}
	if err := recordRestoreDrill(path, profile.Name, true, startedAt, nil); err != nil {
		return err
	}
	if backupRestoreDrillOutput == "json" {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Plan         backup.RestoreDrillPlan `json:"plan"`
			Verification *driver.VerifyResult    `json:"verification"`
		}{Plan: plan, Verification: verification})
	}
	fmt.Printf("Restore drill passed: %s → %s\n", artifact.Path, profile.Name)
	fmt.Printf("Verified %d/%d expected table(s).\n", verification.TablesFound, verification.TablesExpected)
	return nil
}

func printRestoreDrillPreview(plan backup.RestoreDrillPlan) error {
	if backupRestoreDrillOutput == "json" {
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	fmt.Printf("Restore drill preview: %s → %s\n", plan.Artifact.Path, plan.TargetProfile)
	fmt.Println("Would clean the opted-in sandbox, restore the artifact, then verify table recovery.")
	for _, warning := range plan.ManifestWarnings {
		fmt.Printf("WARNING: %s\n", warning)
	}
	fmt.Println("Preview only: rerun with --confirm to execute the destructive sandbox restore.")
	return nil
}

func recordRestoreDrill(path, profile string, success bool, startedAt time.Time, operationErr error) error {
	record := history.HistoryRecord{
		Operation: "restore-drill",
		File:      path,
		Profile:   profile,
		Time:      startedAt,
		Success:   success,
		Command:   "backup restore-drill",
	}
	if operationErr != nil {
		record.Error = operationErr.Error()
	}
	if err := history.AppendHistory(record); err != nil {
		return fmt.Errorf("record restore drill history: %w", err)
	}
	return operationErr
}

func init() {
	backupRestoreDrillCmd.Flags().StringVar(&backupRestoreDrillProfile, "profile", "", "Opted-in sandbox PostgreSQL profile")
	backupRestoreDrillCmd.Flags().BoolVar(&backupRestoreDrillConfirm, "confirm", false, "Execute the destructive restore into the opted-in sandbox")
	backupRestoreDrillCmd.Flags().StringVar(&backupRestoreDrillOutput, "output", "table", "Output format: table or json")
	backupRestoreDrillCmd.Flags().StringVar(&backupRestoreDrillPolicy, "verification-policy", "basic", "Verification policy: basic or strict")
	_ = backupRestoreDrillCmd.MarkFlagRequired("profile")
	backupCmd.AddCommand(backupRestoreDrillCmd)
}
