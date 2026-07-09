package config

import (
	"strings"
	"testing"
)

func TestDetectConfigVersionTreatsVersionlessProfilesAsLegacy(t *testing.T) {
	data := []byte(`profiles:
  local:
    driver: postgres
`)

	version, err := detectConfigVersion(data)
	if err != nil {
		t.Fatalf("detectConfigVersion: %v", err)
	}
	if version != LegacyConfigVersion {
		t.Fatalf("version = %d, want %d", version, LegacyConfigVersion)
	}
}

func TestDetectConfigVersionReturnsExplicitVersion(t *testing.T) {
	data := []byte(`version: 1
profiles:
  local:
    driver: postgres
`)

	version, err := detectConfigVersion(data)
	if err != nil {
		t.Fatalf("detectConfigVersion: %v", err)
	}
	if version != CurrentConfigVersion {
		t.Fatalf("version = %d, want %d", version, CurrentConfigVersion)
	}
}

func TestMigrateConfigLegacyVersionUpgradesToCurrent(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"local": {Driver: "postgres"},
		},
	}

	changed, err := migrateConfig(cfg, LegacyConfigVersion)
	if err != nil {
		t.Fatalf("migrateConfig: %v", err)
	}
	if !changed {
		t.Fatalf("expected legacy config migration to report changed")
	}
	if cfg.Version != CurrentConfigVersion {
		t.Fatalf("cfg.Version = %d, want %d", cfg.Version, CurrentConfigVersion)
	}
	if cfg.Profiles == nil {
		t.Fatalf("expected profiles map to stay initialized")
	}
}

func TestMigrateConfigReturnsErrorWhenStepMissing(t *testing.T) {
	cfg := &Config{}
	orig := configMigrations
	configMigrations = map[int]migrationFunc{}
	defer func() {
		configMigrations = orig
	}()

	_, err := migrateConfig(cfg, LegacyConfigVersion)
	if err == nil {
		t.Fatalf("expected missing migration error")
	}
	if !strings.Contains(err.Error(), "missing config migration") {
		t.Fatalf("unexpected error: %v", err)
	}
}
