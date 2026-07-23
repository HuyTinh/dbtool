package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"dbtool/internal/history"

	"github.com/spf13/cobra"
)

var recoverabilityProfile, recoverabilityOutput, recoverabilitySLA string

type recoverabilityStatus struct {
	Profile       string                 `json:"profile"`
	SLA           string                 `json:"sla"`
	LastSuccessAt *time.Time             `json:"last_success_at,omitempty"`
	LastFailure   *history.HistoryRecord `json:"last_failure,omitempty"`
	Stale         bool                   `json:"stale"`
}

var recoverabilityStatusCmd = &cobra.Command{
	Use:   "recoverability-status",
	Short: "Report the latest restore-drill recoverability evidence",
	RunE: func(cmd *cobra.Command, args []string) error {
		if recoverabilityOutput != "table" && recoverabilityOutput != "json" {
			return fmt.Errorf("unsupported output %q (want table or json)", recoverabilityOutput)
		}
		sla, err := time.ParseDuration(recoverabilitySLA)
		if err != nil || sla <= 0 {
			return fmt.Errorf("invalid --sla %q", recoverabilitySLA)
		}
		records, err := history.ReadHistory(history.QueryFilter{Profile: recoverabilityProfile, Limit: 0})
		if err != nil {
			return err
		}
		status := recoverabilityStatus{Profile: recoverabilityProfile, SLA: sla.String(), Stale: true}
		for _, record := range records {
			if record.Operation != "restore-drill" {
				continue
			}
			if record.Success && status.LastSuccessAt == nil {
				value := record.Time
				status.LastSuccessAt = &value
			}
			if !record.Success && status.LastFailure == nil {
				value := record
				status.LastFailure = &value
			}
		}
		if status.LastSuccessAt != nil {
			status.Stale = time.Since(*status.LastSuccessAt) > sla
		}
		if recoverabilityOutput == "json" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "PROFILE: %s\nSLA: %s\n", status.Profile, status.SLA)
		if status.LastSuccessAt == nil {
			fmt.Fprintln(cmd.OutOrStdout(), "LAST PASS: never")
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "LAST PASS: %s\n", status.LastSuccessAt.Local().Format(time.RFC3339))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "STATUS: %s\n", map[bool]string{true: "STALE", false: "CURRENT"}[status.Stale])
		if status.LastFailure != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "LAST FAILURE: %s\n", status.LastFailure.Error)
		}
		return nil
	},
}

func init() {
	recoverabilityStatusCmd.Flags().StringVar(&recoverabilityProfile, "profile", "", "Restore-drill sandbox profile")
	recoverabilityStatusCmd.Flags().StringVar(&recoverabilityOutput, "output", "table", "Output format: table or json")
	recoverabilityStatusCmd.Flags().StringVar(&recoverabilitySLA, "sla", "168h", "Maximum age of the last successful drill")
	_ = recoverabilityStatusCmd.MarkFlagRequired("profile")
	backupCmd.AddCommand(recoverabilityStatusCmd)
}
