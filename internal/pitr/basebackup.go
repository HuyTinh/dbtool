package pitr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dbtool/internal/config"
)

// BackupOptions holds options for running pg_basebackup
type BackupOptions struct {
	Profile      config.Profile
	OutputDir    string
	Jobs         int
	Checkpoint   string // "fast" or "spread"
	NoCompress   bool
	Verbose      bool
}

// RunBaseBackup executes pg_basebackup and returns metadata
func RunBaseBackup(ctx context.Context, opts BackupOptions) (*BaseBackupMetadata, error) {
	args := []string{
		"-h", opts.Profile.Host,
		"-p", fmt.Sprint(opts.Profile.Port),
		"-U", opts.Profile.User,
		"-D", opts.OutputDir,
		"-Ft",     // tar format
		"-Xfetch", // fetch WAL during backup
		"-P",      // progress
	}

	if opts.Checkpoint != "" {
		args = append(args, "--checkpoint="+opts.Checkpoint)
	} else {
		args = append(args, "--checkpoint=fast")
	}

	if !opts.NoCompress {
		args = append(args, "-z") // gzip compression
	}

	if opts.Jobs > 1 {
		args = append(args, "-j", fmt.Sprint(opts.Jobs))
	}

	cmd := exec.CommandContext(ctx, "pg_basebackup", args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+opts.Profile.Password)

	if opts.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = nil
		cmd.Stderr = nil
	}

	startTime := time.Now()
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pg_basebackup failed: %w", err)
	}
	duration := int(time.Since(startTime).Seconds())

	// Parse metadata from backup_label
	metadata, err := parseBackupLabel(opts.OutputDir, opts.NoCompress)
	if err != nil {
		return nil, fmt.Errorf("cannot parse backup metadata: %w", err)
	}

	metadata.EndTime = time.Now()
	metadata.Duration = duration
	metadata.Format = "tar.gz"
	if opts.NoCompress {
		metadata.Format = "tar"
	}
	metadata.Compressed = !opts.NoCompress

	// Calculate total size
	size, err := calculateBackupSize(opts.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("cannot calculate backup size: %w", err)
	}
	metadata.Size = size

	// Find WALEnd from pg_wal.tar.gz
	walEnd, err := findWALEnd(opts.OutputDir, opts.NoCompress)
	if err != nil {
		// Not critical, use WALStart as fallback
		metadata.WALEnd = metadata.WALStart
	} else {
		metadata.WALEnd = walEnd
	}

	return metadata, nil
}

// parseBackupLabel parses the backup_label file from the backup
func parseBackupLabel(backupDir string, noCompress bool) (*BaseBackupMetadata, error) {
	baseFile := filepath.Join(backupDir, "base.tar")
	if !noCompress {
		baseFile = baseFile + ".gz"
	}

	// Extract backup_label from tar
	labelContent, err := extractFileFromTar(baseFile, "backup_label", noCompress)
	if err != nil {
		return nil, fmt.Errorf("cannot extract backup_label: %w", err)
	}

	metadata := &BaseBackupMetadata{}
	scanner := bufio.NewScanner(strings.NewReader(labelContent))

	walRegex := regexp.MustCompile(`START WAL LOCATION: (.+) \(file (.+)\)`)
	checkpointRegex := regexp.MustCompile(`CHECKPOINT LOCATION: (.+)`)
	startTimeRegex := regexp.MustCompile(`START TIME: (.+)`)
	timelineRegex := regexp.MustCompile(`START TIMELINE: (\d+)`)

	for scanner.Scan() {
		line := scanner.Text()

		if matches := walRegex.FindStringSubmatch(line); len(matches) == 3 {
			metadata.WALStart = matches[1]
		} else if matches := checkpointRegex.FindStringSubmatch(line); len(matches) == 2 {
			metadata.Checkpoint = matches[1]
		} else if matches := startTimeRegex.FindStringSubmatch(line); len(matches) == 2 {
			t, err := time.Parse("2006-01-02 15:04:05 MST", matches[1])
			if err != nil {
				// Try without timezone
				t, err = time.Parse("2006-01-02 15:04:05", matches[1])
				if err != nil {
					continue
				}
			}
			metadata.StartTime = t
		} else if matches := timelineRegex.FindStringSubmatch(line); len(matches) == 2 {
			timeline, err := strconv.Atoi(matches[1])
			if err == nil {
				metadata.Timeline = timeline
			}
		}
	}

	if metadata.WALStart == "" || metadata.StartTime.IsZero() {
		return nil, fmt.Errorf("incomplete backup_label content")
	}

	return metadata, nil
}

