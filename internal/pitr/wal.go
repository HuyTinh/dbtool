package pitr

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// WAL filename format: 000000010000000000000001 (24 hex characters)
// - First 8 chars: timeline ID (e.g., 00000001)
// - Next 8 chars: log ID (e.g., 00000000)
// - Last 8 chars: segment ID (e.g., 00000001)

var walFilenameRegex = regexp.MustCompile(`^[0-9A-Fa-f]{24}$`)

// ParseWALFilename parses a WAL filename into its components
func ParseWALFilename(filename string) (*WALFileInfo, error) {
	if !walFilenameRegex.MatchString(filename) {
		return nil, fmt.Errorf("invalid WAL filename format: %s", filename)
	}

	timelineHex := filename[0:8]
	logHex := filename[8:16]
	segmentHex := filename[16:24]

	timeline, err := strconv.ParseInt(timelineHex, 16, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot parse timeline: %w", err)
	}

	log, err := strconv.ParseInt(logHex, 16, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot parse log: %w", err)
	}

	segment, err := strconv.ParseInt(segmentHex, 16, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot parse segment: %w", err)
	}

	return &WALFileInfo{
		Filename: filename,
		Timeline: int(timeline),
		Log:      int(log),
		Segment:  int(segment),
	}, nil
}

// FormatWALFilename formats WAL components into a filename
func FormatWALFilename(timeline, log, segment int) string {
	return fmt.Sprintf("%08X%08X%08X", timeline, log, segment)
}

// CompareWALFiles compares two WAL files by their LSN
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func CompareWALFiles(a, b *WALFileInfo) int {
	if a.Timeline != b.Timeline {
		if a.Timeline < b.Timeline {
			return -1
		}
		return 1
	}

	if a.Log != b.Log {
		if a.Log < b.Log {
			return -1
		}
		return 1
	}

	if a.Segment != b.Segment {
		if a.Segment < b.Segment {
			return -1
		}
		return 1
	}

	return 0
}

// ParseLSN parses an LSN string like "0/1000000" into timeline and segment
func ParseLSN(lsn string) (log, segment int, err error) {
	parts := strings.Split(lsn, "/")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid LSN format: %s", lsn)
	}

	logVal, err := strconv.ParseInt(parts[0], 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot parse log part: %w", err)
	}

	segmentVal, err := strconv.ParseInt(parts[1], 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot parse segment part: %w", err)
	}

	return int(logVal), int(segmentVal), nil
}

// FormatLSN formats log and segment into an LSN string
func FormatLSN(log, segment int) string {
	return fmt.Sprintf("%X/%X", log, segment)
}

// ListWALFiles lists all WAL files in a directory
func ListWALFiles(archiveDir string) ([]*WALFileInfo, error) {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read archive directory: %w", err)
	}

	var walFiles []*WALFileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if !walFilenameRegex.MatchString(filename) {
			continue
		}

		info, err := ParseWALFilename(filename)
		if err != nil {
			continue // skip invalid files
		}

		stat, err := entry.Info()
		if err != nil {
			continue
		}

		info.FullPath = filepath.Join(archiveDir, filename)
		info.Size = stat.Size()
		info.ModTime = stat.ModTime()

		walFiles = append(walFiles, info)
	}

	// Sort by timeline, log, segment
	sort.Slice(walFiles, func(i, j int) bool {
		return CompareWALFiles(walFiles[i], walFiles[j]) < 0
	})

	return walFiles, nil
}

// ValidateWALRange validates that a range of WAL files is complete
// from startWAL to endWAL (inclusive)
func ValidateWALRange(walFiles []*WALFileInfo, startWAL, endWAL string, timeline int) (bool, error) {
	if len(walFiles) == 0 {
		return false, fmt.Errorf("no WAL files available")
	}

	startLog, startSegment, err := ParseLSN(startWAL)
	if err != nil {
		return false, fmt.Errorf("cannot parse start WAL: %w", err)
	}

	endLog, endSegment, err := ParseLSN(endWAL)
	if err != nil {
		return false, fmt.Errorf("cannot parse end WAL: %w", err)
	}

	start := &WALFileInfo{Timeline: timeline, Log: startLog, Segment: startSegment}
	end := &WALFileInfo{Timeline: timeline, Log: endLog, Segment: endSegment}

	// Filter WAL files by timeline
	var filtered []*WALFileInfo
	for _, wal := range walFiles {
		if wal.Timeline == timeline {
			filtered = append(filtered, wal)
		}
	}

	if len(filtered) == 0 {
		return false, fmt.Errorf("no WAL files found for timeline %d", timeline)
	}

	// Check if we have all files from start to end
	expected := start
	for {
		found := false
		for _, wal := range filtered {
			if CompareWALFiles(wal, expected) == 0 {
				found = true
				break
			}
		}

		if !found {
			return false, fmt.Errorf("missing WAL file: %s", FormatWALFilename(expected.Timeline, expected.Log, expected.Segment))
		}

		// Check if we've reached the end
		if CompareWALFiles(expected, end) >= 0 {
			break
		}

		// Move to next segment
		expected.Segment++
		if expected.Segment >= 256 { // WAL segments are 0-255 per log
			expected.Segment = 0
			expected.Log++
		}
	}

	return true, nil
}

// FindWALFilesInRange finds all WAL files in a given range
func FindWALFilesInRange(walFiles []*WALFileInfo, startWAL, endWAL string, timeline int) ([]*WALFileInfo, error) {
	startLog, startSegment, err := ParseLSN(startWAL)
	if err != nil {
		return nil, fmt.Errorf("cannot parse start WAL: %w", err)
	}

	endLog, endSegment, err := ParseLSN(endWAL)
	if err != nil {
		return nil, fmt.Errorf("cannot parse end WAL: %w", err)
	}

	start := &WALFileInfo{Timeline: timeline, Log: startLog, Segment: startSegment}
	end := &WALFileInfo{Timeline: timeline, Log: endLog, Segment: endSegment}

	var result []*WALFileInfo
	for _, wal := range walFiles {
		if wal.Timeline != timeline {
			continue
		}

		if CompareWALFiles(wal, start) >= 0 && CompareWALFiles(wal, end) <= 0 {
			result = append(result, wal)
		}
	}

	return result, nil
}
