package pitr

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestBaseBackupMetadata_Fields(t *testing.T) {
	startTime := time.Date(2026, 7, 5, 14, 0, 0, 0, time.UTC)
	endTime := startTime.Add(5 * time.Minute)

	metadata := &BaseBackupMetadata{
		ID:         "20260705_140000",
		StartTime:  startTime,
		EndTime:    endTime,
		Timeline:   1,
		Size:       1073741824, // 1 GB
		WALStart:   "0/1000000",
		WALEnd:     "0/1500000",
		Checkpoint: "0/1200000",
		Format:     "tar.gz",
		Compressed: true,
		Duration:   300,
	}

	if metadata.ID != "20260705_140000" {
		t.Errorf("ID = %v", metadata.ID)
	}
	if metadata.Timeline != 1 {
		t.Errorf("Timeline = %v", metadata.Timeline)
	}
	if metadata.Size != 1073741824 {
		t.Errorf("Size = %v", metadata.Size)
	}
	if metadata.WALStart != "0/1000000" {
		t.Errorf("WALStart = %v", metadata.WALStart)
	}
	if metadata.WALEnd != "0/1500000" {
		t.Errorf("WALEnd = %v", metadata.WALEnd)
	}
	if metadata.Checkpoint != "0/1200000" {
		t.Errorf("Checkpoint = %v", metadata.Checkpoint)
	}
	if metadata.Format != "tar.gz" {
		t.Errorf("Format = %v", metadata.Format)
	}
	if !metadata.Compressed {
		t.Error("Compressed should be true")
	}
	if metadata.Duration != 300 {
		t.Errorf("Duration = %v", metadata.Duration)
	}
}

func TestSaveAndLoadBackupMetadata(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "backup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	metadata := &BaseBackupMetadata{
		ID:         "20260705_140000",
		StartTime:  time.Now(),
		EndTime:    time.Now().Add(5 * time.Minute),
		Timeline:   1,
		Size:       1073741824,
		WALStart:   "0/1000000",
		WALEnd:     "0/1500000",
		Checkpoint: "0/1200000",
		Format:     "tar.gz",
		Compressed: true,
		Duration:   300,
	}

	// Save metadata
	err = SaveBackupMetadata(tmpDir, metadata)
	if err != nil {
		t.Fatalf("SaveBackupMetadata() error = %v", err)
	}

	// Verify metadata.json exists
	metadataPath := filepath.Join(tmpDir, "metadata.json")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Fatal("metadata.json not created")
	}

	// Load metadata
	loaded, err := LoadBackupMetadata(tmpDir)
	if err != nil {
		t.Fatalf("LoadBackupMetadata() error = %v", err)
	}

	// Verify fields
	if loaded.ID != metadata.ID {
		t.Errorf("ID = %v, want %v", loaded.ID, metadata.ID)
	}
	if loaded.Timeline != metadata.Timeline {
		t.Errorf("Timeline = %v, want %v", loaded.Timeline, metadata.Timeline)
	}
	if loaded.Size != metadata.Size {
		t.Errorf("Size = %v, want %v", loaded.Size, metadata.Size)
	}
	if loaded.WALStart != metadata.WALStart {
		t.Errorf("WALStart = %v, want %v", loaded.WALStart, metadata.WALStart)
	}
	if loaded.WALEnd != metadata.WALEnd {
		t.Errorf("WALEnd = %v, want %v", loaded.WALEnd, metadata.WALEnd)
	}
	if loaded.Checkpoint != metadata.Checkpoint {
		t.Errorf("Checkpoint = %v, want %v", loaded.Checkpoint, metadata.Checkpoint)
	}
	if loaded.Format != metadata.Format {
		t.Errorf("Format = %v, want %v", loaded.Format, metadata.Format)
	}
	if loaded.Compressed != metadata.Compressed {
		t.Errorf("Compressed = %v, want %v", loaded.Compressed, metadata.Compressed)
	}
	if loaded.Duration != metadata.Duration {
		t.Errorf("Duration = %v, want %v", loaded.Duration, metadata.Duration)
	}
}

func TestLoadBackupMetadata_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "backup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = LoadBackupMetadata(tmpDir)
	if err == nil {
		t.Error("LoadBackupMetadata() should return error for non-existent metadata")
	}
}

