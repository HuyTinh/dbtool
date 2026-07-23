package pitr

import (
	"strings"
	"testing"
	"time"
)

func TestBuildStatusSummarizesRecoveryWindow(t *testing.T) {
	first := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	last := time.Date(2026, 7, 2, 9, 30, 0, 0, time.UTC)
	config := &PITRConfig{
		ProfileName: "source",
		Retention:   RetentionPolicy{KeepBaseBackups: 3, KeepWALDays: 7},
	}

	status := BuildStatus(config, []*BaseBackupMetadata{{
		ID:        "20260701_080000",
		StartTime: first,
		EndTime:   first.Add(5 * time.Minute),
		Size:      1024,
	}}, []*WALFileInfo{{
		Filename: "000000010000000000000001",
		Timeline: 1,
		Size:     2048,
		ModTime:  last,
	}})

	if !status.Configured {
		t.Fatal("Configured = false, want true")
	}
	if status.BackupCount != 1 || status.WALCount != 1 {
		t.Fatalf("counts = backups:%d wals:%d, want 1/1", status.BackupCount, status.WALCount)
	}
	if !status.EarliestBackupTime.Equal(first) || !status.LatestWALModTime.Equal(last) {
		t.Fatalf("metadata timestamps = %s - %s, want %s - %s", status.EarliestBackupTime, status.LatestWALModTime, first, last)
	}
	if status.TotalBackupSize != 1024 || status.TotalWALSize != 2048 {
		t.Fatalf("sizes = backup:%d wal:%d, want 1024/2048", status.TotalBackupSize, status.TotalWALSize)
	}
}

func TestBuildRecoveryPlanSelectsLatestBackupBeforeTarget(t *testing.T) {
	target := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	plan := BuildRecoveryPlan([]*BaseBackupMetadata{
		{ID: "20260701_080000", StartTime: target.Add(-48 * time.Hour)},
		{ID: "20260703_080000", StartTime: target.Add(-4 * time.Hour)},
	}, target, target.Add(-time.Hour))

	if plan.SelectedBackup == nil || plan.SelectedBackup.ID != "20260703_080000" {
		t.Fatalf("SelectedBackup = %#v, want latest backup before target", plan.SelectedBackup)
	}
	if !plan.PlanOnly {
		t.Fatal("PlanOnly = false, want true to prevent TUI execution")
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("Warnings = %#v, want target-after-WAL warning", plan.Warnings)
	}
}

func TestBuildStatusSortsBackupInventoryByStartTime(t *testing.T) {
	first := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	status := BuildStatus(&PITRConfig{}, []*BaseBackupMetadata{
		{ID: "newest", StartTime: first.Add(24 * time.Hour)},
		{ID: "oldest", StartTime: first},
	}, nil)

	if got := status.Backups[0].ID; got != "oldest" {
		t.Fatalf("first backup = %q, want oldest", got)
	}
}

func TestBuildValidatedRecoveryPlanRejectsBackupStillRunningAtTarget(t *testing.T) {
	target := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	plan := BuildValidatedRecoveryPlan(&PITRConfig{}, []*BaseBackupMetadata{{
		ID:        "in-progress",
		StartTime: target.Add(-time.Hour),
		EndTime:   target.Add(time.Hour),
	}}, target)

	if plan.SelectedBackup != nil {
		t.Fatalf("SelectedBackup = %#v, want none for incomplete backup", plan.SelectedBackup)
	}
	if len(plan.Blockers) == 0 || !strings.Contains(plan.Blockers[0], "completed") {
		t.Fatalf("Blockers = %#v, want completed-backup blocker", plan.Blockers)
	}
}
