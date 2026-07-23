package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	LegacyConfigVersion  = 0
	CurrentConfigVersion = 2
)

type Profile struct {
	Name                string          `yaml:"-"`
	Driver              string          `yaml:"driver"`
	Host                string          `yaml:"host"`
	Port                int             `yaml:"port"`
	User                string          `yaml:"user"`
	Database            string          `yaml:"database"`
	Password            string          `yaml:"password,omitempty"`
	PasswordRef         string          `yaml:"password_ref,omitempty"`
	PasswordEncrypted   string          `yaml:"password_encrypted,omitempty"`
	PasswordSalt        string          `yaml:"password_salt,omitempty"`
	PasswordNonce       string          `yaml:"password_nonce,omitempty"`
	PasswordAlgorithm   string          `yaml:"password_algorithm,omitempty"`
	RestoreDrillSandbox bool            `yaml:"restore_drill_sandbox,omitempty"`
	Runtime             *RuntimeProfile `yaml:"runtime,omitempty"`
}

type RuntimeProfile struct {
	Type        string         `yaml:"type,omitempty"`
	Source      string         `yaml:"source,omitempty"`
	SourceFile  string         `yaml:"source_file,omitempty"`
	ServiceName string         `yaml:"service_name,omitempty"`
	Container   string         `yaml:"container,omitempty"`
	Mounts      []MountMapping `yaml:"mounts,omitempty"`
	Paths       RuntimePaths   `yaml:"paths,omitempty"`
}

type MountMapping struct {
	Type   string `yaml:"type,omitempty"`
	Source string `yaml:"source,omitempty"`
	Target string `yaml:"target,omitempty"`
}

type RuntimePaths struct {
	DataDirectory string `yaml:"data_directory,omitempty"`
	HBAFile       string `yaml:"hba_file,omitempty"`
	HostHBAFile   string `yaml:"host_hba_file,omitempty"`
}

type Config struct {
	Version  int                `yaml:"version,omitempty"`
	Profiles map[string]Profile `yaml:"profiles"`
}

func GetConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "dbtool")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func GetConfigFilePath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "profiles.yaml"), nil
}

func LoadConfig() (*Config, error) {
	path, err := GetConfigFilePath()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Version:  CurrentConfigVersion,
		Profiles: make(map[string]Profile),
	}

	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return cfg, nil
	} else if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	version, err := detectConfigVersion(data)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse profiles.yaml: %w", err)
	}
	if _, err := migrateConfig(cfg, version); err != nil {
		return nil, err
	}

	// Populate name field inside the Profile structs
	for k, v := range cfg.Profiles {
		v.Name = k
		cfg.Profiles[k] = v
	}
	if err := ResolveProfileSecrets(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func SaveConfig(cfg *Config) error {
	path, err := GetConfigFilePath()
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.Version == LegacyConfigVersion {
		cfg.Version = CurrentConfigVersion
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}

	configToSave := *cfg
	configToSave.Profiles = make(map[string]Profile, len(cfg.Profiles))
	for name, profile := range cfg.Profiles {
		if profile.PasswordRef != "" || profile.PasswordEncrypted != "" {
			profile.Password = ""
		}
		configToSave.Profiles[name] = profile
	}

	data, err := yaml.Marshal(&configToSave)
	if err != nil {
		return err
	}

	// Ensure private file permissions: 0600
	return os.WriteFile(path, data, 0600)
}

func (c *Config) GetProfile(name string) (Profile, bool) {
	p, ok := c.Profiles[name]
	return p, ok
}

func (c *Config) SaveProfile(name string, p Profile) error {
	if c.Profiles == nil {
		c.Profiles = make(map[string]Profile)
	}
	if err := StoreProfilePassword(name, &p); err != nil {
		return err
	}
	c.Profiles[name] = p
	return SaveConfig(c)
}
