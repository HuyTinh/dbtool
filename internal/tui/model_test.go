package tui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dbtool/internal/config"

	tea "github.com/charmbracelet/bubbletea"
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
		t.Fatalf("truncate short = %q, want short", short)
	}

	long := truncateMiddle("C:/very/long/path/to/file.dump", 16)
	if len([]rune(long)) > 16 || !strings.Contains(long, "...") || !strings.HasPrefix(long, "C:/") || !strings.HasSuffix(long, "dump") {
		t.Fatalf("truncate long = %q, want <=16 runes with prefix/suffix", long)
	}

	unicode := truncateMiddle("docker • docker-cli • service pap-cmms-db-des", 24)
	if strings.Contains(unicode, "�") {
		t.Fatalf("truncate unicode = %q, want valid UTF-8 truncation", unicode)
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

	out := renderProfileRows(80, profiles, 1, "source")
	if !strings.Contains(out, "▶") {
		t.Fatalf("rendered rows missing selected marker: %q", out)
	}
	if !strings.Contains(out, "SOURCE") {
		t.Fatalf("rendered rows missing source marker: %q", out)
	}
	if !strings.Contains(out, "POSTGRES") {
		t.Fatalf("rendered rows missing driver badge: %q", out)
	}
}

func TestRenderProfileRowsShowsDockerRuntimeBadges(t *testing.T) {
	profiles := []config.Profile{{
		Name:     "docker-source",
		Driver:   "postgres",
		Host:     "localhost",
		Port:     5447,
		Database: "pap-cmms-db",
		Runtime: &config.RuntimeProfile{
			Type:        "docker",
			Source:      "docker-cli",
			ServiceName: "pap-cmms-db-source",
			Container:   "pap-cmms-db-source",
		},
	}}

	out := renderProfileRows(90, profiles, 0, "")
	for _, want := range []string{"DOCKER", "LIVE", "pap-cmms-db-source"} {
		if !strings.Contains(out, want) {
			t.Fatalf("docker profile rows missing %q in %q", want, out)
		}
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

func TestAppShellHelpers(t *testing.T) {
	header := renderAppHeader("Restore Database", "Profile: local-dev", 80)
	if !strings.Contains(header, "dbtool") || !strings.Contains(header, "Restore Database") || !strings.Contains(header, "Profile: local-dev") {
		t.Fatalf("header = %q, want app name, title and subtitle", header)
	}

	bar := renderCommandBar(80, []keyHint{
		{Key: "R", Label: "restore"},
		{Key: "Q", Label: "quit", Danger: true},
	})
	if !strings.Contains(bar, "R") || !strings.Contains(bar, "restore") || !strings.Contains(bar, "Q") || !strings.Contains(bar, "quit") {
		t.Fatalf("command bar = %q, want key labels", bar)
	}

	frame := renderScreenFrame(80, "Title", "Subtitle", "Body", []keyHint{{Key: "Enter", Label: "go"}})
	if !strings.Contains(frame, "Title") || !strings.Contains(frame, "Subtitle") || !strings.Contains(frame, "Body") || !strings.Contains(frame, "Enter") {
		t.Fatalf("screen frame = %q, want shell content", frame)
	}
}

func TestCardAndBadgeHelpers(t *testing.T) {
	card := renderCard(80, "Target", "Database app")
	if !strings.Contains(card, "Target") || !strings.Contains(card, "Database app") {
		t.Fatalf("card = %q, want title and body", card)
	}

	grid := renderKeyValueGrid([]kvRow{{Label: "Profile", Value: "prod"}}, 10)
	if !strings.Contains(grid, "Profile") || !strings.Contains(grid, "prod") {
		t.Fatalf("grid = %q, want label/value", grid)
	}

	badge := renderStatusBadge("CLEAN", badgeDanger)
	if !strings.Contains(badge, "CLEAN") {
		t.Fatalf("badge = %q, want label", badge)
	}
}

func TestNewModelPopulatesProfileNamesFromConfigMap(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"local-dev": {Driver: "postgres", Host: "localhost", Port: 5432, User: "postgres", Database: "app"},
	}}

	m := NewModel(cfg, RestoreSettings{})
	if len(m.profiles) != 1 {
		t.Fatalf("profiles len = %d, want 1", len(m.profiles))
	}
	if got := m.profiles[0].Name; got != "local-dev" {
		t.Fatalf("profile name = %q, want local-dev", got)
	}
}

