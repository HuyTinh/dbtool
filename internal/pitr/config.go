package pitr

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// PITRConfig holds PITR configuration for a profile
type PITRConfig struct {
	ProfileName    string          `yaml:"profile_name"`
	Driver         string          `yaml:"driver"` // only "postgres"
	ArchiveDir     string          `yaml:"archive_dir"`
	BaseBackupDir  string          `yaml:"base_backup_dir"`
	Retention      RetentionPolicy `yaml:"retention"`
	CreatedAt      time.Time       `yaml:"created_at"`
	LastBaseBackup time.Time       `yaml:"last_base_backup"`
}

// RetentionPolicy defines how long to keep backups and WAL files
type RetentionPolicy struct {
	KeepBaseBackups int `yaml:"keep_base_backups"` // default: 3
	KeepWALDays     int `yaml:"keep_wal_days"`     // default: 7
}

// BaseBackupMetadata stores metadata about a base backup
type BaseBackupMetadata struct {
	ID         string    `json:"id"`            // Format: 20060102_150405
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	Timeline   int       `json:"timeline"`
	Size       int64     `json:"size"`       // bytes
	WALStart   string    `json:"wal_start"`  // LSN: "0/1000000"
	WALEnd     string    `json:"wal_end"`    // LSN: "0/1500000"
	Checkpoint string    `json:"checkpoint"` // LSN
	Format     string    `json:"format"`     // "tar.gz"
	Compressed bool      `json:"compressed"`
	Duration   int       `json:"duration_sec"`
}

// WALFileInfo stores information about a WAL file
type WALFileInfo struct {
	Filename string    // "000000010000000000000001"
	Timeline int       // 1
	Log      int       // 0
	Segment  int       // 1
	FullPath string
	Size     int64
	ModTime  time.Time
}

// GetPITRDir returns the base PITR directory
func GetPITRDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot get user config directory: %w", err)
	}
	return filepath.Join(configDir, "dbtool", "pitr"), nil
}

// GetProfilePITRDir returns the PITR directory for a specific profile
func GetProfilePITRDir(profileName string) (string, error) {
	pitrDir, err := GetPITRDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(pitrDir, profileName), nil
}

// LoadPITRConfig loads PITR configuration for a profile
func LoadPITRConfig(profileName string) (*PITRConfig, error) {
	profileDir, err := GetProfilePITRDir(profileName)
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(profileDir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("PITR not configured for profile '%s' (run: dbtool pitr setup --profile %s)", profileName, profileName)
		}
		return nil, fmt.Errorf("cannot read PITR config: %w", err)
	}

	var config PITRConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("cannot parse PITR config: %w", err)
	}

	return &config, nil
}

// SavePITRConfig saves PITR configuration for a profile
func SavePITRConfig(config *PITRConfig) error {
	profileDir, err := GetProfilePITRDir(config.ProfileName)
	if err != nil {
		return err
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return fmt.Errorf("cannot create PITR directory: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("cannot marshal PITR config: %w", err)
	}

	configPath := filepath.Join(profileDir, "config.yaml")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("cannot write PITR config: %w", err)
	}

	return nil
}

// DefaultRetentionPolicy returns the default retention policy
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		KeepBaseBackups: 3,
		KeepWALDays:     7,
	}
}
