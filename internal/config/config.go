package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Profile struct {
	Name     string `yaml:"-"`
	Driver   string `yaml:"driver"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Database string `yaml:"database"`
	Password string `yaml:"password"`
}

type Config struct {
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

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse profiles.yaml: %w", err)
	}

	// Populate name field inside the Profile structs
	for k, v := range cfg.Profiles {
		v.Name = k
		cfg.Profiles[k] = v
	}

	return cfg, nil
}

func SaveConfig(cfg *Config) error {
	path, err := GetConfigFilePath()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
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
	c.Profiles[name] = p
	return SaveConfig(c)
}