func TestSaveProfileFormPreservesRuntimeMetadataWhenEditing(t *testing.T) {
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "AppData", "Roaming"))

	runtime := &config.RuntimeProfile{
		Type:        "docker",
		Source:      "docker-compose",
		SourceFile:  "docker-compose.yml",
		ServiceName: "postgres",
		Container:   "db-1",
		Mounts: []config.MountMapping{{
			Type:   "bind",
			Source: "/repo/pgdata",
			Target: "/var/lib/postgresql/data",
		}},
		Paths: config.RuntimePaths{
			DataDirectory: "/var/lib/postgresql/data",
			HBAFile:       "/var/lib/postgresql/data/pg_hba.conf",
			HostHBAFile:   "/repo/pgdata/pg_hba.conf",
		},
	}
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"docker-dev": {
			Name:     "docker-dev",
			Driver:   "postgres",
			Host:     "localhost",
			Port:     5432,
			User:     "postgres",
			Password: "secret",
			Database: "app",
			Runtime:  runtime,
		},
	}}
	m := NewModel(cfg, RestoreSettings{})
	selected := cfg.Profiles["docker-dev"]
	m.inputs = m.initFormFields(&selected)
	m.inputs[2].SetValue("127.0.0.1")
	m.isEditing = true
	m.origName = "docker-dev"

	updated, _ := m.saveProfileForm()
	gotModel := updated.(Model)
	got := gotModel.cfg.Profiles["docker-dev"]

	if got.Host != "127.0.0.1" {
		t.Fatalf("host = %q, want 127.0.0.1", got.Host)
	}
	if !reflect.DeepEqual(got.Runtime, runtime) {
		t.Fatalf("runtime metadata was not preserved: got %#v, want %#v", got.Runtime, runtime)
	}
}

func TestViewEditProfileFormShowsRuntimeStatusAndShortcut(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 90
	m.startProfileForm(nil, false)

	out := m.viewEditProfileForm()
	for _, want := range []string{"MANUAL", "F2/Ctrl+R", "F3", "F4", "detect runtime"} {
		if !strings.Contains(out, want) {
			t.Fatalf("edit profile form missing %q in %q", want, out)
		}
	}
}

func TestViewEditProfileFormShowsDetectingProgress(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 90
	m.startProfileForm(nil, false)
	m.runtimeDetecting = true
	m.runtimeDetectPct = 65
	m.runtimeDetectText = "Scanning local compose files..."

	out := m.viewEditProfileForm()
	for _, want := range []string{"DETECTING", "Scanning local compose files", "█", "░"} {
		if !strings.Contains(out, want) {
			t.Fatalf("detecting form missing %q in %q", want, out)
		}
	}
}

func TestViewEditProfileFormShowsDockerRuntimeDetails(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 100
	m.startProfileForm(nil, false)
	m.formRuntime = &config.RuntimeProfile{
		Type:        "docker",
		Source:      "docker-cli",
		SourceFile:  filepath.Join("repo", "docker-compose.db.yml"),
		ServiceName: "pap-cmms-db-source",
		Container:   "pap-cmms-db-source",
		Mounts:      []config.MountMapping{{Source: "/repo/pgdata", Target: "/var/lib/postgresql/data", Type: "bind"}},
	}
	m.formInfo = formatRuntimeSummary(m.formRuntime)
	m.formRuntimeDetails = true

	out := m.viewEditProfileForm()
	for _, want := range []string{"DOCKER DETECTED", "LIVE CONTAINER", "Docker Runtime Details", "Service:", "Container:", "Compose File:", "Mounts:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("docker runtime form missing %q in %q", want, out)
		}
	}
}

func TestUpdateEditProfileFormF3TogglesAndF4ClearsRuntime(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.startProfileForm(nil, false)
	m.formRuntime = &config.RuntimeProfile{Type: "docker", Source: "docker-cli", Container: "pg-live"}
	m.formInfo = formatRuntimeSummary(m.formRuntime)
	m.formRuntimeDetails = true

	updated, _ := m.updateEditProfileForm(tea.KeyMsg{Type: tea.KeyF3})
	got := updated.(Model)
	if got.formRuntimeDetails {
		t.Fatalf("expected F3 to hide runtime details")
	}

	updated, _ = got.updateEditProfileForm(tea.KeyMsg{Type: tea.KeyF4})
	got = updated.(Model)
	if got.formRuntime != nil {
		t.Fatalf("expected F4 to clear runtime metadata, got %#v", got.formRuntime)
	}
	if got.formInfo != defaultManualRuntimeMessage() {
		t.Fatalf("form info = %q, want manual runtime message", got.formInfo)
	}
}

