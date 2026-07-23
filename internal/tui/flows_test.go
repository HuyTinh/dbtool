package tui

import (
	"fmt"
	"strings"
	"testing"

	"dbtool/internal/config"
	"dbtool/internal/flows"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestApplyFlowPrefillsRestoreAndRoutesToReview(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"target": {Name: "target", Driver: "postgres"}}}
	m := NewModel(cfg, RestoreSettings{})
	flow := flows.Flow{Operation: flows.OperationRestore, Profile: "target", FilePath: "/backups/app.dump", Settings: flows.Settings{Jobs: 4, Clean: true}}
	if err := m.applyFlow(flow); err != nil {
		t.Fatal(err)
	}
	if m.step != stepConfirm {
		t.Fatalf("step = %v, want review stepConfirm", m.step)
	}
	if m.result.File != flow.FilePath || m.result.Profile.Name != "target" || m.result.Settings.Jobs != 4 || !m.result.Settings.Clean {
		t.Fatalf("flow was not prefilling restore review: %#v", m.result)
	}
}

func TestApplyMigrateFlowRoutesThroughPreflight(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{"source": {Name: "source", Driver: "postgres"}, "target": {Name: "target", Driver: "postgres"}}}
	m := NewModel(cfg, RestoreSettings{})
	flow := flows.Flow{Operation: flows.OperationMigrate, Profile: "source", DestinationProfile: "target", Settings: flows.Settings{Jobs: 2, SchemaOnly: true}}
	if err := m.applyFlow(flow); err != nil {
		t.Fatal(err)
	}
	if m.step != stepMigratePreflight || m.result.DestProfile.Name != "target" || !m.result.MigrateSettings.SchemaOnly {
		t.Fatalf("flow did not route through preflight: step=%v result=%#v", m.step, m.result)
	}
}

func TestFlowsSelectionScrollsThroughEveryRecentAndStaysInViewport(t *testing.T) {
	m := flowsScrollTestModel(12, 24)
	page := m.flowVisibleHeight()
	if page < 1 || m.flowMaxOffset() == 0 {
		t.Fatalf("expected a positive viewport with overflow, got page=%d max=%d", page, m.flowMaxOffset())
	}

	for i := 0; i < len(m.flowRecent)-1; i++ {
		m = updateFlowsKey(t, m, tea.KeyDown)
	}
	if m.flowIdx != len(m.flowRecent)-1 {
		t.Fatalf("selected index = %d, want final flow %d", m.flowIdx, len(m.flowRecent)-1)
	}
	if m.flowIdx < m.flowViewportOffset || m.flowIdx >= m.flowViewportOffset+m.flowVisibleHeight() {
		t.Fatalf("selected index %d is outside viewport [%d, %d)", m.flowIdx, m.flowViewportOffset, m.flowViewportOffset+m.flowVisibleHeight())
	}
	if !strings.Contains(m.View(), "flow_23") {
		t.Fatalf("bottom viewport does not show selected final flow:\n%s", m.View())
	}
}

func TestFlowsPageAndBoundsNavigationClampAndKeepFrameFixed(t *testing.T) {
	m := flowsScrollTestModel(12, 24)
	page := m.flowVisibleHeight()
	m = updateFlowsKey(t, m, tea.KeyPgDown)
	if m.flowIdx != min(page, len(m.flowRecent)-1) {
		t.Fatalf("page down selection = %d, want %d", m.flowIdx, min(page, len(m.flowRecent)-1))
	}
	m = updateFlowsKey(t, m, tea.KeyEnd)
	if m.flowIdx != len(m.flowRecent)-1 || m.flowViewportOffset != m.flowMaxOffset() {
		t.Fatalf("end = index %d offset %d, want index %d offset %d", m.flowIdx, m.flowViewportOffset, len(m.flowRecent)-1, m.flowMaxOffset())
	}
	if got := lipgloss.Height(m.View()); got > m.height {
		t.Fatalf("rendered flows height = %d, exceeds terminal height %d", got, m.height)
	}
	m = updateFlowsKey(t, m, tea.KeyHome)
	if m.flowIdx != 0 || m.flowViewportOffset != 0 {
		t.Fatalf("home = index %d offset %d, want 0/0", m.flowIdx, m.flowViewportOffset)
	}
}

func TestFlowsOnlyShowPositionAndScrollHintsWhenOverflowing(t *testing.T) {
	m := flowsScrollTestModel(12, 24)
	if view := m.View(); !strings.Contains(view, "position") || !strings.Contains(view, "PgUp/PgDn") {
		t.Fatalf("overflow footer missing position and scroll hints:\n%s", view)
	}
	m.height = 200
	if view := m.View(); strings.Contains(view, "position") || strings.Contains(view, "PgUp/PgDn") {
		t.Fatalf("non-overflow footer must omit position and scroll hints:\n%s", view)
	}
}

func TestFlowsTabSwitchResetsSelectionAndViewportForPinnedFlows(t *testing.T) {
	m := flowsScrollTestModel(12, 24)
	m.flowPinned = []flows.Flow{{Operation: flows.OperationDump, Profile: "pinned_0"}, {Operation: flows.OperationDump, Profile: "pinned_1"}}
	m = updateFlowsKey(t, m, tea.KeyEnd)
	m = updateFlowsKey(t, m, tea.KeyTab)
	if !m.flowPinnedTab || m.flowIdx != 0 || m.flowViewportOffset != 0 {
		t.Fatalf("tab switch = pinned:%v index:%d offset:%d, want true/0/0", m.flowPinnedTab, m.flowIdx, m.flowViewportOffset)
	}
	if view := m.View(); !strings.Contains(view, "Pinned flows") || !strings.Contains(view, "pinned_0") {
		t.Fatalf("pinned tab did not render its selected content:\n%s", view)
	}
}

func updateFlowsKey(t *testing.T, m Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated.(Model)
}

func flowsScrollTestModel(height, count int) Model {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepFlows
	m.width = 100
	m.height = height
	for i := 0; i < count; i++ {
		m.flowRecent = append(m.flowRecent, flows.Flow{Operation: flows.OperationRestore, Profile: fmt.Sprintf("flow_%02d", i), FilePath: fmt.Sprintf("/very/long/path/to/flow_%02d.backup", i)})
	}
	return m
}