func TestListBackups(t *testing.T) {
	// Create temp directory with multiple backups
	tmpDir, err := os.MkdirTemp("", "backups-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create backup directories
	backups := []struct {
		id       string
		timeline int
	}{
		{"20260705_140000", 1},
		{"20260705_150000", 1},
		{"20260705_160000", 2},
	}

	for _, b := range backups {
		backupDir := filepath.Join(tmpDir, b.id)
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			t.Fatalf("Failed to create backup dir: %v", err)
		}

		metadata := &BaseBackupMetadata{
			ID:        b.id,
			StartTime: time.Now(),
			EndTime:   time.Now().Add(5 * time.Minute),
			Timeline:  b.timeline,
			Size:      1073741824,
			WALStart:  "0/1000000",
			WALEnd:    "0/1500000",
			Format:    "tar.gz",
		}

		if err := SaveBackupMetadata(backupDir, metadata); err != nil {
			t.Fatalf("Failed to save metadata: %v", err)
		}
	}

	// Create invalid directory (should be skipped)
	invalidDir := filepath.Join(tmpDir, "invalid-name")
	if err := os.MkdirAll(invalidDir, 0755); err != nil {
		t.Fatalf("Failed to create invalid dir: %v", err)
	}

	// List backups
	listed, err := ListBackups(tmpDir)
	if err != nil {
		t.Fatalf("ListBackups() error = %v", err)
	}

	if len(listed) != 3 {
		t.Errorf("ListBackups() returned %d backups, want 3", len(listed))
	}

	// Verify backups are loaded
	for _, b := range listed {
		if b.ID == "" {
			t.Error("Backup ID should not be empty")
		}
		if b.Timeline == 0 {
			t.Error("Backup Timeline should not be zero")
		}
	}
}

func TestListBackups_Empty(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "backups-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	backups, err := ListBackups(tmpDir)
	if err != nil {
		t.Fatalf("ListBackups() error = %v", err)
	}

	if len(backups) != 0 {
		t.Errorf("ListBackups() returned %d backups, want 0", len(backups))
	}
}

func TestBackupSorting(t *testing.T) {
	backups := []*BaseBackupMetadata{
		{ID: "20260705_160000", StartTime: time.Date(2026, 7, 5, 16, 0, 0, 0, time.UTC)},
		{ID: "20260705_140000", StartTime: time.Date(2026, 7, 5, 14, 0, 0, 0, time.UTC)},
		{ID: "20260705_150000", StartTime: time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC)},
	}

	// Sort by start time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].StartTime.After(backups[j].StartTime)
	})

	if backups[0].ID != "20260705_160000" {
		t.Errorf("First backup should be newest, got %s", backups[0].ID)
	}
	if backups[2].ID != "20260705_140000" {
		t.Errorf("Last backup should be oldest, got %s", backups[2].ID)
	}
}

func TestBaseBackupMetadata_SizeFormatting(t *testing.T) {
	tests := []struct {
		size     int64
		expected string
	}{
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
	}

	for _, tt := range tests {
		metadata := &BaseBackupMetadata{Size: tt.size}
		if metadata.Size != tt.size {
			t.Errorf("Size = %v, want %v", metadata.Size, tt.size)
		}
	}
}

func TestBaseBackupSuggestion_PgHBAReplicationFailure(t *testing.T) {
	stderr := `pg_basebackup: error: connection to server at "localhost" (::1), port 5447 failed: FATAL:  no pg_hba.conf entry for replication connection from host "172.23.0.1", user "postgres", no encryption`

	suggestion := BaseBackupSuggestion(stderr)

	for _, want := range []string{
		"pg_hba.conf",
		"replication",
		"postgres",
		"172.23.0.1/32",
		"SELECT pg_reload_conf();",
	} {
		if !strings.Contains(suggestion, want) {
			t.Fatalf("BaseBackupSuggestion() = %q, want it to contain %q", suggestion, want)
		}
	}
}

func TestBaseBackupSuggestion_ReplicationPrivilegeFailure(t *testing.T) {
	stderr := `pg_basebackup: error: must be superuser or replication role to start walsender`

	suggestion := BaseBackupSuggestion(stderr)

	for _, want := range []string{
		"REPLICATION privilege",
		"ALTER ROLE",
	} {
		if !strings.Contains(suggestion, want) {
			t.Fatalf("BaseBackupSuggestion() = %q, want it to contain %q", suggestion, want)
		}
	}
}

func TestBaseBackupSuggestion_UnrelatedError(t *testing.T) {
	stderr := `pg_basebackup: error: could not create directory "backup": Permission denied`

	if suggestion := BaseBackupSuggestion(stderr); suggestion != "" {
		t.Fatalf("BaseBackupSuggestion() = %q, want empty suggestion", suggestion)
	}
}

func TestReplicationPgHBARule_IPv4(t *testing.T) {
	rule := ReplicationPgHBARule("postgres", "172.23.0.1", "scram-sha-256")
	want := "host    replication     postgres        172.23.0.1/32        scram-sha-256"

	if rule != want {
		t.Fatalf("ReplicationPgHBARule() = %q, want %q", rule, want)
	}
}

