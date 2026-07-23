package tui

import (
	"strings"
	"testing"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestHealthModeRoutesProfileSelectionToDashboard(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"source": {Name: "source", Driver: "postgres", Host: "localhost", Port: 5432, Database: "app"},
	}}
	m := NewModel(cfg, RestoreSettings{})

	updated, _ := m.updateSelectMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	selected := updated.(Model)
	if selected.result.Mode != ModeHealth || selected.step != stepSelectProfile {
		t.Fatalf("mode/step = %v/%v, want health/profile selector", selected.result.Mode, selected.step)
	}

	updated, cmd := selected.updateProfileSelector(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.step != stepHealthDashboard || cmd == nil {
		t.Fatalf("step/cmd = %v/%v, want health dashboard and async refresh", got.step, cmd != nil)
	}
}

func TestHealthDashboardRendersReadOnlySnapshot(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Database: "app"}
	m.step = stepHealthDashboard
	m.height = 200
	m.health.snapshot = &driver.HealthSnapshot{
		ServerVersion:      "16.4",
		Latency:            12 * time.Millisecond,
		DatabaseSize:       1024 * 1024,
		CurrentConnections: 4,
		MaxConnections:     100,
		BlockedSessions:    1,
	}

	view := strings.ToLower(m.View())
	for _, marker := range []string{"health dashboard", "read-only", "blocked sessions", "refresh"} {
		if !strings.Contains(view, marker) {
			t.Fatalf("dashboard missing %q:\n%s", marker, view)
		}
	}
}

func TestHealthProfileSelectorUsesSourceCopy(t *testing.T) {
	if got := profileSelectorPrompt(ModeHealth); got != "Choose a PostgreSQL profile for health" {
		t.Fatalf("profileSelectorPrompt = %q", got)
	}
	if got := profileSelectorSubtitle(ModeHealth); got != "Review a read-only database health snapshot" {
		t.Fatalf("profileSelectorSubtitle = %q", got)
	}
}

func TestHealthDashboardRoutesSizeExplorerAsynchronously(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Database: "app"}
	m.step = stepHealthDashboard

	updated, cmd := m.updateHealthDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	got := updated.(Model)
	if got.step != stepSizeExplorer || cmd == nil {
		t.Fatalf("step/cmd = %v/%v, want size explorer and async refresh", got.step, cmd != nil)
	}
}

func TestHealthDashboardRefreshResetsDocumentOffset(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepHealthDashboard
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Database: "app"}
	m.health.offset = 7

	updated, cmd := m.updateHealthDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	got := updated.(Model)
	if cmd == nil || got.health.offset != 0 {
		t.Fatalf("refresh cmd/offset = %v/%d, want async refresh and reset offset", cmd != nil, got.health.offset)
	}
}

func TestHealthDashboardScrollsUnavailableMetricsWithinFixedFrame(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepHealthDashboard
	m.width = 100
	m.height = 12
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Database: "app"}
	m.health.snapshot = &driver.HealthSnapshot{
		Capabilities: []driver.HealthCapability{
			{Name: "capability-01", Message: "unavailable"}, {Name: "capability-02", Message: "unavailable"},
			{Name: "capability-03", Message: "unavailable"}, {Name: "capability-04", Message: "unavailable"},
			{Name: "capability-05", Message: "unavailable"}, {Name: "capability-06", Message: "unavailable"},
			{Name: "capability-07", Message: "unavailable"}, {Name: "capability-08", Message: "unavailable"},
			{Name: "capability-09", Message: "unavailable"}, {Name: "capability-10", Message: "unavailable"},
			{Name: "capability-11", Message: "unavailable"},
		},
	}

	if got := lipgloss.Height(m.View()); got > m.height {
		t.Fatalf("top dashboard height = %d, exceeds terminal height %d", got, m.height)
	}
	if view := m.View(); !strings.Contains(view, "Health Profile") || !strings.Contains(view, "PgUp/PgDn") {
		t.Fatalf("top dashboard must show its first content and overflow controls:\n%s", view)
	}
	if m.healthDashboardMaxOffset() == 0 {
		t.Fatal("expected unavailable metrics to overflow a short health dashboard")
	}

	m = updateHealthDashboardKey(t, m, tea.KeyDown)
	if m.health.offset != 1 {
		t.Fatalf("down offset = %d, want 1", m.health.offset)
	}
	m = updateHealthDashboardRune(t, m, 'j')
	if m.health.offset != 2 {
		t.Fatalf("j offset = %d, want 2", m.health.offset)
	}
	m = updateHealthDashboardKey(t, m, tea.KeyPgDown)
	m = updateHealthDashboardKey(t, m, tea.KeyPgUp)
	m = updateHealthDashboardKey(t, m, tea.KeyEnd)
	if m.health.offset != m.healthDashboardMaxOffset() {
		t.Fatalf("end offset = %d, want max %d", m.health.offset, m.healthDashboardMaxOffset())
	}
	if view := m.View(); !strings.Contains(view, "capability-11") || !strings.Contains(view, "Health Dashboard") || !strings.Contains(view, "refresh") {
		t.Fatalf("bottom dashboard must expose final warning content and fixed frame:\n%s", view)
	}

	m = updateHealthDashboardKey(t, m, tea.KeyHome)
	if m.health.offset != 0 || !strings.Contains(m.View(), "Health Profile") {
		t.Fatalf("home must return to the dashboard start, offset=%d:\n%s", m.health.offset, m.View())
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 200})
	m = updated.(Model)
	if m.health.offset != 0 || strings.Contains(m.View(), "PgUp/PgDn") {
		t.Fatalf("fitting dashboard must clamp offset and omit overflow controls, offset=%d:\n%s", m.health.offset, m.View())
	}
}

func updateHealthDashboardKey(t *testing.T, m Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated.(Model)
}

func updateHealthDashboardRune(t *testing.T, m Model, value rune) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}})
	return updated.(Model)
}

func TestSizeExplorerRendersStorageAndEstimateDisclosure(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Database: "app"}
	m.step = stepSizeExplorer
	m.height = 80
	m.size.snapshot = &driver.SizeSnapshot{
		DatabaseSize: 4 * 1024 * 1024,
		Relations:    []driver.SizeRelation{{Schema: "app", Name: "orders", EstimatedRows: 42, TableBytes: 1024, IndexBytes: 2048, TotalBytes: 3072}},
		Indexes:      []driver.SizeIndex{{Schema: "app", Name: "orders_pkey", TableName: "orders", SizeBytes: 2048}},
	}

	view := strings.ToLower(m.View())
	for _, marker := range []string{"size explorer", "estimates", "largest relations", "largest indexes", "read-only", "refresh"} {
		if !strings.Contains(view, marker) {
			t.Fatalf("size explorer missing %q:\n%s", marker, view)
		}
	}
}
