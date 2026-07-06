package runtime

import "dbtool/internal/config"

type PostgresSettings struct {
	DataDirectory string
	HBAFile       string
}

type DetectionResult struct {
	Runtime     *config.RuntimeProfile
	HBAHostPath string
	HBAMatched  bool
	HBAEditable bool
	HBAReason   string
}

func BuildDetectionFromSettings(profile config.Profile, settings PostgresSettings) DetectionResult {
	runtimeProfile := cloneRuntimeProfile(profile.Runtime)
	if runtimeProfile == nil {
		runtimeProfile = &config.RuntimeProfile{}
	}
	runtimeProfile.Paths.DataDirectory = settings.DataDirectory
	runtimeProfile.Paths.HBAFile = settings.HBAFile

	result := DetectionResult{Runtime: runtimeProfile}
	if runtimeProfile.Type == "docker" && len(runtimeProfile.Mounts) > 0 && settings.HBAFile != "" {
		mapped := MapContainerPathToHost(settings.HBAFile, runtimeProfile.Mounts)
		result.HBAMatched = mapped.Matched
		result.HBAEditable = mapped.Editable
		result.HBAReason = mapped.Reason
		if mapped.Editable {
			result.HBAHostPath = mapped.HostPath
			runtimeProfile.Paths.HostHBAFile = mapped.HostPath
		}
	}
	return result
}

func cloneRuntimeProfile(src *config.RuntimeProfile) *config.RuntimeProfile {
	if src == nil {
		return nil
	}
	clone := *src
	if src.Mounts != nil {
		clone.Mounts = append([]config.MountMapping(nil), src.Mounts...)
	}
	return &clone
}
