package cmd

import (
	"fmt"
	"strings"

	"dbtool/internal/integrity"

	"github.com/spf13/cobra"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect [dump_file]",
	Short: "List all database tables, views, and schemas contained in a dump file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		if err := validateDumpFile(filePath); err != nil {
			return err
		}

		entries, version, err := integrity.ParseTOC(filePath)
		if err != nil {
			return fmt.Errorf("failed to read archive TOC (file might be plain SQL or corrupt, or pg_restore is not in PATH): %w", err)
		}

		tables := entries.Tables()
		views := entries.Views()

		fmt.Printf("\nDump File Archive: %s\n", filePath)
		if version != "" {
			fmt.Printf("PostgreSQL version: %s\n", version)
		}
		fmt.Println(strings.Repeat("=", 60))

		fmt.Printf("\nTables (%d):\n", len(tables))
		fmt.Println(strings.Repeat("-", 40))
		if len(tables) == 0 {
			fmt.Println("  (No tables found)")
		} else {
			for _, t := range tables {
				fmt.Printf("  - %s.%s (Owner: %s)\n", t.Schema, t.Name, t.Owner)
			}
		}

		if len(views) > 0 {
			fmt.Printf("\nViews (%d):\n", len(views))
			fmt.Println(strings.Repeat("-", 40))
			for _, v := range views {
				fmt.Printf("  - %s.%s (Owner: %s)\n", v.Schema, v.Name, v.Owner)
			}
		}
		fmt.Println()

		return nil
	},
}

func init() {
	RootCmd.AddCommand(inspectCmd)
}
