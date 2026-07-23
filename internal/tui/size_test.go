package tui

import (
	"fmt"
	"strings"
	"testing"

	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestSizeExplorerTruncatesLongRelationAndIndexNames(t *testing.T) {
	longName := "asset_component_template_structures_log_with_extra_suffix"
	relations := renderSizeRelations([]driver.SizeRelation{{Schema: "cmms", Name: longName}})
	indexes := renderSizeIndexes([]driver.SizeIndex{{Schema: "cmms", Name: longName, TableName: longName}})
	if strings.Contains(relations, longName) || strings.Contains(indexes, longName) {
		t.Fatal("long names must be truncated to preserve the size explorer table layout")
	}
}

func TestSizeExplorerScrollKeysNavigateOneLineAndOnePage(t *testing.T) {
	m := sizeExplorerTestModel(12)
	page := m.sizeExplorerVisibleHeight()
	if page < 1 {
		t.Fatalf("visible height = %d, want positive", page)
	}

	m = updateSizeExplorerKey(t, m, tea.KeyDown)
	if m.size.offset != 1 {
		t.Fatalf("down offset = %d, want 1", m.size.offset)
	}

	m = updateSizeExplorerKey(t, m, tea.KeyPgDown)
	if m.size.offset != 1+page {
		t.Fatalf("page down offset = %d, want %d", m.size.offset, 1+page)
	}

	m = updateSizeExplorerKey(t, m, tea.KeyPgUp)
	if m.size.offset != 1 {
		t.Fatalf("page up offset = %d, want 1", m.size.offset)
	}

	m = updateSizeExplorerKey(t, m, tea.KeyUp)
	if m.size.offset != 0 {
		t.Fatalf("up offset = %d, want 0", m.size.offset)
	}
}

func TestSizeExplorerScrollClampsAtBoundsAndAfterResizeOrRefresh(t *testing.T) {
	m := sizeExplorerTestModel(12)
	m = updateSizeExplorerKey(t, m, tea.KeyEnd)
	if m.size.offset != m.sizeExplorerMaxOffset() || m.size.offset == 0 {
		t.Fatalf("end offset = %d, want max positive offset %d", m.size.offset, m.sizeExplorerMaxOffset())
	}

	m = updateSizeExplorerKey(t, m, tea.KeyPgDown)
	if m.size.offset != m.sizeExplorerMaxOffset() {
		t.Fatalf("page down past end offset = %d, want %d", m.size.offset, m.sizeExplorerMaxOffset())
	}
	m = updateSizeExplorerKey(t, m, tea.KeyHome)
	if m.size.offset != 0 {
		t.Fatalf("home offset = %d, want 0", m.size.offset)
	}

	m = updateSizeExplorerKey(t, m, tea.KeyEnd)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 200})
	m = updated.(Model)
	if m.size.offset != 0 {
		t.Fatalf("resize must clamp offset to 0 when all content fits, got %d", m.size.offset)
	}

	m.height = 12
	m.size.offset = 999
	updated, _ = m.Update(sizeLoadedMsg{snapshot: &driver.SizeSnapshot{}})
	m = updated.(Model)
	if m.size.offset != m.sizeExplorerMaxOffset() {
		t.Fatalf("refresh offset = %d, want clamped max %d", m.size.offset, m.sizeExplorerMaxOffset())
	}
}

func TestSizeExplorerFrameFitsTerminalHeight(t *testing.T) {
	m := sizeExplorerTestModel(12)
	if got := lipgloss.Height(m.viewSizeExplorer()); got > m.height {
		t.Fatalf("rendered size explorer height = %d, exceeds terminal height %d", got, m.height)
	}
}

func TestSizeExplorerFooterShowsPositionAndScrollHintsOnlyWhenNeeded(t *testing.T) {
	m := sizeExplorerTestModel(12)
	view := m.viewSizeExplorer()
	wantPosition := fmt.Sprintf("1–%d / %d", m.sizeExplorerVisibleHeight(), len(m.sizeExplorerContentLines()))
	if !strings.Contains(view, wantPosition) {
		t.Fatalf("overflow footer missing position %q:\n%s", wantPosition, view)
	}
	if !strings.Contains(view, "↑/k") || !strings.Contains(view, "↓/j") {
		t.Fatalf("overflow footer missing scroll hints:\n%s", view)
	}

	m.height = 200
	view = m.viewSizeExplorer()
	if strings.Contains(view, "↑/k") || strings.Contains(view, "↓/j") {
		t.Fatalf("non-overflow footer must not show scroll hints:\n%s", view)
	}
}

func sizeExplorerTestModel(height int) Model {
	m := NewModel(nil, RestoreSettings{})
	m.step = stepSizeExplorer
	m.width = 100
	m.height = height
	m.result.Profile.Driver = "postgres"
	for i := 0; i < 12; i++ {
		m.size.snapshot = &driver.SizeSnapshot{}
		m.size.snapshot.Relations = append(m.size.snapshot.Relations, driver.SizeRelation{Name: fmt.Sprintf("relation_%02d", i)})
		m.size.snapshot.Indexes = append(m.size.snapshot.Indexes, driver.SizeIndex{Name: fmt.Sprintf("index_%02d", i)})
	}
	return m
}

func updateSizeExplorerKey(t *testing.T, m Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated.(Model)
}
