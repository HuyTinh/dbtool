package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/spf13/cobra"
)

var migratePreflightFrom, migratePreflightTo, migratePreflightOutput string

var migratePreflightCmd = &cobra.Command{
	Use:   "preflight",
	Short: "Compare source and target schemas without migrating data",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithTimeout(cmd, cmd.Context(), executeMigratePreflight)
	},
}

func executeMigratePreflight(ctx context.Context) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	src, ok := cfg.GetProfile(migratePreflightFrom)
	if !ok {
		return fmt.Errorf("source profile %q not found", migratePreflightFrom)
	}
	dst, ok := cfg.GetProfile(migratePreflightTo)
	if !ok {
		return fmt.Errorf("target profile %q not found", migratePreflightTo)
	}
	if src.Name == dst.Name {
		return fmt.Errorf("source and target profiles must be different")
	}
	if src.Driver != dst.Driver {
		return fmt.Errorf("cross-driver migration preflight is not supported (source: %s, target: %s)", src.Driver, dst.Driver)
	}
	drv, err := driver.Get(src.Driver)
	if err != nil {
		return err
	}
	inspector, ok := drv.(driver.SchemaInspector)
	if !ok {
		return fmt.Errorf("driver %q does not support schema snapshots", src.Driver)
	}
	source, err := inspector.CollectSchema(ctx, src)
	if err != nil {
		return fmt.Errorf("collect source schema: %w", err)
	}
	target, err := inspector.CollectSchema(ctx, dst)
	if err != nil {
		return fmt.Errorf("collect target schema: %w", err)
	}
	diff := driver.CompareSchemaSnapshots(source, target)
	if migratePreflightOutput == "json" {
		return json.NewEncoder(os.Stdout).Encode(diff)
	}
	if migratePreflightOutput != "table" {
		return fmt.Errorf("unsupported output %q (want table or json)", migratePreflightOutput)
	}
	fmt.Printf("Migration Preflight: %s → %s\n", src.Name, dst.Name)
	if len(diff.MissingTables)+len(diff.ExtraTables)+len(diff.ColumnDifferences) == 0 {
		fmt.Println("INFO: no catalog differences found.")
		return nil
	}
	for _, table := range diff.MissingTables {
		fmt.Printf("WARNING: missing from target: %s\n", table)
	}
	for _, table := range diff.ExtraTables {
		fmt.Printf("WARNING: extra on target: %s\n", table)
	}
	for _, column := range diff.ColumnDifferences {
		fmt.Printf("WARNING: column difference: %s.%s\n", column.Table, column.Column)
	}
	return nil
}

func init() {
	migratePreflightCmd.Flags().StringVar(&migratePreflightFrom, "source", "", "Source PostgreSQL profile")
	migratePreflightCmd.Flags().StringVar(&migratePreflightTo, "target", "", "Target PostgreSQL profile")
	migratePreflightCmd.Flags().StringVar(&migratePreflightOutput, "output", "table", "Output format: table or json")
	_ = migratePreflightCmd.MarkFlagRequired("source")
	_ = migratePreflightCmd.MarkFlagRequired("target")
	migrateCmd.AddCommand(migratePreflightCmd)
}