// extractFileFromTar extracts a specific file from a tar archive
func extractFileFromTar(tarPath, filename string, compressed bool) (string, error) {
	var cmd *exec.Cmd
	if compressed {
		// gunzip and tar
		cmd = exec.Command("sh", "-c", fmt.Sprintf("gunzip -c %s | tar -xf - -O %s", tarPath, filename))
	} else {
		cmd = exec.Command("tar", "-xf", tarPath, "-O", filename)
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("cannot extract %s from tar: %w", filename, err)
	}

	return string(output), nil
}

// calculateBackupSize calculates the total size of the backup directory
func calculateBackupSize(backupDir string) (int64, error) {
	var size int64
	err := filepath.Walk(backupDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// findWALEnd finds the last WAL file in pg_wal.tar.gz
func findWALEnd(backupDir string, noCompress bool) (string, error) {
	walFile := filepath.Join(backupDir, "pg_wal.tar")
	if !noCompress {
		walFile = walFile + ".gz"
	}

	// Check if pg_wal.tar.gz exists
	if _, err := os.Stat(walFile); os.IsNotExist(err) {
		return "", fmt.Errorf("pg_wal.tar.gz not found")
	}

	// List files in pg_wal.tar.gz
	var cmd *exec.Cmd
	if noCompress {
		cmd = exec.Command("tar", "-tf", walFile)
	} else {
		cmd = exec.Command("sh", "-c", fmt.Sprintf("gunzip -c %s | tar -tf -", walFile))
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("cannot list pg_wal contents: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("no WAL files in pg_wal.tar.gz")
	}

	// Find the last WAL file
	var lastWAL string
	for _, line := range lines {
		filename := filepath.Base(line)
		if walFilenameRegex.MatchString(filename) {
			if lastWAL == "" || filename > lastWAL {
				lastWAL = filename
			}
		}
	}

	if lastWAL == "" {
		return "", fmt.Errorf("no valid WAL files found in pg_wal.tar.gz")
	}

	// Convert filename to LSN format
	info, err := ParseWALFilename(lastWAL)
	if err != nil {
		return "", err
	}

	return FormatLSN(info.Log, info.Segment), nil
}

// SaveBackupMetadata saves backup metadata to a JSON file
func SaveBackupMetadata(backupDir string, metadata *BaseBackupMetadata) error {
	metadata.ID = filepath.Base(backupDir)

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal metadata: %w", err)
	}

	metadataPath := filepath.Join(backupDir, "metadata.json")
	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("cannot write metadata: %w", err)
	}

	return nil
}

// LoadBackupMetadata loads backup metadata from a JSON file
func LoadBackupMetadata(backupDir string) (*BaseBackupMetadata, error) {
	metadataPath := filepath.Join(backupDir, "metadata.json")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read metadata: %w", err)
	}

	var metadata BaseBackupMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("cannot parse metadata: %w", err)
	}

	return &metadata, nil
}

// ListBackups lists all base backups in the base backup directory
func ListBackups(baseBackupDir string) ([]*BaseBackupMetadata, error) {
	entries, err := os.ReadDir(baseBackupDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read base backup directory: %w", err)
	}

	var backups []*BaseBackupMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory name matches backup ID format (YYYYMMDD_HHMMSS)
		if !regexp.MustCompile(`^\d{8}_\d{6}$`).MatchString(entry.Name()) {
			continue
		}

		backupDir := filepath.Join(baseBackupDir, entry.Name())
		metadata, err := LoadBackupMetadata(backupDir)
		if err != nil {
			continue // skip backups without metadata
		}

		backups = append(backups, metadata)
	}

	return backups, nil
}
