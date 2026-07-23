package backup

import (
	"testing"

	"dbtool/internal/integrity"
)

func TestBuildRestorePreflightClassifiesUnsafeArtifactAndTarget(t *testing.T) {
	plan := BuildRestorePreflight(Artifact{Path: "broken.dump", Checksum: ChecksumMismatch}, nil, "", false, "target unavailable")
	if len(plan.Blockers) != 3 {
		t.Fatalf("blockers = %#v, want checksum, archive, and target blockers", plan.Blockers)
	}

	plan = BuildRestorePreflight(Artifact{Path: "unchecked.dump", Checksum: ChecksumMissing}, integrity.TOCEntryList{{Type: "TABLE", Schema: "public", Name: "orders"}}, "16.2", true, "")
	if len(plan.Blockers) != 0 || len(plan.Warnings) != 1 || plan.TableCount != 1 || plan.ArchiveVersion != "16.2" {
		t.Fatalf("plan = %#v, want listable archive with checksum warning", plan)
	}
}
