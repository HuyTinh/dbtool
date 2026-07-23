package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"dbtool/internal/backup"

	"github.com/spf13/cobra"
)

var backupAuditKeep int
var backupAuditMaxAge, backupAuditOutput string

type backupAudit struct {
	Artifacts       int                  `json:"artifacts"`
	Valid           int                  `json:"valid"`
	MissingChecksum int                  `json:"missing_checksum"`
	Invalid         int                  `json:"invalid"`
	Latest          *backup.Artifact     `json:"latest,omitempty"`
	Stale           bool                 `json:"stale"`
	Retention       backup.RetentionPlan `json:"retention"`
}

var backupAuditCmd = &cobra.Command{
	Use: "audit [directory]", Short: "Audit local backup freshness, checksum health, and retention",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if backupAuditOutput != "table" && backupAuditOutput != "json" {
			return fmt.Errorf("unsupported output %q (want table or json)", backupAuditOutput)
		}
		maxAge, err := time.ParseDuration(backupAuditMaxAge)
		if err != nil || maxAge <= 0 {
			return fmt.Errorf("invalid --max-age %q", backupAuditMaxAge)
		}
		artifacts, err := backup.ScanCatalog(args[0])
		if err != nil {
			return err
		}
		audit := backupAudit{Artifacts: len(artifacts), Stale: true, Retention: backup.PlanRetention(artifacts, backupAuditKeep)}
		for i := range artifacts {
			switch artifacts[i].Checksum {
			case backup.ChecksumValid:
				audit.Valid++
			case backup.ChecksumMissing:
				audit.MissingChecksum++
			default:
				audit.Invalid++
			}
		}
		if len(artifacts) > 0 {
			audit.Latest = &artifacts[0]
			audit.Stale = time.Since(artifacts[0].ModifiedAt) > maxAge
		}
		if backupAuditOutput == "json" {
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(audit); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Artifacts: %d | valid: %d | missing checksum: %d | invalid: %d\n", audit.Artifacts, audit.Valid, audit.MissingChecksum, audit.Invalid)
			if audit.Latest == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Latest: none")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Latest: %s (%s)\n", audit.Latest.Path, audit.Latest.ModifiedAt.Local().Format(time.RFC3339))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Freshness: %s\n", map[bool]string{true: "STALE", false: "CURRENT"}[audit.Stale])
			fmt.Fprintf(cmd.OutOrStdout(), "Retention preview: %d candidate(s), %s reclaimable\n", len(audit.Retention.Candidates), formatBytes(audit.Retention.ReclaimBytes))
		}
		if audit.Invalid > 0 || audit.Latest == nil || audit.Stale {
			return fmt.Errorf("backup audit failed")
		}
		return nil
	},
}

func init() {
	backupAuditCmd.Flags().IntVar(&backupAuditKeep, "keep", 5, "Number of newest artifacts to retain")
	backupAuditCmd.Flags().StringVar(&backupAuditMaxAge, "max-age", "24h", "Maximum age of the newest backup")
	backupAuditCmd.Flags().StringVar(&backupAuditOutput, "output", "table", "Output format: table or json")
	backupCmd.AddCommand(backupAuditCmd)
}
