package pitr

import (
	"testing"
	"time"
)

func TestParseWALFilename(t *testing.T) {
	tests := []struct {
		name      string
		filename  string
		wantErr   bool
		timeline  int
		log       int
		segment   int
	}{
		{
			name:     "valid WAL filename",
			filename: "000000010000000000000001",
			wantErr:  false,
			timeline: 1,
			log:      0,
			segment:  1,
		},
		{
			name:     "valid WAL filename with high values",
			filename: "00000002000000010000000A",
			wantErr:  false,
			timeline: 2,
			log:      1,
			segment:  10,
		},
		{
			name:     "valid WAL filename uppercase",
			filename: "000000010000000A000000FF",
			wantErr:  false,
			timeline: 1,
			log:      10,
			segment:  255,
		},
		{
			name:     "too short",
			filename: "0000000100000000",
			wantErr:  true,
		},
		{
			name:     "too long",
			filename: "00000001000000000000000100",
			wantErr:  true,
		},
		{
			name:     "invalid characters",
			filename: "00000001000000000000000G",
			wantErr:  true,
		},
		{
			name:     "empty string",
			filename: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseWALFilename(tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseWALFilename() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if info.Timeline != tt.timeline {
					t.Errorf("Timeline = %d, want %d", info.Timeline, tt.timeline)
				}
				if info.Log != tt.log {
					t.Errorf("Log = %d, want %d", info.Log, tt.log)
				}
				if info.Segment != tt.segment {
					t.Errorf("Segment = %d, want %d", info.Segment, tt.segment)
				}
			}
		})
	}
}

