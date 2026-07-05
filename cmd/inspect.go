package cmd

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"

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

		// Run native pg_restore -l command to read Table of Contents (TOC)
		prCmd := exec.Command("pg_restore", "-l", filePath)
		out, err := prCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to read archive TOC (file might be plain SQL or corrupt, or pg_restore is not in PATH): %w", err)
		}

		type dbObject struct {
			Type   string
			Schema string
			Name   string
			Owner  string
		}

		var tables []dbObject
		var views []dbObject

		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, ";") {
				continue
			}

			// Format of pg_restore -l lines:
			// 3125; 16400 16405 TABLE public orders postgres
			parts := strings.SplitN(line, ";", 2)
			if len(parts) < 2 {
				continue
			}

			fields := strings.Fields(parts[1])
			if len(fields) < 5 {
				continue
			}

			objType := fields[2]
			schema := fields[3]
			name := fields[4]
			owner := ""
			if len(fields) > 5 {
				owner = fields[5]
			}

			obj := dbObject{Type: objType, Schema: schema, Name: name, Owner: owner}
			switch objType {
			case "TABLE":
				tables = append(tables, obj)
			case "VIEW", "MATERIALIZED VIEW":
				views = append(views, obj)
			}
		}

		fmt.Printf("\nDump File Archive: %s\n", filePath)
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
