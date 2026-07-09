package pitr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateBaseBackupIntegrity_Valid(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validate-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a valid backup
	backupID := "20260705_140000"
	backupDir := filepath.Join(tmpDir, backupID)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("Failed to create backup dir: %v", err)
	}

	// Create base.tar.gz
	baseFile := filepath.Join(backupDir, "base.tar.gz")
	if err := os.WriteFile(baseFile, []byte("fake-tar-content"), 0644); err != nil {
		t.Fatalf("Failed to create base.tar.gz: %v", err)
	}

	metadata := &BaseBackupMetadata{
		ID:         backupID,
		StartTime:  time.Now(),
		EndTime:    time.Now().Add(5 * time.Minute),
		Timeline:   1,
		Size:       1024,
		WALStart:   "0/1000000",
		WALEnd:     "0/1500000",
		Checkpoint: "0/1200000",
		Format:     "tar.gz",
		Compressed: true,
		Duration:   300,
	}

	result := ValidateBaseBackupIntegrity(tmpDir, metadata)
	if !result.Valid {
		t.Errorf("Expected valid backup, got errors: %v", result.Errors)
	}
}

func TestValidateBaseBackupIntegrity_MissingBaseArchive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validate-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	backupID := "20260705_140000"
	backupDir := filepath.Join(tmpDir, backupID)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("Failed to create backup dir: %v", err)
	}
	// Don't create base.tar.gz

	metadata := &BaseBackupMetadata{
		ID:         backupID,
		StartTime:  time.Now(),
		Timeline:   1,
		WALStart:   "0/1000000",
		WALEnd:     "0/1500000",
		Compressed: true,
	}

	result := ValidateBaseBackupIntegrity(tmpDir, metadata)
	if result.Valid {
		t.Error("Expected invalid backup due to missing base archive")
	}
	if len(result.Errors) == 0 {
		t.Error("Expected errors for missing base archive")
	}
}

func TestValidateBaseBackupIntegrity_EmptyMetadata(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validate-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	backupID := "20260705_140000"
	backupDir := filepath.Join(tmpDir, backupID)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("Failed to create backup dir: %v", err)
	}

	baseFile := filepath.Join(backupDir, "base.tar.gz")
	if err := os.WriteFile(baseFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create base.tar.gz: %v", err)
	}

	metadata := &BaseBackupMetadata{
		ID:         backupID,
		StartTime:  time.Time{}, // zero
		Timeline:   0,           // zero
		WALStart:   "",          // empty
		WALEnd:     "",          // empty
		Compressed: true,
	}

	result := ValidateBaseBackupIntegrity(tmpDir, metadata)
	if result.Valid {
		t.Error("Expected invalid backup due to empty metadata")
	}
	if len(result.Errors) < 3 {
		t.Errorf("Expected at least 3 errors, got %d", len(result.Errors))
	}
}

func TestValidateWALCoverage_NoWALFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validate-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	backup := &BaseBackupMetadata{
		Timeline: 1,
		WALStart: "0/1",
		WALEnd:   "0/3",
	}

	result := ValidateWALCoverage(tmpDir, backup)
	if result.Valid {
		t.Error("Expected invalid due to no WAL files")
	}
}

func TestFindBestBackup(t *testing.T) {
	backups := []*BaseBackupMetadata{
		{ID: "backup1", StartTime: time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)},
		{ID: "backup2", StartTime: time.Date(2026, 7, 5, 14, 0, 0, 0, time.UTC)},
		{ID: "backup3", StartTime: time.Date(2026, 7, 5, 18, 0, 0, 0, time.UTC)},
	}

	// Target time after all backups
	target := time.Date(2026, 7, 5, 20, 0, 0, 0, time.UTC)
	best := FindBestBackup(backups, target)
	if best == nil || best.ID != "backup3" {
		t.Errorf("Expected backup3, got %v", best)
	}

	// Target time between backup1 and backup2
	target = time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	best = FindBestBackup(backups, target)
	if best == nil || best.ID != "backup1" {
		t.Errorf("Expected backup1, got %v", best)
	}

	// Target time before all backups
	target = time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	best = FindBestBackup(backups, target)
	if best != nil {
		t.Errorf("Expected nil, got %v", best)
	}
}

func TestValidateRestoreTarget(t *testing.T) {
	backupStart := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	walEnd := time.Date(2026, 7, 5, 20, 0, 0, 0, time.UTC)

	// Valid target time
	target := time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC)
	result := ValidateRestoreTarget(target, backupStart, walEnd)
	if !result.Valid {
		t.Errorf("Expected valid, got errors: %v", result.Errors)
	}

	// Target before backup start
	target = time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	result = ValidateRestoreTarget(target, backupStart, walEnd)
	if result.Valid {
		t.Error("Expected invalid for target before backup start")
	}

	// Target after WAL end (warning, not error)
	target = time.Date(2026, 7, 5, 21, 0, 0, 0, time.UTC)
	result = ValidateRestoreTarget(target, backupStart, walEnd)
	if !result.Valid {
		t.Error("Expected valid with warning for target after WAL end")
	}
	if len(result.Warnings) == 0 {
		t.Error("Expected warning for target after WAL end")
	}
}

func TestCheckDiskSpace(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "disk-check-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with small requirement (should pass)
	result := CheckDiskSpace(tmpDir, 1024)
	if !result.Valid {
		t.Errorf("Expected valid for small requirement, got errors: %v", result.Errors)
	}

	// Test with non-existent path
	result = CheckDiskSpace("/non/existent/path", 1024)
	if len(result.Warnings) == 0 {
		t.Error("Expected warning for non-existent path")
	}
}
