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

var (
	sizeProfile string
	sizeSchema  string
	sizeLimit   int
	sizeOutput  string
)

var sizeCmd = &cobra.Command{
	Use:   "size",
	Short: "Inspect read-only PostgreSQL table and index storage sizes",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithTimeout(cmd, cmd.Context(), func(ctx context.Context) error {
			return executeSize(ctx)
		})
	},
}

func executeSize(ctx context.Context) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	profile, ok := cfg.GetProfile(sizeProfile)
	if !ok {
		return fmt.Errorf("profile %q not found", sizeProfile)
	}
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		return err
	}
	collector, ok := drv.(driver.SizeCollector)
	if !ok {
		return fmt.Errorf("driver %q does not support size snapshots", profile.Driver)
	}
	snapshot, err := collector.CollectSize(ctx, profile, sizeSchema, sizeLimit)
	if err != nil {
		return err
	}
	switch sizeOutput {
	case "table":
		printSizeTable(profile.Name, snapshot)
	case "json":
		return json.NewEncoder(os.Stdout).Encode(snapshot)
	default:
		return fmt.Errorf("unsupported output %q (want table or json)", sizeOutput)
	}
	return nil
}

func printSizeTable(profileName string, snapshot *driver.SizeSnapshot) {
	fmt.Printf("Database Size: %s (%s)\n", profileName, formatBytes(snapshot.DatabaseSize))
	fmt.Println("\nLargest relations (catalog row counts are estimates):")
	fmt.Printf("%-32s %12s %12s %12s %12s %12s\n", "RELATION", "ROWS", "TABLE", "INDEXES", "TOAST", "TOTAL")
	for _, relation := range snapshot.Relations {
		fmt.Printf("%-32s %12d %12s %12s %12s %12s\n", relation.Schema+"."+relation.Name, relation.EstimatedRows, formatBytes(relation.TableBytes), formatBytes(relation.IndexBytes), formatBytes(relation.ToastBytes), formatBytes(relation.TotalBytes))
	}
	fmt.Println("\nLargest indexes:")
	fmt.Printf("%-32s %-24s %12s\n", "INDEX", "TABLE", "SIZE")
	for _, index := range snapshot.Indexes {
		fmt.Printf("%-32s %-24s %12s\n", index.Schema+"."+index.Name, index.TableName, formatBytes(index.SizeBytes))
	}
}

func init() {
	sizeCmd.Flags().StringVar(&sizeProfile, "profile", "", "PostgreSQL profile name")
	sizeCmd.Flags().StringVar(&sizeSchema, "schema", "", "Limit results to one schema")
	sizeCmd.Flags().IntVar(&sizeLimit, "limit", 20, "Maximum relations and indexes to show")
	sizeCmd.Flags().StringVar(&sizeOutput, "output", "table", "Output format: table or json")
	_ = sizeCmd.MarkFlagRequired("profile")
	RootCmd.AddCommand(sizeCmd)
}
