package backup

import (
	"fmt"

	"dbtool/internal/integrity"
)

// RestorePreflight is a read-only assessment. It does not prove a backup can
// be restored; an isolated restore drill is required for that claim.
type RestorePreflight struct {
	Artifact        Artifact `json:"artifact"`
	ArchiveListable bool     `json:"archive_listable"`
	ArchiveVersion  string   `json:"archive_version,omitempty"`
	SchemaCount     int      `json:"schema_count"`
	TableCount      int      `json:"table_count"`
	TargetReachable bool     `json:"target_reachable"`
	Blockers        []string `json:"blockers"`
	Warnings        []string `json:"warnings"`
	Info            []string `json:"info"`
}

// BuildRestorePreflight classifies metadata already collected by read-only checks.
func BuildRestorePreflight(artifact Artifact, entries integrity.TOCEntryList, version string, targetReachable bool, targetError string) RestorePreflight {
	plan := RestorePreflight{Artifact: artifact, ArchiveListable: entries != nil, ArchiveVersion: version, TargetReachable: targetReachable}
	if entries != nil {
		plan.SchemaCount = len(entries.Schemas())
		plan.TableCount = len(entries.Tables())
	}
	switch artifact.Checksum {
	case ChecksumValid:
		plan.Info = append(plan.Info, "checksum sidecar verified")
	case ChecksumMissing:
		plan.Warnings = append(plan.Warnings, "checksum sidecar is missing; artifact was not cryptographically verified")
	default:
		plan.Blockers = append(plan.Blockers, "artifact checksum is invalid or could not be verified")
	}
	if entries == nil {
		plan.Blockers = append(plan.Blockers, "archive cannot be listed with pg_restore")
	} else {
		plan.Info = append(plan.Info, fmt.Sprintf("archive inventory: %d schema(s), %d table(s)", plan.SchemaCount, plan.TableCount))
	}
	if !targetReachable {
		plan.Blockers = append(plan.Blockers, "target profile is unavailable: "+targetError)
	} else {
		plan.Info = append(plan.Info, "target profile is reachable")
	}
	return plan
}
