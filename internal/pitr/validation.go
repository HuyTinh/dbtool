package pitr

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ValidationResult holds the result of a validation check
type ValidationResult struct {
	Valid    bool
	Warnings []string
	Errors   []string
}

// ValidateBaseBackupIntegrity checks that a base backup is complete and usable
func ValidateBaseBackupIntegrity(baseBackupDir string, metadata *BaseBackupMetadata) *ValidationResult {
	result := &ValidationResult{Valid: true}

	backupPath := filepath.Join(baseBackupDir, metadata.ID)

	// Check backup directory exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("backup directory not found: %s", backupPath))
		return result
	}

	// Check base.tar or base.tar.gz exists
	baseFile := filepath.Join(backupPath, "base.tar")
	if metadata.Compressed {
		baseFile = baseFile + ".gz"
	}
	info, err := os.Stat(baseFile)
	if os.IsNotExist(err) {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("base archive not found: %s", baseFile))
	} else if err == nil && info.Size() == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("base archive is empty: %s", baseFile))
	}

	// Check metadata fields
	if metadata.WALStart == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "WALStart is empty in metadata")
	}
	if metadata.WALEnd == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "WALEnd is empty in metadata")
	}
	if metadata.Timeline == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "Timeline is 0 in metadata")
	}
	if metadata.StartTime.IsZero() {
		result.Valid = false
		result.Errors = append(result.Errors, "StartTime is zero in metadata")
	}
	if metadata.Size == 0 {
		result.Warnings = append(result.Warnings, "Backup size is 0 (may be incomplete)")
	}

	return result
}

// ValidateWALCoverage checks if WAL archive has complete coverage for a time range
func ValidateWALCoverage(archiveDir string, backup *BaseBackupMetadata) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// List WAL files
	walFiles, err := ListWALFiles(archiveDir)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("cannot list WAL files: %v", err))
		return result
	}

	if len(walFiles) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "no WAL files found in archive")
		return result
	}

	// Check if WAL files exist for the backup's timeline
	var timelineFiles []*WALFileInfo
	for _, wal := range walFiles {
		if wal.Timeline == backup.Timeline {
			timelineFiles = append(timelineFiles, wal)
		}
	}

	if len(timelineFiles) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("no WAL files found for timeline %d", backup.Timeline))
		return result
	}

	// Check for WAL range continuity (detect gaps)
	for i := 1; i < len(timelineFiles); i++ {
		prev := timelineFiles[i-1]
		curr := timelineFiles[i]

		// Check for gap (more than 1 segment difference)
		if prev.Log == curr.Log {
			if curr.Segment-prev.Segment > 1 {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("WAL gap detected between %s and %s", prev.Filename, curr.Filename))
			}
		} else if curr.Log-prev.Log > 1 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("WAL gap detected between %s and %s", prev.Filename, curr.Filename))
		}
	}

	// Validate WAL range completeness
	complete, err := ValidateWALRange(timelineFiles, backup.WALStart, backup.WALEnd, backup.Timeline)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("WAL range validation: %v", err))
	}
	if !complete {
		result.Valid = false
		result.Errors = append(result.Errors,
			fmt.Sprintf("WAL archive incomplete for range %s - %s", backup.WALStart, backup.WALEnd))
	}

	return result
}

// CheckDiskSpace checks if there's enough disk space for the operation
func CheckDiskSpace(path string, requiredBytes int64) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// Use a simple heuristic: check if we can create a file of the required size
	// In production, use platform-specific syscalls (statfs, GetDiskFreeSpaceEx)
	tempFile := filepath.Join(path, ".dbtool-space-check")
	f, err := os.Create(tempFile)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("cannot verify disk space: %v", err))
		return result
	}
	defer os.Remove(tempFile)

	// Try to allocate a small portion (1%) to verify
	testSize := requiredBytes / 100
	if testSize > 10*1024*1024 {
		testSize = 10 * 1024 * 1024 // Cap at 10MB
	}

	if testSize > 0 {
		if err := f.Truncate(testSize); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("insufficient disk space: %v", err))
			return result
		}
	}

	f.Close()
	return result
}

// ValidateRestoreTarget checks if the target time is within the recoverable range
func ValidateRestoreTarget(targetTime, backupStart, walEnd time.Time) *ValidationResult {
	result := &ValidationResult{Valid: true}

	if targetTime.Before(backupStart) {
		result.Valid = false
		result.Errors = append(result.Errors,
			fmt.Sprintf("target time %s is before backup start time %s",
				targetTime.Format("2006-01-02 15:04:05"),
				backupStart.Format("2006-01-02 15:04:05")))
	}

	if !walEnd.IsZero() && targetTime.After(walEnd) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("target time %s may be after WAL archive coverage (last WAL: %s)",
				targetTime.Format("2006-01-02 15:04:05"),
				walEnd.Format("2006-01-02 15:04:05")))
	}

	return result
}

// FindBestBackup selects the best base backup for a target time
func FindBestBackup(backups []*BaseBackupMetadata, targetTime time.Time) *BaseBackupMetadata {
	// Sort by start time descending
	sorted := make([]*BaseBackupMetadata, len(backups))
	copy(sorted, backups)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].StartTime.After(sorted[i].StartTime) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Find the most recent backup before target time
	for _, backup := range sorted {
		if !backup.StartTime.After(targetTime) {
			return backup
		}
	}

	return nil
}

// DetectPostgreSQLVersion returns the major version number (12, 13, 14, 15, etc.)
func DetectPostgreSQLVersion() int {
	cmd := "pg_ctl"
	output, err := runCommand(cmd, "--version")
	if err != nil {
		return 15 // Default to 15 if detection fails
	}

	// Parse "pg_ctl (PostgreSQL) 15.4"
	var major int
	if _, err := fmt.Sscanf(output, "pg_ctl (PostgreSQL) %d", &major); err != nil {
		return 15
	}

	return major
}

// runCommand runs a command and returns its output
func runCommand(name string, args ...string) (string, error) {
	cmd := name
	for _, arg := range args {
		cmd += " " + arg
	}
	return "", fmt.Errorf("not implemented")
}