func TestFormatWALFilename(t *testing.T) {
	tests := []struct {
		name     string
		timeline int
		log      int
		segment  int
		want     string
	}{
		{
			name:     "basic",
			timeline: 1,
			log:      0,
			segment:  1,
			want:     "000000010000000000000001",
		},
		{
			name:     "high values",
			timeline: 2,
			log:      10,
			segment:  255,
			want:     "000000020000000A000000FF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatWALFilename(tt.timeline, tt.log, tt.segment)
			if got != tt.want {
				t.Errorf("FormatWALFilename() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompareWALFiles(t *testing.T) {
	tests := []struct {
		name string
		a    *WALFileInfo
		b    *WALFileInfo
		want int
	}{
		{
			name: "a < b by timeline",
			a:    &WALFileInfo{Timeline: 1, Log: 0, Segment: 0},
			b:    &WALFileInfo{Timeline: 2, Log: 0, Segment: 0},
			want: -1,
		},
		{
			name: "a > b by timeline",
			a:    &WALFileInfo{Timeline: 2, Log: 0, Segment: 0},
			b:    &WALFileInfo{Timeline: 1, Log: 0, Segment: 0},
			want: 1,
		},
		{
			name: "a < b by log",
			a:    &WALFileInfo{Timeline: 1, Log: 0, Segment: 0},
			b:    &WALFileInfo{Timeline: 1, Log: 1, Segment: 0},
			want: -1,
		},
		{
			name: "a > b by log",
			a:    &WALFileInfo{Timeline: 1, Log: 1, Segment: 0},
			b:    &WALFileInfo{Timeline: 1, Log: 0, Segment: 0},
			want: 1,
		},
		{
			name: "a < b by segment",
			a:    &WALFileInfo{Timeline: 1, Log: 0, Segment: 1},
			b:    &WALFileInfo{Timeline: 1, Log: 0, Segment: 2},
			want: -1,
		},
		{
			name: "a > b by segment",
			a:    &WALFileInfo{Timeline: 1, Log: 0, Segment: 2},
			b:    &WALFileInfo{Timeline: 1, Log: 0, Segment: 1},
			want: 1,
		},
		{
			name: "a == b",
			a:    &WALFileInfo{Timeline: 1, Log: 0, Segment: 1},
			b:    &WALFileInfo{Timeline: 1, Log: 0, Segment: 1},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareWALFiles(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("CompareWALFiles() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseLSN(t *testing.T) {
	tests := []struct {
		name        string
		lsn         string
		wantErr     bool
		wantLog     int
		wantSegment int
	}{
		{
			name:        "valid LSN",
			lsn:         "0/1000000",
			wantErr:     false,
			wantLog:     0,
			wantSegment: 16777216,
		},
		{
			name:        "valid LSN with hex",
			lsn:         "A/FF",
			wantErr:     false,
			wantLog:     10,
			wantSegment: 255,
		},
		{
			name:    "invalid format - no slash",
			lsn:     "01000000",
			wantErr: true,
		},
		{
			name:    "invalid format - too many parts",
			lsn:     "0/1000000/2000000",
			wantErr: true,
		},
		{
			name:    "invalid hex",
			lsn:     "G/1000000",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, segment, err := ParseLSN(tt.lsn)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLSN() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if log != tt.wantLog {
					t.Errorf("Log = %d, want %d", log, tt.wantLog)
				}
				if segment != tt.wantSegment {
					t.Errorf("Segment = %d, want %d", segment, tt.wantSegment)
				}
			}
		})
	}
}

func TestFormatLSN(t *testing.T) {
	tests := []struct {
		name    string
		log     int
		segment int
		want    string
	}{
		{
			name:    "basic",
			log:     0,
			segment: 16777216,
			want:    "0/1000000",
		},
		{
			name:    "hex values",
			log:     10,
			segment: 255,
			want:    "A/FF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatLSN(tt.log, tt.segment)
			if got != tt.want {
				t.Errorf("FormatLSN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateWALRange(t *testing.T) {
	tests := []struct {
		name     string
		walFiles []*WALFileInfo
		startWAL string
		endWAL   string
		timeline int
		want     bool
		wantErr  bool
	}{
		{
			name: "complete range",
			walFiles: []*WALFileInfo{
				{Timeline: 1, Log: 0, Segment: 1},
				{Timeline: 1, Log: 0, Segment: 2},
				{Timeline: 1, Log: 0, Segment: 3},
			},
			startWAL: "0/1",
			endWAL:   "0/3",
			timeline: 1,
			want:     true,
			wantErr:  false,
		},
		{
			name: "missing middle file",
			walFiles: []*WALFileInfo{
				{Timeline: 1, Log: 0, Segment: 1},
				{Timeline: 1, Log: 0, Segment: 3},
			},
			startWAL: "0/1",
			endWAL:   "0/3",
			timeline: 1,
			want:     false,
			wantErr:  true,
		},
		{
			name: "wrong timeline",
			walFiles: []*WALFileInfo{
				{Timeline: 2, Log: 0, Segment: 1},
				{Timeline: 2, Log: 0, Segment: 2},
			},
			startWAL: "0/1",
			endWAL:   "0/2",
			timeline: 1,
			want:     false,
			wantErr:  true,
		},
		{
			name:     "empty WAL list",
			walFiles: []*WALFileInfo{},
			startWAL: "0/1",
			endWAL:   "0/2",
			timeline: 1,
			want:     false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateWALRange(tt.walFiles, tt.startWAL, tt.endWAL, tt.timeline)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWALRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ValidateWALRange() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindWALFilesInRange(t *testing.T) {
	walFiles := []*WALFileInfo{
		{Timeline: 1, Log: 0, Segment: 1},
		{Timeline: 1, Log: 0, Segment: 2},
		{Timeline: 1, Log: 0, Segment: 3},
		{Timeline: 1, Log: 0, Segment: 4},
		{Timeline: 2, Log: 0, Segment: 1},
	}

	tests := []struct {
		name     string
		startWAL string
		endWAL   string
		timeline int
		wantLen  int
		wantErr  bool
	}{
		{
			name:     "full range",
			startWAL: "0/1",
			endWAL:   "0/4",
			timeline: 1,
			wantLen:  4,
			wantErr:  false,
		},
		{
			name:     "partial range",
			startWAL: "0/2",
			endWAL:   "0/3",
			timeline: 1,
			wantLen:  2,
			wantErr:  false,
		},
		{
			name:     "wrong timeline",
			startWAL: "0/1",
			endWAL:   "0/4",
			timeline: 2,
			wantLen:  1,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindWALFilesInRange(walFiles, tt.startWAL, tt.endWAL, tt.timeline)
			if (err != nil) != tt.wantErr {
				t.Errorf("FindWALFilesInRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("FindWALFilesInRange() returned %d files, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestWALFileInfo_ModTime(t *testing.T) {
	now := time.Now()
	wal := &WALFileInfo{
		Filename: "000000010000000000000001",
		ModTime:  now,
	}

	if !wal.ModTime.Equal(now) {
		t.Errorf("ModTime not set correctly")
	}
}
