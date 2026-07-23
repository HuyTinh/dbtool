package tui

import (
	"fmt"
	"strings"
	"testing"

	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSessionExplorerFocusedRowStaysVisibleAcrossNavigationAndBounds(t *testing.T) {
	m := sessionExplorerViewportTestModel(12)
	assertSelectorFrame(t, m.viewSessionExplorer(), m.height)
	assertContains(t, m.viewSessionExplorer(), "> 100")
	assertNotContains(t, m.viewSessionExplorer(), "111")

	m = updateSessionExplorerKey(t, m, tea.KeyDown)
	if m.sessions.focus != 1 {
		t.Fatalf("down focus = %d, want 1", m.sessions.focus)
	}
	assertContains(t, m.viewSessionExplorer(), "> 101")

	m = updateSessionExplorerKey(t, m, tea.KeyPgDown)
	if m.sessions.focus <= 1 {
		t.Fatalf("pgdown focus = %d, want progress", m.sessions.focus)
	}
	assertContains(t, m.viewSessionExplorer(), fmt.Sprintf("> %d", 100+m.sessions.focus))

	m = updateSessionExplorerKey(t, m, tea.KeyEnd)
	if m.sessions.focus != len(m.sessions.snapshot.Sessions)-1 {
		t.Fatalf("end focus = %d, want %d", m.sessions.focus, len(m.sessions.snapshot.Sessions)-1)
	}
	view := m.viewSessionExplorer()
	assertContains(t, view, "> 111")
	assertSelectorFrame(t, view, m.height)

	m = updateSessionExplorerKey(t, m, tea.KeyHome)
	if m.sessions.focus != 0 {
		t.Fatalf("home focus = %d, want 0", m.sessions.focus)
	}
	m = updateSessionExplorerKey(t, m, tea.KeyEnd)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 200})
	m = updated.(Model)
	if m.sessions.focus != len(m.sessions.snapshot.Sessions)-1 || m.sessions.offset != 0 {
		t.Fatalf("resize focus/offset = %d/%d, want final focus and zero offset when all rows fit", m.sessions.focus, m.sessions.offset)
	}
	assertContains(t, m.viewSessionExplorer(), "> 111")
}

func TestSessionExplorerShowsOverflowHintsOnlyWhenNeeded(t *testing.T) {
	m := sessionExplorerViewportTestModel(12)
	view := m.viewSessionExplorer()
	if !strings.Contains(view, "↑/k") || !strings.Contains(view, "PgUp/PgDn") {
		t.Fatalf("overflow footer missing navigation hints:\n%s", view)
	}

	m.height = 200
	view = m.viewSessionExplorer()
	if strings.Contains(view, "↑/k") || strings.Contains(view, "PgUp/PgDn") {
		t.Fatalf("non-overflow footer must not show navigation hints:\n%s", view)
	}
}

func TestSessionExplorerPUsesFocusedActionableSessionAndRejectsFocusedSelfOrBackground(t *testing.T) {
	m := sessionExplorerViewportTestModel(24)
	m.sessions.snapshot = &driver.SessionSnapshot{SelfPID: 101, Sessions: []driver.SessionEntry{
		{PID: 100, State: ""},
		{PID: 101, State: "active"},
		{PID: 102, State: "idle"},
	}}
	m.sessions.focus = 2

	updated, cmd := m.updateSessionExplorer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	got := updated.(Model)
	if cmd != nil || got.step != stepSessionActionPlan || got.sessions.plan == nil || got.sessions.plan.PID != 102 {
		t.Fatalf("focused actionable P = step %v plan %#v cmd %v, want PID 102 plan only", got.step, got.sessions.plan, cmd != nil)
	}

	m.sessions.focus = 1
	updated, cmd = m.updateSessionExplorer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	got = updated.(Model)
	if cmd != nil || got.step != stepSessionExplorer || got.sessions.plan != nil || !strings.Contains(strings.ToLower(got.sessions.planErr), "self") {
		t.Fatalf("focused self P must stay read-only explorer: step %v plan %#v error %q cmd %v", got.step, got.sessions.plan, got.sessions.planErr, cmd != nil)
	}

	m.sessions.focus = 0
	updated, cmd = m.updateSessionExplorer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	got = updated.(Model)
	if cmd != nil || got.step != stepSessionExplorer || got.sessions.plan != nil || !strings.Contains(strings.ToLower(got.sessions.planErr), "background") {
		t.Fatalf("focused background P must stay read-only explorer: step %v plan %#v error %q cmd %v", got.step, got.sessions.plan, got.sessions.planErr, cmd != nil)
	}
}

func sessionExplorerViewportTestModel(height int) Model {
	m := NewModel(nil, RestoreSettings{})
	m.step = stepSessionExplorer
	m.width = 100
	m.height = height
	m.result.Profile.Driver = "postgres"
	m.sessions.snapshot = &driver.SessionSnapshot{}
	for i := 0; i < 12; i++ {
		m.sessions.snapshot.Sessions = append(m.sessions.snapshot.Sessions, driver.SessionEntry{PID: int32(100 + i), Database: "app", User: "reader", State: "active"})
	}
	return m
}

func updateSessionExplorerKey(t *testing.T, m Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated.(Model)
}
