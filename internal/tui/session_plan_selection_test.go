package tui

import (
	"strings"
	"testing"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSessionExplorerPlanRejectsFocusedCollectorSelfPID(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepSessionExplorer
	m.sessions.snapshot = &driver.SessionSnapshot{SelfPID: 1, Sessions: []driver.SessionEntry{{PID: 1}, {PID: 2, State: "active"}}}

	updated, cmd := m.updateSessionExplorer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	got := updated.(Model)
	if cmd != nil || got.step != stepSessionExplorer || got.sessions.plan != nil {
		t.Fatalf("focused self plan = step %v plan %#v cmd %v, want refusal without work", got.step, got.sessions.plan, cmd != nil)
	}
	if !strings.Contains(strings.ToLower(got.sessions.planErr), "self") {
		t.Fatalf("self refusal = %q, want visible self protection", got.sessions.planErr)
	}
}
