package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadConfigLegacyVersionlessFileDefaultsToCurrentVersion(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "AppData", "Roaming")
	t.Setenv("APPDATA", configDir)

	path, err := GetConfigFilePath()
	if err != nil {
		t.Fatalf("GetConfigFilePath: %v", err)
	}

	legacy := `profiles:
  local:
    driver: postgres
    host: localhost
    port: 5432
    user: postgres
    database: app
    password: secret
`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Version != CurrentConfigVersion {
		t.Fatalf("cfg.Version = %d, want %d", cfg.Version, CurrentConfigVersion)
	}
	if _, ok := cfg.GetProfile("local"); !ok {
		t.Fatalf("expected legacy profile to load")
	}
}

func TestSaveConfigWritesCurrentVersion(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "AppData", "Roaming")
	t.Setenv("APPDATA", configDir)

	cfg := &Config{
		Profiles: map[string]Profile{
			"local": {
				Driver:   "postgres",
				Host:     "localhost",
				Port:     5432,
				User:     "postgres",
				Database: "app",
				Password: "secret",
			},
		},
	}

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if cfg.Version != CurrentConfigVersion {
		t.Fatalf("cfg.Version after save = %d, want %d", cfg.Version, CurrentConfigVersion)
	}

	path, err := GetConfigFilePath()
	if err != nil {
		t.Fatalf("GetConfigFilePath: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(data), "version: 1") {
		t.Fatalf("saved config missing version header:\n%s", string(data))
	}
}
