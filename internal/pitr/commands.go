package pitr

import (
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"dbtool/internal/config"
	dbtruntime "dbtool/internal/runtime"
)

var currentGOOS = goruntime.GOOS

func BuildArchiveCommand(profile config.Profile, hostArchiveDir string) (string, string, error) {
	archiveDirForPostgres, err := ResolvePathForPostgres(profile, hostArchiveDir)
	if err != nil {
		return "", "", err
	}
	return buildCopyToArchiveCommand(profile, archiveDirForPostgres), archiveDirForPostgres, nil
}

func BuildRestoreCommand(profile config.Profile, hostArchiveDir string) (string, string, error) {
	archiveDirForPostgres, err := ResolvePathForPostgres(profile, hostArchiveDir)
	if err != nil {
		return "", "", err
	}
	return buildRestoreFromArchiveCommand(profile, archiveDirForPostgres), archiveDirForPostgres, nil
}

func ResolvePathForPostgres(profile config.Profile, hostPath string) (string, error) {
	hostPath = strings.TrimSpace(hostPath)
	if hostPath == "" {
		return "", fmt.Errorf("path is empty")
	}

	if profile.Runtime == nil || profile.Runtime.Type == "" {
		return filepath.Clean(hostPath), nil
	}

	if profile.Runtime.Type != "docker" {
		return filepath.Clean(hostPath), nil
	}

	if len(profile.Runtime.Mounts) == 0 {
		return "", fmt.Errorf("profile %q uses Docker runtime but has no mount metadata; re-detect the runtime or set up a bind mount for %s", profile.Name, hostPath)
	}

	mapped := dbtruntime.MapHostPathToContainer(hostPath, profile.Runtime.Mounts)
	if !mapped.Matched || !mapped.Editable || mapped.HostPath == "" {
		reason := mapped.Reason
		if reason == "" {
			reason = "archive path is not accessible inside the PostgreSQL container"
		}
		return "", fmt.Errorf("profile %q uses Docker runtime but host path %s is not accessible inside the PostgreSQL container: %s", profile.Name, hostPath, reason)
	}

	return mapped.HostPath, nil
}

func buildCopyToArchiveCommand(profile config.Profile, archiveDir string) string {
	if profile.Runtime != nil && profile.Runtime.Type == "docker" {
		return fmt.Sprintf("cp \"%%p\" \"%s/%%f\"", strings.TrimRight(archiveDir, "/"))
	}
	if currentGOOS == "windows" {
		return fmt.Sprintf("cmd /c copy /Y \"%%p\" \"%s\\%%f\"", filepath.Clean(archiveDir))
	}
	return fmt.Sprintf("cp \"%%p\" \"%s/%%f\"", filepath.ToSlash(filepath.Clean(archiveDir)))
}

func buildRestoreFromArchiveCommand(profile config.Profile, archiveDir string) string {
	if profile.Runtime != nil && profile.Runtime.Type == "docker" {
		return fmt.Sprintf("cp \"%s/%%f\" \"%%p\"", strings.TrimRight(archiveDir, "/"))
	}
	if currentGOOS == "windows" {
		return fmt.Sprintf("cmd /c copy /Y \"%s\\%%f\" \"%%p\"", filepath.Clean(archiveDir))
	}
	return fmt.Sprintf("cp \"%s/%%f\" \"%%p\"", filepath.ToSlash(filepath.Clean(archiveDir)))
}
