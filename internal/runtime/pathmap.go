package runtime

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"dbtool/internal/config"
)

type PathMapResult struct {
	HostPath string
	Matched  bool
	Editable bool
	Reason   string
	Mount    *config.MountMapping
}

func MapContainerPathToHost(path string, mounts []config.MountMapping) PathMapResult {
	path = cleanContainerPath(path)
	if path == "" {
		return PathMapResult{Reason: "empty path"}
	}

	sorted := append([]config.MountMapping(nil), mounts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(cleanContainerPath(sorted[i].Target)) > len(cleanContainerPath(sorted[j].Target))
	})

	for _, mount := range sorted {
		target := cleanContainerPath(mount.Target)
		if target == "" || !containerPathHasPrefix(path, target) {
			continue
		}

		matchedMount := mount
		switch strings.ToLower(mount.Type) {
		case "bind":
			rel := strings.TrimPrefix(path, target)
			rel = strings.TrimPrefix(rel, "/")
			hostPath := filepath.Clean(mount.Source)
			if rel != "" {
				hostPath = filepath.Join(hostPath, filepath.FromSlash(rel))
			}
			return PathMapResult{HostPath: hostPath, Matched: true, Editable: true, Mount: &matchedMount}
		case "volume":
			return PathMapResult{Matched: true, Editable: false, Reason: "path is inside a Docker named volume; direct host edit is not supported", Mount: &matchedMount}
		case "tmpfs":
			return PathMapResult{Matched: true, Editable: false, Reason: "path is inside a tmpfs mount; direct host edit is not supported", Mount: &matchedMount}
		default:
			return PathMapResult{Matched: true, Editable: false, Reason: "path is inside a non-bind mount; direct host edit is not supported", Mount: &matchedMount}
		}
	}

	return PathMapResult{Reason: "no mount target matches path"}
}

func MapHostPathToContainer(path string, mounts []config.MountMapping) PathMapResult {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return PathMapResult{Reason: "empty path"}
	}

	sorted := append([]config.MountMapping(nil), mounts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(filepath.Clean(sorted[i].Source)) > len(filepath.Clean(sorted[j].Source))
	})

	for _, mount := range sorted {
		source := filepath.Clean(strings.TrimSpace(mount.Source))
		if source == "" || !hostPathHasPrefix(path, source) {
			continue
		}

		matchedMount := mount
		switch strings.ToLower(mount.Type) {
		case "bind":
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return PathMapResult{Matched: true, Editable: false, Reason: fmt.Sprintf("cannot map host path into container mount: %v", err), Mount: &matchedMount}
			}
			containerPath := cleanContainerPath(mount.Target)
			if rel != "." {
				containerPath = cleanContainerPath(containerPath + "/" + filepath.ToSlash(rel))
			}
			return PathMapResult{HostPath: containerPath, Matched: true, Editable: true, Mount: &matchedMount}
		case "volume":
			return PathMapResult{Matched: true, Editable: false, Reason: "path is inside a Docker named volume; reverse host-to-container mapping is not supported", Mount: &matchedMount}
		case "tmpfs":
			return PathMapResult{Matched: true, Editable: false, Reason: "path is inside a tmpfs mount; reverse host-to-container mapping is not supported", Mount: &matchedMount}
		default:
			return PathMapResult{Matched: true, Editable: false, Reason: "path is inside a non-bind mount; reverse host-to-container mapping is not supported", Mount: &matchedMount}
		}
	}

	return PathMapResult{Reason: "no mount source matches path"}
}

func cleanContainerPath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" {
		return ""
	}
	parts := make([]string, 0)
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
			continue
		}
		parts = append(parts, part)
	}
	if strings.HasPrefix(path, "/") {
		return "/" + strings.Join(parts, "/")
	}
	return strings.Join(parts, "/")
}

func containerPathHasPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, strings.TrimRight(prefix, "/")+"/")
}

func hostPathHasPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+string(filepath.Separator))
}
