package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProfileYAMLBackwardCompatibleWithoutRuntime(t *testing.T) {
	data := []byte(`driver: postgres
host: localhost
port: 5432
user: postgres
database: app
password: secret
`)

	var profile Profile
	if err := yaml.Unmarshal(data, &profile); err != nil {
		t.Fatalf("unmarshal profile without runtime: %v", err)
	}

	if profile.Runtime != nil {
		t.Fatalf("expected nil runtime for legacy profile, got %#v", profile.Runtime)
	}
	if profile.Driver != "postgres" || profile.Host != "localhost" || profile.Port != 5432 {
		t.Fatalf("legacy fields were not preserved: %#v", profile)
	}
}

func TestProfileYAMLRuntimeRoundTrip(t *testing.T) {
	profile := Profile{
		Driver:   "postgres",
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Database: "app",
		Password: "secret",
		Runtime: &RuntimeProfile{
			Type:        "docker",
			Source:      "docker-compose",
			SourceFile:  "docker-compose.yml",
			ServiceName: "postgres",
			Container:   "app-postgres-1",
			Mounts: []MountMapping{{
				Type:   "bind",
				Source: "/repo/pgdata",
				Target: "/var/lib/postgresql/data",
			}},
			Paths: RuntimePaths{
				DataDirectory: "/var/lib/postgresql/data",
				HBAFile:       "/var/lib/postgresql/data/pg_hba.conf",
				HostHBAFile:   "/repo/pgdata/pg_hba.conf",
			},
		},
	}

	data, err := yaml.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}

	var got Profile
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal profile: %v", err)
	}

	if got.Runtime == nil {
		t.Fatalf("expected runtime metadata after round trip")
	}
	if got.Runtime.Type != "docker" || got.Runtime.Source != "docker-compose" || got.Runtime.ServiceName != "postgres" {
		t.Fatalf("runtime metadata mismatch: %#v", got.Runtime)
	}
	if len(got.Runtime.Mounts) != 1 {
		t.Fatalf("expected one mount, got %#v", got.Runtime.Mounts)
	}
	if got.Runtime.Mounts[0].Source != "/repo/pgdata" || got.Runtime.Mounts[0].Target != "/var/lib/postgresql/data" {
		t.Fatalf("mount metadata mismatch: %#v", got.Runtime.Mounts[0])
	}
	if got.Runtime.Paths.HostHBAFile != "/repo/pgdata/pg_hba.conf" {
		t.Fatalf("runtime paths mismatch: %#v", got.Runtime.Paths)
	}
}
