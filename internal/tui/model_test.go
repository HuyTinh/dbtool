package tui

import (
	"strings"
	"testing"

	"dbtool/internal/config"
)

func TestParseFilterValues(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "spaces only", in: "   ", want: nil},
		{name: "comma separated", in: "users,orders,products", want: []string{"users", "orders", "products"}},
		{name: "trim spaces and skip empty", in: " users, , orders ,, products ", want: []string{"users", "orders", "products"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFilterValues(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: 42, want: "42 B"},
		{bytes: 1024, want: "1.0 KB"},
		{bytes: 1024 * 1024, want: "1.0 MB"},
	}

	for _, tt := range tests {
		if got := formatBytes(tt.bytes); got != tt.want {
			t.Fatalf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestVisibleEntriesFiltersByName(t *testing.T) {
	m := Model{
		entries: []fileEntry{
			{Name: "alpha.dump"},
			{Name: "beta.sql"},
			{Name: "notes.txt"},
		},
	}

	if got := m.visibleEntries(); len(got) != 3 {
		t.Fatalf("not searching len = %d, want 3", len(got))
	}

	m.searching = true
	m.searchInput.SetValue("DUMP")
	got := m.visibleEntries()
	if len(got) != 1 || got[0].Name != "alpha.dump" {
		t.Fatalf("filtered got %#v, want alpha.dump only", got)
	}

	m.searchInput.SetValue("missing")
	if got := m.visibleEntries(); len(got) != 0 {
		t.Fatalf("missing filter len = %d, want 0", len(got))
	}
}

func TestLayoutHelpers(t *testing.T) {
	if got := safePanelWidth(2); got != 20 {
		t.Fatalf("safePanelWidth(2) = %d, want 20", got)
	}
	if got := safePanelWidth(80); got != 76 {
		t.Fatalf("safePanelWidth(80) = %d, want 76", got)
	}

	short := truncateMiddle("short", 10)
	if short != "short" {
		t.Fatalf("truncate short = %q", short)
	}

	long := truncateMiddle("C:/very/long/path/to/file.dump", 16)
	if len(long) > 16 || !strings.Contains(long, "...") || !strings.HasPrefix(long, "C:/") || !strings.HasSuffix(long, "dump") {
		t.Fatalf("truncate long = %q, want <=16 with prefix/suffix", long)
	}
}

func TestProgressHelpers(t *testing.T) {
	if got := clampPercent(-10); got != 0 {
		t.Fatalf("clampPercent(-10) = %v, want 0", got)
	}
	if got := clampPercent(150); got != 100 {
		t.Fatalf("clampPercent(150) = %v, want 100", got)
	}
	if got := progressWidth(10); got != 20 {
		t.Fatalf("progressWidth(10) = %d, want 20", got)
	}
	bar := renderProgressBar(10, 50)
	if !strings.Contains(bar, "█") || !strings.Contains(bar, "░") {
		t.Fatalf("progress bar = %q, want filled and empty segments", bar)
	}
}

func TestRenderProfileRowsMarksSelectedAndSource(t *testing.T) {
	profiles := []config.Profile{
		{Name: "source", Driver: "postgres", Host: "localhost", Port: 5432, Database: "src"},
		{Name: "target", Driver: "postgres", Host: "localhost", Port: 5433, Database: "dst"},
	}

	out := renderProfileRows(profiles, 1, "source")
	if !strings.Contains(out, "▶") {
		t.Fatalf("rendered rows missing selected marker: %q", out)
	}
	if !strings.Contains(out, "(source)") {
		t.Fatalf("rendered rows missing source marker: %q", out)
	}
	if !strings.Contains(out, "POSTGRES") {
		t.Fatalf("rendered rows missing driver badge: %q", out)
	}
}

func TestConfirmViewsShowSafetyWarnings(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 80
	m.result.Profile = config.Profile{Name: "prod", Driver: "postgres", Host: "localhost", Port: 5432, Database: "app"}
	m.result.File = "backup.dump"
	m.result.Settings.Clean = true
	if out := m.viewConfirm(); !strings.Contains(out, "may drop database objects") {
		t.Fatalf("restore confirm missing clean warning: %q", out)
	}

	m.result.DestProfile = config.Profile{Name: "target", Driver: "postgres", Host: "localhost", Port: 5433, Database: "targetdb"}
	m.result.MigrateSettings.Clean = true
	if out := m.viewMigrateConfirm(); !strings.Contains(out, "target may be overwritten") || !strings.Contains(out, "may drop target database objects") {
		t.Fatalf("migrate confirm missing safety warning: %q", out)
	}

	m.profiles = []config.Profile{{Name: "prod", Driver: "postgres", Host: "localhost", Port: 5432, Database: "app"}}
	m.profileIdx = 0
	if out := m.viewConfirmDelete(); !strings.Contains(out, "does not delete the database") {
		t.Fatalf("delete confirm missing local-only warning: %q", out)
	}
}
