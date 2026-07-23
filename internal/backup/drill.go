package backup

import "fmt"

// RestoreDrillRequest contains only the safety inputs needed to authorize a
// destructive restore drill. The caller remains responsible for loading the
// profile and executing the resulting plan.
type RestoreDrillRequest struct {
	Artifact      Artifact
	TargetProfile string
	TargetSandbox bool
	Confirmed     bool
}

// RestoreDrillPlan is an approved, bounded restore-drill operation. It always
// clears the designated sandbox before restoring and verifies the result.
type RestoreDrillPlan struct {
	Artifact          Artifact
	TargetProfile     string
	Clean             bool
	VerifyPostRestore bool
	Executable        bool
	ManifestWarnings  []string
}

// BuildRestoreDrillPlan refuses unsafe targets and artifacts before a database
// command can be created. A profile must be explicitly designated as a sandbox.
// Without confirmation, the returned plan remains a non-executable preview.
func BuildRestoreDrillPlan(request RestoreDrillRequest) (RestoreDrillPlan, error) {
	if request.TargetProfile == "" {
		return RestoreDrillPlan{}, fmt.Errorf("restore drill target profile is required")
	}
	if !request.TargetSandbox {
		return RestoreDrillPlan{}, fmt.Errorf("target profile %q is not designated as a restore-drill sandbox", request.TargetProfile)
	}
	if request.Artifact.Checksum != ChecksumValid {
		return RestoreDrillPlan{}, fmt.Errorf("restore drill requires a checksum-verified artifact (got %s)", request.Artifact.Checksum)
	}
	return RestoreDrillPlan{
		Artifact:          request.Artifact,
		TargetProfile:     request.TargetProfile,
		Clean:             true,
		VerifyPostRestore: true,
		Executable:        request.Confirmed,
	}, nil
}