func TestUpdateEditProfileFormF2DetectsDockerRuntime(t *testing.T) {
	origDocker := dockerRuntimeProfileDetector
	origCompose := composeRuntimeProfileDetector
	defer func() {
		dockerRuntimeProfileDetector = origDocker
		composeRuntimeProfileDetector = origCompose
	}()

	dockerRuntimeProfileDetector = func(input config.Profile, dir string) (config.Profile, string, error) {
		return input, "No running Docker database containers matched the current form values; checking compose files.", nil
	}
	composeRuntimeProfileDetector = func(input config.Profile, dir string) (config.Profile, string, error) {
		input.Runtime = &config.RuntimeProfile{Type: "docker", Source: "docker-compose", ServiceName: "postgres"}
		input.Database = "app"
		return input, "Runtime: docker • docker-compose • service postgres", nil
	}

	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	tempDir := t.TempDir()
	m.currentDir = tempDir
	m.startProfileForm(nil, false)

	updated, _ := m.updateEditProfileForm(tea.KeyMsg{Type: tea.KeyF2})
	intermediate := updated.(Model)
	if !intermediate.runtimeDetecting {
		t.Fatalf("expected runtime detection to start after F2")
	}

	result, msg, err := detectRuntimeProfile(config.Profile{Driver: "postgres", Host: "localhost", Port: 5432, User: "postgres"}, tempDir, nil)
	if err != nil {
		t.Fatalf("detectRuntimeProfile error: %v", err)
	}

	finished, _ := intermediate.finishRuntimeDetection(runtimeDetectFinishedMsg{Profile: result, Message: msg})
	got := finished.(Model)

	if got.formRuntime == nil {
		t.Fatalf("expected docker runtime metadata after F2 detection")
	}
	if got.formRuntime.Type != "docker" {
		t.Fatalf("runtime type = %q, want docker", got.formRuntime.Type)
	}
	if got.inputs[6].Value() != "app" {
		t.Fatalf("database field = %q, want app", got.inputs[6].Value())
	}
	if !strings.Contains(got.formInfo, "Runtime: docker") {
		t.Fatalf("form info = %q, want docker summary", got.formInfo)
	}
}

func TestSelectModeDashboard(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	out := m.viewSelectMode()
	for _, want := range []string{"Restore", "Dump", "Migrate", "TARGET DB", "BACKUP", "SRC → DST"} {
		if !strings.Contains(out, want) {
			t.Fatalf("select mode missing %q in %q", want, out)
		}
	}
}

func TestConfirmViewsUseReviewSections(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 90
	m.result.Profile = config.Profile{Name: "prod", Driver: "postgres", Host: "localhost", Port: 5432, Database: "app", User: "postgres"}
	m.result.File = "backup.dump"
	m.result.Settings = RestoreSettings{Format: "custom", Jobs: 4, Clean: true, IncludeTable: []string{"users"}}
	out := m.viewConfirm()
	for _, want := range []string{"Target Profile", "Dump File", "Restore Options", "Safety Review"} {
		if !strings.Contains(out, want) {
			t.Fatalf("restore confirm missing %q in %q", want, out)
		}
	}

	m.result.DumpFile = "backup.dump"
	m.result.DumpSettings = DumpSettings{Format: "custom", IncludeSchema: []string{"public"}}
	out = m.viewDumpConfirm()
	for _, want := range []string{"Source Profile", "Output File", "Dump Options"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dump confirm missing %q in %q", want, out)
		}
	}

	m.result.DestProfile = config.Profile{Name: "target", Driver: "postgres", Host: "localhost", Port: 5433, Database: "targetdb", User: "postgres"}
	m.result.MigrateSettings = MigrateSettings{Format: "custom", Jobs: 4, Clean: true}
	out = m.viewMigrateConfirm()
	for _, want := range []string{"Source Profile", "Target Profile", "SRC → DST", "Migrate Options", "Safety Review"} {
		if !strings.Contains(out, want) {
			t.Fatalf("migrate confirm missing %q in %q", want, out)
		}
	}
}
