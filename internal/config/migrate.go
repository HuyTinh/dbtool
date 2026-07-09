package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type migrationFunc func(*Config) error

var configMigrations = map[int]migrationFunc{
	LegacyConfigVersion: migrateV0ToV1,
}

type configVersionProbe struct {
	Version  int                  `yaml:"version,omitempty"`
	Profiles map[string]yaml.Node `yaml:"profiles,omitempty"`
}

func detectConfigVersion(data []byte) (int, error) {
	var probe configVersionProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return 0, fmt.Errorf("failed to inspect config version: %w", err)
	}
	if probe.Version != 0 {
		return probe.Version, nil
	}
	if len(probe.Profiles) > 0 {
		return LegacyConfigVersion, nil
	}
	return CurrentConfigVersion, nil
}

func migrateConfig(cfg *Config, from int) (bool, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	changed := false
	version := from
	for version < CurrentConfigVersion {
		migration, ok := configMigrations[version]
		if !ok {
			return changed, fmt.Errorf("missing config migration from version %d", version)
		}
		if err := migration(cfg); err != nil {
			return changed, fmt.Errorf("migrate config v%d->v%d: %w", version, version+1, err)
		}
		version++
		cfg.Version = version
		changed = true
	}

	if cfg.Version == 0 {
		cfg.Version = version
	}
	return changed, nil
}

func migrateV0ToV1(cfg *Config) error {
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	return nil
}
