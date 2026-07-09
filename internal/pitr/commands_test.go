package pitr

import (
	"path/filepath"
	"strings"
	"testing"

	"dbtool/internal/config"
)

func TestBuildArchiveCommandForDockerUsesContainerPath(t *testing.T) {
	profile := config.Profile{
		Name: "docker-pg",
		Runtime: &config.RuntimeProfile{
			Type: "docker",
			Mounts: []config.MountMapping{{
				Type:   "bind",
				Source: filepath.Join("host", "archive"),
				Target: "/var/lib/postgresql/archive",
			}},
		},
	}

	command, resolvedDir, err := BuildArchiveCommand(profile, filepath.Join("host", "archive", "wal"))
	if err != nil {
		t.Fatalf("BuildArchiveCommand returned error: %v", err)
	}
	if resolvedDir != "/var/lib/postgresql/archive/wal" {
		t.Fatalf("resolved dir mismatch: got %s", resolvedDir)
	}
	want := `cp "%p" "/var/lib/postgresql/archive/wal/%f"`
	if command != want {
		t.Fatalf("command mismatch:\n got: %s\nwant: %s", command, want)
	}
}

func TestBuildArchiveCommandForDockerRequiresMountedPath(t *testing.T) {
	profile := config.Profile{
		Name: "docker-pg",
		Runtime: &config.RuntimeProfile{
			Type: "docker",
			Mounts: []config.MountMapping{{
				Type:   "bind",
				Source: filepath.Join("host", "pgdata"),
				Target: "/var/lib/postgresql/data",
			}},
		},
	}

	_, _, err := BuildArchiveCommand(profile, filepath.Join("host", "archive", "wal"))
	if err == nil {
		t.Fatal("expected error for unmapped archive dir")
	}
	if !strings.Contains(err.Error(), "not accessible inside the PostgreSQL container") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildArchiveCommandForWindowsLocalUsesCmdCopy(t *testing.T) {
	prev := currentGOOS
	currentGOOS = "windows"
	defer func() { currentGOOS = prev }()

	profile := config.Profile{Name: "local"}
	command, resolvedDir, err := BuildArchiveCommand(profile, `C:\archive\wal`)
	if err != nil {
		t.Fatalf("BuildArchiveCommand returned error: %v", err)
	}
	if resolvedDir != `C:\archive\wal` {
		t.Fatalf("resolved dir mismatch: got %s", resolvedDir)
	}
	want := `cmd /c copy /Y "%p" "C:\archive\wal\%f"`
	if command != want {
		t.Fatalf("command mismatch:\n got: %s\nwant: %s", command, want)
	}
}

func TestBuildRestoreCommandForDockerUsesContainerPath(t *testing.T) {
	profile := config.Profile{
		Name: "docker-pg",
		Runtime: &config.RuntimeProfile{
			Type: "docker",
			Mounts: []config.MountMapping{{
				Type:   "bind",
				Source: filepath.Join("host", "archive"),
				Target: "/var/lib/postgresql/archive",
			}},
		},
	}

	command, resolvedDir, err := BuildRestoreCommand(profile, filepath.Join("host", "archive", "wal"))
	if err != nil {
		t.Fatalf("BuildRestoreCommand returned error: %v", err)
	}
	if resolvedDir != "/var/lib/postgresql/archive/wal" {
		t.Fatalf("resolved dir mismatch: got %s", resolvedDir)
	}
	want := `cp "/var/lib/postgresql/archive/wal/%f" "%p"`
	if command != want {
		t.Fatalf("command mismatch:\n got: %s\nwant: %s", command, want)
	}
}
