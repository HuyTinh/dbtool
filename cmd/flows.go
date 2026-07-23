package cmd

import (
	"fmt"
	"strings"

	"dbtool/internal/flows"

	"github.com/spf13/cobra"
)

func dumpFlow(profile, filePath, format string, includeTables, excludeTables, includeSchemas, excludeSchemas []string) flows.Flow {
	return flows.Flow{Operation: flows.OperationDump, Profile: profile, FilePath: filePath, Settings: flowSettings(format, 0, false, false, false, false, false, includeTables, excludeTables, includeSchemas, excludeSchemas)}
}

func restoreFlow(profile, filePath, format string, jobs int, clean, createIfMissing, optimize bool, includeTables, excludeTables, includeSchemas, excludeSchemas []string) flows.Flow {
	return flows.Flow{Operation: flows.OperationRestore, Profile: profile, FilePath: filePath, Settings: flowSettings(format, jobs, clean, createIfMissing, optimize, false, false, includeTables, excludeTables, includeSchemas, excludeSchemas)}
}

func migrateFlow(source, target, format string, jobs int, schemaOnly, dataOnly, clean, createIfMissing bool, includeTables, excludeTables, includeSchemas, excludeSchemas []string) flows.Flow {
	return flows.Flow{Operation: flows.OperationMigrate, Profile: source, DestinationProfile: target, Settings: flowSettings(format, jobs, clean, createIfMissing, false, schemaOnly, dataOnly, includeTables, excludeTables, includeSchemas, excludeSchemas)}
}

func flowSettings(format string, jobs int, clean, createIfMissing, optimize, schemaOnly, dataOnly bool, includeTables, excludeTables, includeSchemas, excludeSchemas []string) flows.Settings {
	return flows.Settings{Format: format, Jobs: jobs, Clean: clean, CreateIfMissing: createIfMissing, Optimize: optimize, SchemaOnly: schemaOnly, DataOnly: dataOnly, IncludeTables: includeTables, ExcludeTables: excludeTables, IncludeSchemas: includeSchemas, ExcludeSchemas: excludeSchemas}
}

// recordFlow is deliberately best-effort: a completed database operation must not
// be reported as failed solely because its optional reusable-flow metadata cannot be saved.
func recordFlow(flow flows.Flow) {
	store, err := flows.Open()
	if err == nil {
		_ = store.Record(flow)
	}
}

var flowsCmd = &cobra.Command{
	Use:   "flows",
	Short: "List recent and pinned reusable flows",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := flows.Open()
		if err != nil {
			return fmt.Errorf("open flows: %w", err)
		}
		data, err := store.Load()
		if err != nil {
			return err
		}
		if len(data.Pinned) == 0 && len(data.Recent) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No recent or pinned flows.")
			return nil
		}
		printFlows(cmd, "Pinned", data.Pinned)
		printFlows(cmd, "Recent", data.Recent)
		return nil
	},
}

func printFlows(cmd *cobra.Command, heading string, entries []flows.Flow) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s:\n", heading)
	for _, flow := range entries {
		target := flow.Profile
		if flow.DestinationProfile != "" {
			target += " -> " + flow.DestinationProfile
		}
		if flow.FilePath != "" {
			target += "  " + flow.FilePath
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %-8s %-40s %s\n", strings.ToUpper(string(flow.Operation)), target, flow.LastUsed.Local().Format("2006-01-02 15:04"))
	}
}

func init() { RootCmd.AddCommand(flowsCmd) }