func TestReplicationPgHBARule_LeavesCIDRAddressAlone(t *testing.T) {
	rule := ReplicationPgHBARule("backup", "172.23.0.0/16", "md5")
	want := "host    replication     backup          172.23.0.0/16        md5"

	if rule != want {
		t.Fatalf("ReplicationPgHBARule() = %q, want %q", rule, want)
	}
}

func TestReplicationPgHBARule_UnknownAddress(t *testing.T) {
	rule := ReplicationPgHBARule("postgres", "", "")
	want := "host    replication     postgres        <client-ip>/32        scram-sha-256"

	if rule != want {
		t.Fatalf("ReplicationPgHBARule() = %q, want %q", rule, want)
	}
}

func TestEnsureReplicationPgHBA_AppendsRuleAndCreatesBackup(t *testing.T) {
	tmpDir := t.TempDir()
	hbaPath := filepath.Join(tmpDir, "pg_hba.conf")
	original := "# TYPE  DATABASE        USER            ADDRESS                 METHOD\nlocal   all             all                                     trust\n"
	if err := os.WriteFile(hbaPath, []byte(original), 0644); err != nil {
		t.Fatalf("write pg_hba.conf: %v", err)
	}

	rule := ReplicationPgHBARule("postgres", "172.23.0.1", "scram-sha-256")
	result, err := EnsureReplicationPgHBA(hbaPath, rule, time.Date(2026, 7, 6, 20, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("EnsureReplicationPgHBA() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("EnsureReplicationPgHBA() Changed = false, want true")
	}
	if result.BackupPath == "" {
		t.Fatal("EnsureReplicationPgHBA() BackupPath is empty")
	}

	updated, err := os.ReadFile(hbaPath)
	if err != nil {
		t.Fatalf("read updated pg_hba.conf: %v", err)
	}
	if !strings.Contains(string(updated), rule) {
		t.Fatalf("updated pg_hba.conf = %q, want it to contain %q", string(updated), rule)
	}

	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatalf("read backup pg_hba.conf: %v", err)
	}
	if string(backup) != original {
		t.Fatalf("backup = %q, want original %q", string(backup), original)
	}
}

func TestEnsureReplicationPgHBA_AcceptsDataDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	hbaPath := filepath.Join(tmpDir, "pg_hba.conf")
	original := "# existing config\n"
	if err := os.WriteFile(hbaPath, []byte(original), 0644); err != nil {
		t.Fatalf("write pg_hba.conf: %v", err)
	}

	rule := ReplicationPgHBARule("postgres", "172.23.0.1", "scram-sha-256")
	result, err := EnsureReplicationPgHBA(tmpDir, rule, time.Date(2026, 7, 6, 20, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("EnsureReplicationPgHBA() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("EnsureReplicationPgHBA() Changed = false, want true")
	}
	if !strings.Contains(result.BackupPath, "pg_hba.conf.dbtool-backup-") {
		t.Fatalf("BackupPath = %q, want pg_hba.conf backup path", result.BackupPath)
	}

	updated, err := os.ReadFile(hbaPath)
	if err != nil {
		t.Fatalf("read updated pg_hba.conf: %v", err)
	}
	if !strings.Contains(string(updated), rule) {
		t.Fatalf("updated pg_hba.conf = %q, want it to contain %q", string(updated), rule)
	}
}

func TestEnsureReplicationPgHBA_IsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	hbaPath := filepath.Join(tmpDir, "pg_hba.conf")
	rule := ReplicationPgHBARule("postgres", "172.23.0.1", "scram-sha-256")
	content := "# existing config\n" + rule + "\n"
	if err := os.WriteFile(hbaPath, []byte(content), 0644); err != nil {
		t.Fatalf("write pg_hba.conf: %v", err)
	}

	result, err := EnsureReplicationPgHBA(hbaPath, rule, time.Date(2026, 7, 6, 20, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("EnsureReplicationPgHBA() error = %v", err)
	}
	if result.Changed {
		t.Fatal("EnsureReplicationPgHBA() Changed = true, want false")
	}
	if result.BackupPath != "" {
		t.Fatalf("EnsureReplicationPgHBA() BackupPath = %q, want empty", result.BackupPath)
	}

	updated, err := os.ReadFile(hbaPath)
	if err != nil {
		t.Fatalf("read pg_hba.conf: %v", err)
	}
	if string(updated) != content {
		t.Fatalf("pg_hba.conf changed unexpectedly: %q", string(updated))
	}
}
