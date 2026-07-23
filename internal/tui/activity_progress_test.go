package tui

import (
	"strings"
	"testing"
	"time"
)

func TestRenderActivityProgressShowsBoundedIndeterminateBar(t *testing.T) {
	bar := renderActivityProgress(12, 0, 0)
	if strings.Count(bar, "█") != 4 || strings.Count(bar, "░") != 8 {
		t.Fatalf("activity bar = %q, want 4 filled and 8 empty segments", bar)
	}
}

func TestReadOnlyActivityTickAdvancesAndStopsWhenNoExplorerIsLoading(t *testing.T) {
	m := NewModel(nil, RestoreSettings{})
	m.health.loading = true
	m.activity.startedAt = time.Unix(0, 0)

	updated, cmd := m.Update(readOnlyActivityTickMsg(time.Unix(2, 0)))
	got := updated.(Model)
	if got.activity.frame != 1 {
		t.Fatalf("activity frame = %d, want 1", got.activity.frame)
	}
	if cmd == nil {
		t.Fatal("loading explorer tick must schedule another tick")
	}

	updated, cmd = got.Update(healthLoadedMsg{})
	got = updated.(Model)
	if got.activity.elapsed != 2*time.Second {
		t.Fatalf("activity elapsed = %s, want 2s", got.activity.elapsed)
	}
	if cmd != nil {
		t.Fatal("completed explorers must not schedule another activity tick")
	}
}

func TestLoadingViewsShowSpinnerAndElapsedTime(t *testing.T) {
	for _, tc := range []struct {
		name string
		view func(Model) string
		set  func(*Model)
	}{
		{"health", func(m Model) string { return m.viewHealthDashboard() }, func(m *Model) { m.health.loading = true }},
		{"sessions", func(m Model) string { return m.viewSessionExplorer() }, func(m *Model) { m.sessions.loading = true }},
		{"size", func(m Model) string { return m.viewSizeExplorer() }, func(m *Model) { m.size.loading = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(nil, RestoreSettings{})
			m.width = 80
			m.result.Profile.Driver = "postgres"
			m.activity = readOnlyActivityState{frame: 1, elapsed: 2 * time.Second}
			tc.set(&m)

			view := tc.view(m)
			for _, marker := range []string{"⠙", "Elapsed: 2s"} {
				if !strings.Contains(view, marker) {
					t.Fatalf("loading view missing %q:\n%s", marker, view)
				}
			}
		})
	}
}
