package tui

import (
	"strings"
	"testing"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHealthDashboardRoutesSessionExplorerAsynchronously(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Database: "app"}
	m.step = stepHealthDashboard

	updated, cmd := m.updateHealthDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	got := updated.(Model)
	if got.step != stepSessionExplorer || cmd == nil || !got.sessions.loading {
		t.Fatalf("step/loading/cmd = %v/%v/%v, want session explorer, loading, async refresh", got.step, got.sessions.loading, cmd != nil)
	}
}

func TestSessionExplorerRendersRedactedReadOnlySessions(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Database: "app"}
	m.step = stepSessionExplorer
	m.sessions.snapshot = &driver.SessionSnapshot{Sessions: []driver.SessionEntry{{PID: 42, Database: "app", User: "reader", State: "active", WaitEventType: "Lock", WaitEvent: "transactionid", QueryAgeMS: 1200, BlockingPIDs: []int32{7}}}}

	view := strings.ToLower(m.View())
	for _, marker := range []string{"session explorer", "read-only", "blockers", "refresh", "pid"} {
		if !strings.Contains(view, marker) {
			t.Fatalf("session explorer missing %q:\n%s", marker, view)
		}
	}
	if strings.Contains(view, "super-secret-query-token") {
		t.Fatalf("session explorer must not render query content:\n%s", view)
	}
}

func TestSessionExplorerEscReturnsHealthAndQQuits(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepSessionExplorer

	updated, _ := m.updateSessionExplorer(tea.KeyMsg{Type: tea.KeyEsc})
	if got := updated.(Model); got.step != stepHealthDashboard {
		t.Fatalf("Esc step = %v, want health dashboard", got.step)
	}
	_, cmd := m.updateSessionExplorer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("Q must return tea.Quit command")
	}
}
