package tui

import (
	"strings"
	"testing"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestMigrateDestinationSelectionStartsAsyncPreflight(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{
		"source": {Name: "source", Driver: "postgres", Database: "source_db"},
		"target": {Name: "target", Driver: "postgres", Database: "target_db"},
	}}, RestoreSettings{})
	m.result.Profile = m.profiles[0]
	m.migrateDestIdx = 1

	updated, cmd := m.updateMigrateDestSelector(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.step != stepMigratePreflight {
		t.Fatalf("step = %v, want migrate preflight", got.step)
	}
	if !got.migratePreflight.loading || cmd == nil {
		t.Fatalf("loading/cmd = %v/%v, want true/async command", got.migratePreflight.loading, cmd != nil)
	}
}

func TestMigratePreflightReportsSchemaDiffAndRoutesToConfirmation(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Database: "source_db"}
	m.result.DestProfile = config.Profile{Name: "target", Driver: "postgres", Database: "target_db"}
	m.step = stepMigratePreflight
	m.height = 200
	m.migratePreflight = migratePreflightState{
		source: &driver.SchemaSnapshot{Tables: []driver.SchemaTable{{Schema: "public", Name: "users"}}},
		target: &driver.SchemaSnapshot{Tables: []driver.SchemaTable{{Schema: "public", Name: "legacy"}}},
		diff:   driver.SchemaDiff{MissingTables: []string{"public.users"}, ExtraTables: []string{"public.legacy"}},
	}

	view := strings.ToLower(m.View())
	for _, marker := range []string{"migration preflight", "read-only", "missing from target", "extra on target", "public.users", "public.legacy", "proceed"} {
		if !strings.Contains(view, marker) {
			t.Fatalf("preflight missing %q:\n%s", marker, view)
		}
	}

	updated, _ := m.updateMigratePreflight(tea.KeyMsg{Type: tea.KeyEnter})
	if got := updated.(Model); got.step != stepMigrateConfirm {
		t.Fatalf("step = %v, want migrate confirmation", got.step)
	}
}

func TestMigratePreflightClearlyReportsCrossDriverWithoutNetwork(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres"}
	m.result.DestProfile = config.Profile{Name: "target", Driver: "mysql"}
	m.step = stepMigratePreflight

	cmd := m.startMigratePreflight()
	msg := cmd()
	updated, _ := m.Update(msg)
	got := updated.(Model)
	if got.migratePreflight.err == nil || !strings.Contains(got.migratePreflight.err.Error(), "cross-driver") {
		t.Fatalf("preflight error = %v, want cross-driver error", got.migratePreflight.err)
	}
	if !strings.Contains(strings.ToLower(got.View()), "cross-driver") {
		t.Fatalf("preflight does not render cross-driver error:\n%s", got.View())
	}
}

func TestMigratePreflightFrameFitsTerminalHeightWithLongSchemaDiff(t *testing.T) {
	m := migratePreflightScrollTestModel(12)
	if got := lipgloss.Height(m.View()); got > m.height {
		t.Fatalf("rendered migration preflight height = %d, exceeds terminal height %d", got, m.height)
	}
}

func TestMigratePreflightScrollKeysReachSchemaDiffAndClamp(t *testing.T) {
	m := migratePreflightScrollTestModel(12)
	page := m.migratePreflightVisibleHeight()
	if page < 1 || m.migratePreflightMaxOffset() == 0 {
		t.Fatalf("expected a positive viewport and overflow, got page=%d max=%d", page, m.migratePreflightMaxOffset())
	}
	if !strings.Contains(m.View(), "Source Profile") || !strings.Contains(m.View(), "↑/k") {
		t.Fatalf("top viewport should render the source profile and scroll controls:\n%s", m.View())
	}

	m = updateMigratePreflightKey(t, m, tea.KeyDown)
	if m.migratePreflight.offset != 1 {
		t.Fatalf("down offset = %d, want 1", m.migratePreflight.offset)
	}
	m = updateMigratePreflightKey(t, m, tea.KeyPgDown)
	if m.migratePreflight.offset != min(1+page, m.migratePreflightMaxOffset()) {
		t.Fatalf("page down offset = %d, want %d", m.migratePreflight.offset, min(1+page, m.migratePreflightMaxOffset()))
	}
	m = updateMigratePreflightKey(t, m, tea.KeyEnd)
	if m.migratePreflight.offset != m.migratePreflightMaxOffset() {
		t.Fatalf("end offset = %d, want max %d", m.migratePreflight.offset, m.migratePreflightMaxOffset())
	}
	if view := m.View(); !strings.Contains(view, "public.deprecated") || !strings.Contains(view, "Migration Preflight") || !strings.Contains(view, "Q") {
		t.Fatalf("bottom viewport must expose final diff content with fixed frame:\n%s", view)
	}
	m = updateMigratePreflightKey(t, m, tea.KeyHome)
	if m.migratePreflight.offset != 0 {
		t.Fatalf("home offset = %d, want 0", m.migratePreflight.offset)
	}

	m = updateMigratePreflightKey(t, m, tea.KeyEnd)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 200})
	m = updated.(Model)
	if m.migratePreflight.offset != 0 {
		t.Fatalf("resize must clamp offset to 0 when all content fits, got %d", m.migratePreflight.offset)
	}
}

func TestMigratePreflightScrollHintsAndPositionOnlyAppearOnOverflow(t *testing.T) {
	m := migratePreflightScrollTestModel(12)
	if view := m.View(); !strings.Contains(view, "position") || !strings.Contains(view, "PgUp/PgDn") {
		t.Fatalf("overflow footer missing position and scroll hints:\n%s", view)
	}

	m.height = 200
	if view := m.View(); strings.Contains(view, "position") || strings.Contains(view, "PgUp/PgDn") {
		t.Fatalf("non-overflow footer must omit position and scroll hints:\n%s", view)
	}
}

func updateMigratePreflightKey(t *testing.T, m Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated.(Model)
}

func migratePreflightScrollTestModel(height int) Model {
	m := NewModel(nil, RestoreSettings{})
	m.step = stepMigratePreflight
	m.width = 100
	m.height = height
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Host: "source.example", Port: 5432, Database: "source_db", User: "source_user"}
	m.result.DestProfile = config.Profile{Name: "target", Driver: "postgres", Host: "target.example", Port: 5432, Database: "target_db", User: "target_user"}
	m.migratePreflight.source = &driver.SchemaSnapshot{Tables: make([]driver.SchemaTable, 12)}
	m.migratePreflight.target = &driver.SchemaSnapshot{Tables: make([]driver.SchemaTable, 8)}
	m.migratePreflight.diff = driver.SchemaDiff{
		MissingTables: []string{"public.accounts", "public.invoices", "public.payments", "public.audit_events", "public.notifications", "public.search_documents"},
		ExtraTables:   []string{"public.legacy_accounts", "public.legacy_invoices", "public.archive", "public.staging", "public.temporary", "public.deprecated"},
	}
	return m
}
