package cmd

import (
	"fmt"
	"strings"

	"dbtool/internal/history"

	"github.com/spf13/cobra"
)

var (
	historyProfile string
	historyLimit   int
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Display historical log of database restoration runs",
	RunE: func(cmd *cobra.Command, args []string) error {
		filter := history.QueryFilter{
			Profile: historyProfile,
			Limit:   historyLimit,
		}

		records, err := history.ReadHistory(filter)
		if err != nil {
			return fmt.Errorf("failed to retrieve log history: %w", err)
		}

		if len(records) == 0 {
			fmt.Println("No database restorations found in history.")
			return nil
		}

		fmt.Printf("%-25s %-15s %-16s %-10s %-32s\n", "TIME", "PROFILE", "OPERATION", "STATUS", "DUMP FILE")
		fmt.Println(strings.Repeat("-", 115))
		for _, r := range records {
			status := "\033[1;32mSUCCESS\033[0m"
			if !r.Success {
				status = "\033[1;31mFAILED\033[0m"
			}
			timeStr := r.Time.Local().Format("2006-01-02 15:04:05")
			operation := r.Operation
			if operation == "" {
				operation = "restore"
			}
			fmt.Printf("%-25s %-15s %-16s %-10s %-32s\n", timeStr, r.Profile, operation, status, r.File)
			if !r.Success && r.Error != "" {
				fmt.Printf("  -> \033[3mError: %s\033[0m\n", r.Error)
			}
		}
		return nil
	},
}

func init() {
	historyCmd.Flags().StringVar(&historyProfile, "profile", "", "Filter logs by connection profile name")
	historyCmd.Flags().IntVar(&historyLimit, "limit", 20, "Limit the number of history logs listed")
	RootCmd.AddCommand(historyCmd)
}
