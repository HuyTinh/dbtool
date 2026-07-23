package pitr

import (
	"sort"
	"time"
)

// Status is a read-only PITR summary suitable for a CLI or interactive UI.
type Status struct {
	Configured         bool
	ProfileName        string
	Retention          RetentionPolicy
	BackupCount        int
	WALCount           int
	TotalBackupSize    int64
	TotalWALSize       int64
	EarliestBackupTime time.Time
	LatestWALModTime   time.Time
	Backups            []*BaseBackupMetadata
}

// RecoveryPlan describes a candidate recovery without executing it.
type RecoveryPlan struct {
	TargetTime     time.Time
	SelectedBackup *BaseBackupMetadata
	LatestWALTime  time.Time
	Warnings       []string
	Blockers       []string
	PlanOnly       bool
}

// BuildStatus summarizes already loaded PITR metadata without changing data.
func BuildStatus(config *PITRConfig, backups []*BaseBackupMetadata, walFiles []*WALFileInfo) Status {
	status := Status{
		Configured:  config != nil,
		BackupCount: len(backups),
		WALCount:    len(walFiles),
		Backups:     append([]*BaseBackupMetadata(nil), backups...),
	}
	sort.SliceStable(status.Backups, func(i, j int) bool {
		if status.Backups[i] == nil {
			return false
		}
		if status.Backups[j] == nil {
			return true
		}
		return status.Backups[i].StartTime.Before(status.Backups[j].StartTime)
	})
	if config != nil {
		status.ProfileName = config.ProfileName
		status.Retention = config.Retention
	}
	for _, backup := range backups {
		if backup == nil {
			continue
		}
		status.TotalBackupSize += backup.Size
		if status.EarliestBackupTime.IsZero() || backup.StartTime.Before(status.EarliestBackupTime) {
			status.EarliestBackupTime = backup.StartTime
		}
	}
	for _, wal := range walFiles {
		if wal == nil {
			continue
		}
		status.TotalWALSize += wal.Size
		if status.LatestWALModTime.IsZero() || wal.ModTime.After(status.LatestWALModTime) {
			status.LatestWALModTime = wal.ModTime
		}
	}
	return status
}

// BuildRecoveryPlan selects the newest usable base backup before targetTime.
// It deliberately produces a plan only; execution stays in the explicit CLI flow.
func BuildRecoveryPlan(backups []*BaseBackupMetadata, targetTime, latestWALTime time.Time) RecoveryPlan {
	plan := RecoveryPlan{
		TargetTime:    targetTime,
		LatestWALTime: latestWALTime,
		PlanOnly:      true,
	}
	if targetTime.After(time.Now()) {
		plan.Blockers = append(plan.Blockers, "Target time is in the future")
		return plan
	}
	plan.SelectedBackup = FindBestBackup(backups, targetTime)
	if plan.SelectedBackup == nil {
		plan.Blockers = append(plan.Blockers, "No base backup exists before the target time")
		return plan
	}
	if !latestWALTime.IsZero() && targetTime.After(latestWALTime) {
		plan.Warnings = append(plan.Warnings, "Target time may be after the latest archived WAL")
	}
	return plan
}

// BuildValidatedRecoveryPlan produces a plan only after local backup and WAL
// artifacts pass the available integrity checks. It never executes recovery.
func BuildValidatedRecoveryPlan(config *PITRConfig, backups []*BaseBackupMetadata, targetTime time.Time) RecoveryPlan {
	plan := RecoveryPlan{TargetTime: targetTime, PlanOnly: true}
	if config == nil {
		plan.Blockers = append(plan.Blockers, "PITR is not configured for this profile")
		return plan
	}
	if targetTime.After(time.Now()) {
		plan.Blockers = append(plan.Blockers, "Target time is in the future")
		return plan
	}
	for _, backup := range backups {
		if backup == nil || backup.EndTime.IsZero() || backup.EndTime.After(targetTime) {
			continue
		}
		if plan.SelectedBackup == nil || backup.EndTime.After(plan.SelectedBackup.EndTime) {
			plan.SelectedBackup = backup
		}
	}
	if plan.SelectedBackup == nil {
		plan.Blockers = append(plan.Blockers, "No completed base backup exists before the target time")
		return plan
	}

	backupValidation := ValidateBaseBackupIntegrity(config.BaseBackupDir, plan.SelectedBackup)
	plan.Warnings = append(plan.Warnings, backupValidation.Warnings...)
	plan.Blockers = append(plan.Blockers, backupValidation.Errors...)
	walValidation := ValidateWALCoverage(config.ArchiveDir, plan.SelectedBackup)
	plan.Warnings = append(plan.Warnings, walValidation.Warnings...)
	plan.Blockers = append(plan.Blockers, walValidation.Errors...)
	return plan
}
