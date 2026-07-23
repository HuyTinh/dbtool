package tui

import (
	"strings"
	"testing"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSessionExplorerPOpensPlanOnlyDetailForFirstSession(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Database: "app"}
	m.step = stepSessionExplorer
	m.sessions.snapshot = &driver.SessionSnapshot{Sessions: []driver.SessionEntry{{PID: 42, State: "active", WaitEventType: "Lock", WaitEvent: "transactionid", BlockingPIDs: []int32{7}}}}

	updated, cmd := m.updateSessionExplorer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	got := updated.(Model)
	if got.step != stepSessionActionPlan || cmd != nil {
		t.Fatalf("step/cmd = %v/%v, want plan-only detail without async work", got.step, cmd != nil)
	}

	view := strings.ToLower(got.View())
	for _, marker := range []string{"plan only", "pid", "transactionid", "blockers", "sql preview", "never executes"} {
		if !strings.Contains(view, marker) {
			t.Fatalf("plan detail missing %q:\n%s", marker, view)
		}
	}
	for _, forbidden := range []string{"execute action", "confirm action", "cancel session", "terminate session"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("plan detail exposes execution UI %q:\n%s", forbidden, view)
		}
	}
}

func TestSessionExplorerPBlocksDiscoveredSelfPID(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepSessionExplorer
	m.sessions.snapshot = &driver.SessionSnapshot{SelfPID: 42, Sessions: []driver.SessionEntry{{PID: 42}}}

	updated, cmd := m.updateSessionExplorer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	got := updated.(Model)
	if got.step != stepSessionExplorer || cmd != nil {
		t.Fatalf("step/cmd = %v/%v, want self-protected explorer without work", got.step, cmd != nil)
	}
	if !strings.Contains(strings.ToLower(got.View()), "self") {
		t.Fatalf("self-protection must be visible:\n%s", got.View())
	}
}

func TestSessionExplorerPWithoutSessionsStaysOnExplorer(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepSessionExplorer
	m.sessions.snapshot = &driver.SessionSnapshot{}

	updated, cmd := m.updateSessionExplorer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if got := updated.(Model); got.step != stepSessionExplorer || cmd != nil {
		t.Fatalf("step/cmd = %v/%v, want explorer without work", got.step, cmd != nil)
	}
}
