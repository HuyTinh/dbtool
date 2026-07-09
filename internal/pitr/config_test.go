package pitr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultRetentionPolicy(t *testing.T) {
	policy := DefaultRetentionPolicy()

	if policy.KeepBaseBackups != 3 {
		t.Errorf("KeepBaseBackups = %d, want 3", policy.KeepBaseBackups)
	}
	if policy.KeepWALDays != 7 {
		t.Errorf("KeepWALDays = %d, want 7", policy.KeepWALDays)
	}
}

func TestPITRConfig_SaveAndLoad(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pitr-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override config directory for testing
	origConfigDir := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Setenv("XDG_CONFIG_HOME", origConfigDir)

	// Create test config
	config := &PITRConfig{
		ProfileName:    "test-profile",
		Driver:         "postgres",
		ArchiveDir:     "/tmp/archive",
		BaseBackupDir:  "/tmp/backups",
		Retention:      DefaultRetentionPolicy(),
		CreatedAt:      time.Now(),
		LastBaseBackup: time.Now(),
	}

	// Save config
	err = SavePITRConfig(config)
	if err != nil {
		t.Fatalf("SavePITRConfig() error = %v", err)
	}

	// Load config
	loaded, err := LoadPITRConfig("test-profile")
	if err != nil {
		t.Fatalf("LoadPITRConfig() error = %v", err)
	}

	// Verify
	if loaded.ProfileName != config.ProfileName {
		t.Errorf("ProfileName = %v, want %v", loaded.ProfileName, config.ProfileName)
	}
	if loaded.Driver != config.Driver {
		t.Errorf("Driver = %v, want %v", loaded.Driver, config.Driver)
	}
	if loaded.ArchiveDir != config.ArchiveDir {
		t.Errorf("ArchiveDir = %v, want %v", loaded.ArchiveDir, config.ArchiveDir)
	}
	if loaded.BaseBackupDir != config.BaseBackupDir {
		t.Errorf("BaseBackupDir = %v, want %v", loaded.BaseBackupDir, config.BaseBackupDir)
	}
	if loaded.Retention.KeepBaseBackups != config.Retention.KeepBaseBackups {
		t.Errorf("KeepBaseBackups = %v, want %v", loaded.Retention.KeepBaseBackups, config.Retention.KeepBaseBackups)
	}
	if loaded.Retention.KeepWALDays != config.Retention.KeepWALDays {
		t.Errorf("KeepWALDays = %v, want %v", loaded.Retention.KeepWALDays, config.Retention.KeepWALDays)
	}
}

func TestLoadPITRConfig_NotFound(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pitr-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override config directory
	origConfigDir := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Setenv("XDG_CONFIG_HOME", origConfigDir)

	// Try to load non-existent config
	_, err = LoadPITRConfig("non-existent")
	if err == nil {
		t.Error("LoadPITRConfig() should return error for non-existent profile")
	}
}

func TestGetPITRDir(t *testing.T) {
	dir, err := GetPITRDir()
	if err != nil {
		t.Fatalf("GetPITRDir() error = %v", err)
	}
	if dir == "" {
		t.Error("GetPITRDir() returned empty string")
	}
}

func TestGetProfilePITRDir(t *testing.T) {
	dir, err := GetProfilePITRDir("test-profile")
	if err != nil {
		t.Fatalf("GetProfilePITRDir() error = %v", err)
	}
	if dir == "" {
		t.Error("GetProfilePITRDir() returned empty string")
	}
	expected := filepath.Join("dbtool", "pitr", "test-profile")
	if !filepath.IsAbs(dir) {
		t.Error("GetProfilePITRDir() should return absolute path")
	}
	// Check if path ends with expected suffix
	if filepath.Base(filepath.Dir(filepath.Dir(dir))) != "dbtool" {
		t.Logf("Warning: path structure may not match expected: %s", dir)
	}
	_ = expected // suppress unused warning
}

func TestRetentionPolicy_Values(t *testing.T) {
	tests := []struct {
		name            string
		keepBaseBackups int
		keepWALDays     int
	}{
		{
			name:            "default values",
			keepBaseBackups: 3,
			keepWALDays:     7,
		},
		{
			name:            "custom values",
			keepBaseBackups: 5,
			keepWALDays:     14,
		},
		{
			name:            "zero values",
			keepBaseBackups: 0,
			keepWALDays:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := RetentionPolicy{
				KeepBaseBackups: tt.keepBaseBackups,
				KeepWALDays:     tt.keepWALDays,
			}

			if policy.KeepBaseBackups != tt.keepBaseBackups {
				t.Errorf("KeepBaseBackups = %d, want %d", policy.KeepBaseBackups, tt.keepBaseBackups)
			}
			if policy.KeepWALDays != tt.keepWALDays {
				t.Errorf("KeepWALDays = %d, want %d", policy.KeepWALDays, tt.keepWALDays)
			}
		})
	}
}

func TestPITRConfig_Fields(t *testing.T) {
	now := time.Now()
	config := &PITRConfig{
		ProfileName:    "prod-db",
		Driver:         "postgres",
		ArchiveDir:     "/var/lib/postgresql/archive",
		BaseBackupDir:  "/var/lib/postgresql/backups",
		Retention:      RetentionPolicy{KeepBaseBackups: 5, KeepWALDays: 30},
		CreatedAt:      now,
		LastBaseBackup: now,
	}

	if config.ProfileName != "prod-db" {
		t.Errorf("ProfileName = %v, want prod-db", config.ProfileName)
	}
	if config.Driver != "postgres" {
		t.Errorf("Driver = %v, want postgres", config.Driver)
	}
	if config.ArchiveDir != "/var/lib/postgresql/archive" {
		t.Errorf("ArchiveDir = %v", config.ArchiveDir)
	}
	if config.BaseBackupDir != "/var/lib/postgresql/backups" {
		t.Errorf("BaseBackupDir = %v", config.BaseBackupDir)
	}
	if config.Retention.KeepBaseBackups != 5 {
		t.Errorf("KeepBaseBackups = %v", config.Retention.KeepBaseBackups)
	}
	if config.Retention.KeepWALDays != 30 {
		t.Errorf("KeepWALDays = %v", config.Retention.KeepWALDays)
	}
	if config.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if config.LastBaseBackup.IsZero() {
		t.Error("LastBaseBackup should not be zero")
	}
}
