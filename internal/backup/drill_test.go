package backup

import "testing"

func TestBuildRestoreDrillPlanRejectsTargetWithoutSandboxOptIn(t *testing.T) {
	_, err := BuildRestoreDrillPlan(RestoreDrillRequest{
		Artifact:      Artifact{Path: "backup.dump", Checksum: ChecksumValid},
		TargetProfile: "staging",
		TargetSandbox: false,
		Confirmed:     true,
	})
	if err == nil {
		t.Fatal("expected an unmarked target to be rejected")
	}
}

func TestBuildRestoreDrillPlanDefaultsToNonExecutablePreview(t *testing.T) {
	plan, err := BuildRestoreDrillPlan(RestoreDrillRequest{
		Artifact:      Artifact{Path: "backup.dump", Checksum: ChecksumValid},
		TargetProfile: "recovery-sandbox",
		TargetSandbox: true,
	})
	if err != nil {
		t.Fatalf("BuildRestoreDrillPlan: %v", err)
	}
	if plan.Executable {
		t.Fatal("unconfirmed drill plan must be non-executable")
	}
}

func TestBuildRestoreDrillPlanRequiresVerifiedArtifact(t *testing.T) {
	_, err := BuildRestoreDrillPlan(RestoreDrillRequest{
		Artifact:      Artifact{Path: "backup.dump", Checksum: ChecksumMissing},
		TargetProfile: "recovery-sandbox",
		TargetSandbox: true,
		Confirmed:     true,
	})
	if err == nil {
		t.Fatal("expected an artifact without a valid checksum to be rejected")
	}
}

func TestBuildRestoreDrillPlanEnablesCleanRestoreAndPostVerification(t *testing.T) {
	plan, err := BuildRestoreDrillPlan(RestoreDrillRequest{
		Artifact:      Artifact{Path: "backup.dump", Checksum: ChecksumValid},
		TargetProfile: "recovery-sandbox",
		TargetSandbox: true,
		Confirmed:     true,
	})
	if err != nil {
		t.Fatalf("BuildRestoreDrillPlan: %v", err)
	}
	if !plan.Clean || !plan.VerifyPostRestore || !plan.Executable || plan.TargetProfile != "recovery-sandbox" {
		t.Fatalf("unexpected drill plan: %#v", plan)
	}
}
