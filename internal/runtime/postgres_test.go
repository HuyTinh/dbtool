package runtime

import (
	"path/filepath"
	"testing"

	"dbtool/internal/config"
)

func TestBuildDetectionFromSettingsMapsDockerHBAFile(t *testing.T) {
	profile := config.Profile{
		Runtime: &config.RuntimeProfile{
			Type: "docker",
			Mounts: []config.MountMapping{{
				Type:   "bind",
				Source: "/repo/pgdata",
				Target: "/var/lib/postgresql/data",
			}},
		},
	}

	result := BuildDetectionFromSettings(profile, PostgresSettings{
		DataDirectory: "/var/lib/postgresql/data",
		HBAFile:       "/var/lib/postgresql/data/pg_hba.conf",
	})

	if result.Runtime == nil {
		t.Fatalf("expected runtime metadata")
	}
	if result.Runtime.Paths.DataDirectory != "/var/lib/postgresql/data" {
		t.Fatalf("data directory not copied: %#v", result.Runtime.Paths)
	}
	if result.Runtime.Paths.HBAFile != "/var/lib/postgresql/data/pg_hba.conf" {
		t.Fatalf("hba file not copied: %#v", result.Runtime.Paths)
	}
	wantHBAPath := filepath.Join("/repo/pgdata", "pg_hba.conf")
	if result.HBAHostPath != wantHBAPath || !result.HBAEditable {
		t.Fatalf("expected editable host hba path, got %#v", result)
	}
	if result.Runtime.Paths.HostHBAFile != result.HBAHostPath {
		t.Fatalf("host hba not written to runtime paths: %#v", result.Runtime.Paths)
	}
}

func TestBuildDetectionFromSettingsNamedVolumeReportsNonEditable(t *testing.T) {
	profile := config.Profile{
		Runtime: &config.RuntimeProfile{
			Type: "docker",
			Mounts: []config.MountMapping{{
				Type:   "volume",
				Source: "pgdata",
				Target: "/var/lib/postgresql/data",
			}},
		},
	}

	result := BuildDetectionFromSettings(profile, PostgresSettings{HBAFile: "/var/lib/postgresql/data/pg_hba.conf"})

	if !result.HBAMatched || result.HBAEditable {
		t.Fatalf("expected matched non-editable hba, got %#v", result)
	}
	if result.HBAHostPath != "" {
		t.Fatalf("expected no host path for named volume, got %s", result.HBAHostPath)
	}
}
