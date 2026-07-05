package cmd

import (
	"fmt"

	"dbtool/internal/config"
	"dbtool/internal/tui"

	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch interactive TUI to select profile and dump file for restore",
	RunE:  runTUI,
}

func runTUI(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	initSettings := tui.RestoreSettings{
		Format:          restoreFormat,
		Jobs:            restoreJobs,
		Clean:           restoreClean,
		DryRun:          restoreDryRun,
		CreateIfMissing: restoreCreateIfMissing,
		IncludeTable:    includeTable,
		ExcludeTable:    excludeTable,
		IncludeSchema:   includeSchema,
		ExcludeSchema:   excludeSchema,
	}

	result, err := tui.Run(cfg, initSettings)
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	if !result.Confirm {
		fmt.Println("Cancelled.")
		return nil
	}

	return nil
}

func init() {
	tuiCmd.Flags().StringVar(&restoreFormat, "format", "auto", "Format of dump file (auto, custom, plain, directory)")
	tuiCmd.Flags().IntVarP(&restoreJobs, "jobs", "j", defaultJobs(), "Number of parallel restore jobs")
	tuiCmd.Flags().BoolVar(&restoreClean, "clean", false, "Clean (drop) database objects before recreating")
	tuiCmd.Flags().BoolVar(&restoreDryRun, "dry-run", false, "Show details and the native command that would run")
	tuiCmd.Flags().BoolVar(&restoreCreateIfMissing, "create-if-missing", false, "Create the target database if it does not exist")
	tuiCmd.Flags().StringSliceVar(&includeTable, "include-table", nil, "Restore specific table (can be repeated)")
	tuiCmd.Flags().StringSliceVar(&excludeTable, "exclude-table", nil, "Exclude specific table (can be repeated)")
	tuiCmd.Flags().StringSliceVar(&includeSchema, "include-schema", nil, "Restore specific schema (can be repeated)")
	tuiCmd.Flags().StringSliceVar(&excludeSchema, "exclude-schema", nil, "Exclude specific schema (can be repeated)")

	RootCmd.AddCommand(tuiCmd)

	// Also launch TUI when dbtool is invoked with no subcommand and no args
	RootCmd.RunE = runTUI
}
