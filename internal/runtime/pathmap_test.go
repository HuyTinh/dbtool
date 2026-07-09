package runtime

import (
	"path/filepath"
	"testing"

	"dbtool/internal/config"
)

func TestMapContainerPathToHostBindMount(t *testing.T) {
	mounts := []config.MountMapping{{
		Type:   "bind",
		Source: filepath.Join("repo", "pgdata"),
		Target: "/var/lib/postgresql/data",
	}}

	result := MapContainerPathToHost("/var/lib/postgresql/data/pg_hba.conf", mounts)

	if !result.Matched || !result.Editable {
		t.Fatalf("expected editable match, got %#v", result)
	}
	want := filepath.Join("repo", "pgdata", "pg_hba.conf")
	if result.HostPath != want {
		t.Fatalf("host path mismatch: got %s want %s", result.HostPath, want)
	}
}

func TestMapContainerPathToHostNamedVolumeIsNotEditable(t *testing.T) {
	mounts := []config.MountMapping{{
		Type:   "volume",
		Source: "pgdata",
		Target: "/var/lib/postgresql/data",
	}}

	result := MapContainerPathToHost("/var/lib/postgresql/data/pg_hba.conf", mounts)

	if !result.Matched || result.Editable {
		t.Fatalf("expected non-editable named volume match, got %#v", result)
	}
	if result.HostPath != "" {
		t.Fatalf("named volume should not return a host path, got %s", result.HostPath)
	}
}

func TestMapContainerPathToHostUsesPathBoundary(t *testing.T) {
	mounts := []config.MountMapping{{
		Type:   "bind",
		Source: "/repo/pgdata",
		Target: "/var/lib/postgresql/data",
	}}

	result := MapContainerPathToHost("/var/lib/postgresql/database/pg_hba.conf", mounts)

	if result.Matched {
		t.Fatalf("expected no match for path-boundary mismatch, got %#v", result)
	}
}

func TestMapContainerPathToHostUsesLongestTargetPrefix(t *testing.T) {
	mounts := []config.MountMapping{
		{Type: "bind", Source: "/repo/data", Target: "/var/lib/postgresql/data"},
		{Type: "bind", Source: "/repo/conf", Target: "/var/lib/postgresql/data/conf"},
	}

	result := MapContainerPathToHost("/var/lib/postgresql/data/conf/pg_hba.conf", mounts)

	if !result.Matched || !result.Editable {
		t.Fatalf("expected editable match, got %#v", result)
	}
	want := filepath.Join("/repo/conf", "pg_hba.conf")
	if result.HostPath != want {
		t.Fatalf("host path mismatch: got %s want %s", result.HostPath, want)
	}
}

func TestMapHostPathToContainerBindMount(t *testing.T) {
	mounts := []config.MountMapping{{
		Type:   "bind",
		Source: filepath.Join("repo", "pgdata"),
		Target: "/var/lib/postgresql/data",
	}}

	result := MapHostPathToContainer(filepath.Join("repo", "pgdata", "archive"), mounts)

	if !result.Matched || !result.Editable {
		t.Fatalf("expected editable match, got %#v", result)
	}
	if result.HostPath != "/var/lib/postgresql/data/archive" {
		t.Fatalf("container path mismatch: got %s", result.HostPath)
	}
}

func TestMapHostPathToContainerUsesLongestSourcePrefix(t *testing.T) {
	mounts := []config.MountMapping{
		{Type: "bind", Source: filepath.Join("repo", "data"), Target: "/var/lib/postgresql/data"},
		{Type: "bind", Source: filepath.Join("repo", "data", "archive"), Target: "/mnt/archive"},
	}

	result := MapHostPathToContainer(filepath.Join("repo", "data", "archive", "wal"), mounts)

	if !result.Matched || !result.Editable {
		t.Fatalf("expected editable match, got %#v", result)
	}
	if result.HostPath != "/mnt/archive/wal" {
		t.Fatalf("container path mismatch: got %s", result.HostPath)
	}
}

func TestMapHostPathToContainerNoMatch(t *testing.T) {
	mounts := []config.MountMapping{{
		Type:   "bind",
		Source: filepath.Join("repo", "pgdata"),
		Target: "/var/lib/postgresql/data",
	}}

	result := MapHostPathToContainer(filepath.Join("other", "archive"), mounts)

	if result.Matched {
		t.Fatalf("expected no match, got %#v", result)
	}
}
