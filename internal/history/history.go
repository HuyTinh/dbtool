package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dbtool/internal/config"

	"github.com/gofrs/flock"
)

const (
	maxHistorySizeBytes = 10 * 1024 * 1024 // 10MB
	defaultMaxRotated   = 5
	defaultMaxAgeDays   = 30
)

type HistoryRecord struct {
	File      string    `json:"file"`
	Profile   string    `json:"profile"`
	Time      time.Time `json:"time"`
	Success   bool      `json:"success"`
	Command   string    `json:"command"`
	Error     string    `json:"error,omitempty"`
}

func getHistoryFilePath() (string, error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.jsonl"), nil
}

func AppendHistory(record HistoryRecord) error {
	path, err := getHistoryFilePath()
	if err != nil {
		return err
	}

	lockPath := path + ".lock"
	lock := flock.New(lockPath)
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("failed to acquire history file lock: %w", err)
	}
	defer lock.Unlock()

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal history record: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to open history file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write history record: %w", err)
	}

	// Check if rotation is needed
	info, err := f.Stat()
	if err == nil && info.Size() > maxHistorySizeBytes {
		_ = f.Close() // close file before rotating
		_ = rotate(path)
	}

	return nil
}

func rotate(path string) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	// Identify and shift existing history.jsonl.N files
	// First, let's find all files matching history.jsonl.*
	files, err := filepath.Glob(filepath.Join(dir, base+".*"))
	if err != nil {
		return err
	}

	// We only care about numeric suffixes
	nums := make([]int, 0)
	for _, f := range files {
		ext := filepath.Ext(f) // e.g. ".1"
		if len(ext) > 1 {
			if n, err := strconv.Atoi(ext[1:]); err == nil {
				nums = append(nums, n)
			}
		}
	}

	// Sort numeric suffixes in descending order to avoid overwrites during shift
	sort.Sort(sort.Reverse(sort.IntSlice(nums)))

	for _, n := range nums {
		oldPath := filepath.Join(dir, fmt.Sprintf("%s.%d", base, n))
		newPath := filepath.Join(dir, fmt.Sprintf("%s.%d", base, n+1))
		_ = os.Rename(oldPath, newPath)
	}

	// Rename main history.jsonl to history.jsonl.1
	newMainPath := filepath.Join(dir, base+".1")
	_ = os.Rename(path, newMainPath)

	// Clean up older rotated files
	return pruneRotated(dir, defaultMaxRotated, defaultMaxAgeDays)
}

func pruneRotated(dir string, maxFiles int, maxAgeDays int) error {
	files, err := filepath.Glob(filepath.Join(dir, "history.jsonl.*"))
	if err != nil {
		return err
	}

	type rotatedFileInfo struct {
		path    string
		num     int
		modTime time.Time
	}

	var infos []rotatedFileInfo
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)

	for _, f := range files {
		ext := filepath.Ext(f)
		if len(ext) > 1 {
			if n, err := strconv.Atoi(ext[1:]); err == nil {
				info, err := os.Stat(f)
				if err != nil {
					continue
				}
				infos = append(infos, rotatedFileInfo{
					path:    f,
					num:     n,
					modTime: info.ModTime(),
				})
			}
		}
	}

	// Sort by suffix number ascending: 1, 2, 3... (newer first, since smaller number is newer)
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].num < infos[j].num
	})

	for i, info := range infos {
		tooOld := info.modTime.Before(cutoff)
		tooMany := i >= maxFiles // Keep only the first maxFiles newest rotated files
		if tooOld || tooMany {
			_ = os.Remove(info.path)
		}
	}

	return nil
}

type QueryFilter struct {
	Profile string
	Limit   int
	Before  time.Time
	After   time.Time
}

func ReadHistory(filter QueryFilter) ([]HistoryRecord, error) {
	path, err := getHistoryFilePath()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []HistoryRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var rec HistoryRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue // skip malformed lines
		}

		// Apply filters
		if filter.Profile != "" && !strings.EqualFold(rec.Profile, filter.Profile) {
			continue
		}
		if !filter.Before.IsZero() && rec.Time.After(filter.Before) {
			continue
		}
		if !filter.After.IsZero() && rec.Time.Before(filter.After) {
			continue
		}

		records = append(records, rec)
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	// ReadHistory returns newest first, so we reverse the read lines
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}

	// Apply Limit
	if filter.Limit > 0 && len(records) > filter.Limit {
		records = records[:filter.Limit]
	}

	return records, nil
}
