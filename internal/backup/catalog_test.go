package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanCatalogListsDumpArtifactsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	older := filepath.Join(dir, "older.dump")
	newer := filepath.Join(dir, "newer.dump")
	if err := os.WriteFile(older, []byte("older"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("newer"), 0600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(24 * time.Hour)
	if err := os.Chtimes(older, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer+".sha256", []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}

	artifacts, err := ScanCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifact count = %d, want 2", len(artifacts))
	}
	if artifacts[0].Path != newer {
		t.Fatalf("newest path = %q, want %q", artifacts[0].Path, newer)
	}
	if artifacts[0].Checksum != ChecksumMismatch {
		t.Fatalf("checksum status = %q, want mismatch", artifacts[0].Checksum)
	}
}

func TestPlanRetentionKeepsNewestArtifacts(t *testing.T) {
	base := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	plan := PlanRetention([]Artifact{
		{Path: "newest.dump", ModifiedAt: base},
		{Path: "middle.dump", ModifiedAt: base.Add(-24 * time.Hour)},
		{Path: "oldest.dump", ModifiedAt: base.Add(-48 * time.Hour)},
	}, 2)

	if len(plan.Candidates) != 1 || plan.Candidates[0].Path != "oldest.dump" {
		t.Fatalf("candidates = %#v, want oldest artifact only", plan.Candidates)
	}
	if plan.ReclaimBytes != 0 {
		t.Fatalf("reclaim bytes = %d, want 0", plan.ReclaimBytes)
	}
}
