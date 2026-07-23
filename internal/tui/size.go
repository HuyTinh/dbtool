package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

const sizeExplorerLimit = 10

type sizeExplorerState struct {
	snapshot *driver.SizeSnapshot
	loading  bool
	err      error
	offset   int
}

type sizeLoadedMsg struct {
	snapshot *driver.SizeSnapshot
	err      error
}

func (m *Model) startSizeRefresh() tea.Cmd {
	m.size.loading = true
	m.size.err = nil
	m.size.offset = 0
	profile := m.result.Profile
	load := func() tea.Msg {
		if profile.Driver != "postgres" {
			return sizeLoadedMsg{err: fmt.Errorf("size explorer is available only for PostgreSQL profiles")}
		}
		drv, err := driver.Get(profile.Driver)
		if err != nil {
			return sizeLoadedMsg{err: err}
		}
		collector, ok := drv.(driver.SizeCollector)
		if !ok {
			return sizeLoadedMsg{err: fmt.Errorf("driver %q does not support size snapshots", profile.Driver)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		snapshot, err := collector.CollectSize(ctx, profile, "", sizeExplorerLimit)
		return sizeLoadedMsg{snapshot: snapshot, err: err}
	}
	return tea.Batch(load, m.startReadOnlyActivity())
}

func (m Model) updateSizeExplorer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r", "R":
		return m, m.startSizeRefresh()
	case "up", "k", "K":
		m.size.offset--
	case "down", "j", "J":
		m.size.offset++
	case "pgup":
		m.size.offset -= m.sizeExplorerVisibleHeight()
	case "pgdown":
		m.size.offset += m.sizeExplorerVisibleHeight()
	case "home":
		m.size.offset = 0
	case "end":
		m.size.offset = m.sizeExplorerMaxOffset()
	case "esc":
		m.step = stepHealthDashboard
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	m.clampSizeExplorerOffset()
	return m, nil
}

const sizeExplorerFrameGaps = 2

func (m Model) viewSizeExplorer() string {
	lines := m.sizeExplorerContentLines()
	return m.sizeExplorerViewport(lines).Render(lines)
}

func (m Model) sizeExplorerContentLines() []string {
	var body strings.Builder
	p := m.result.Profile
	body.WriteString(renderProfileSummaryCard(m.width, "Size Explorer Profile", p.Name, p.Driver, p.Host, p.Port, p.Database, p.User))
	body.WriteString("\n\n")

	if p.Driver != "postgres" {
		body.WriteString(renderCard(m.width, "Size Explorer unavailable", errorStyle.Render("Size Explorer is supported only for PostgreSQL profiles.")))
	} else if m.size.loading {
		body.WriteString(renderCard(m.width, "Storage Snapshot", mutedStyle.Render("Collecting read-only catalog storage metadata…")+"\n\n"+renderActivityProgress(progressWidth(m.width), m.activity.frame, m.activity.elapsed)))
	} else if m.size.err != nil {
		body.WriteString(renderCard(m.width, "Storage Snapshot Failed", errorStyle.Render(m.size.err.Error())))
	} else if m.size.snapshot == nil {
		body.WriteString(renderCard(m.width, "Storage Snapshot", mutedStyle.Render("Press R to collect a read-only storage snapshot.")))
	} else {
		snapshot := m.size.snapshot
		body.WriteString(renderCard(m.width, "Storage Snapshot", renderKeyValueGrid([]kvRow{
			{Label: "Database size", Value: formatHealthBytes(snapshot.DatabaseSize)},
			{Label: "Relations shown", Value: fmt.Sprintf("%d", len(snapshot.Relations))},
			{Label: "Indexes shown", Value: fmt.Sprintf("%d", len(snapshot.Indexes))},
		}, 16)))
		body.WriteString("\n\n")
		body.WriteString(renderCard(m.width, "Largest Relations", renderSizeRelations(snapshot.Relations)))
		body.WriteString("\n\n")
		body.WriteString(renderCard(m.width, "Largest Indexes", renderSizeIndexes(snapshot.Indexes)))
	}
	body.WriteString("\n\n")
	body.WriteString(mutedStyle.Render("Read-only catalog metadata. Row counts are estimates; no ANALYZE, VACUUM, or REINDEX is run."))
	return strings.Split(body.String(), "\n")
}

func (m Model) sizeExplorerVisibleHeight() int {
	return m.sizeExplorerViewport(m.sizeExplorerContentLines()).Visible
}

func (m Model) sizeExplorerViewport(lines []string) fixedDocumentViewport {
	header := renderAppHeader("Size Explorer", "Largest PostgreSQL relations and indexes", m.width)
	return newFixedDocumentViewport(lines, m.height, header, sizeExplorerFrameGaps, m.size.offset, func(viewport scrollViewport) string {
		return renderCommandBar(m.width, m.sizeExplorerFooterHints(len(lines), viewport.Visible, viewport.Offset))
	})
}

func (m Model) sizeExplorerMaxOffset() int {
	return scrollMaxOffset(len(m.sizeExplorerContentLines()), m.sizeExplorerVisibleHeight())
}

func (m *Model) clampSizeExplorerOffset() {
	m.size.offset = clampScrollOffset(m.size.offset, len(m.sizeExplorerContentLines()), m.sizeExplorerVisibleHeight())
}

func (m Model) sizeExplorerFooterHints(total, visibleHeight, offset int) []keyHint {
	viewport := newScrollViewport(total, visibleHeight, offset)
	hints := []keyHint{
		{Key: fmt.Sprintf("%d–%d / %d", viewport.Offset+1, viewport.End, total), Label: "position"},
		{Key: "R", Label: "refresh"},
	}
	if total > visibleHeight {
		hints = append(hints, keyHint{Key: "↑/k", Label: "up"}, keyHint{Key: "↓/j", Label: "down"}, keyHint{Key: "PgUp/PgDn", Label: "page"}, keyHint{Key: "Home/End", Label: "bounds"})
	}
	return append(hints, keyHint{Key: "Esc", Label: "health", Danger: true}, keyHint{Key: "Q", Label: "quit", Danger: true})
}

func renderSizeRelations(relations []driver.SizeRelation) string {
	if len(relations) == 0 {
		return mutedStyle.Render("No user relations found.")
	}
	var body strings.Builder
	body.WriteString(mutedStyle.Render("Relation                              Rows est.      Table    Indexes      Toast      Total"))
	for _, relation := range relations {
		body.WriteString("\n")
		body.WriteString(fmt.Sprintf("%-36s %10d %10s %10s %10s %10s", truncateSizeLabel(relation.Schema+"."+relation.Name, 36), relation.EstimatedRows, formatHealthBytes(relation.TableBytes), formatHealthBytes(relation.IndexBytes), formatHealthBytes(relation.ToastBytes), formatHealthBytes(relation.TotalBytes)))
	}
	return body.String()
}

func renderSizeIndexes(indexes []driver.SizeIndex) string {
	if len(indexes) == 0 {
		return mutedStyle.Render("No user indexes found.")
	}
	var body strings.Builder
	body.WriteString(mutedStyle.Render("Index                                   Table                 Size"))
	for _, index := range indexes {
		body.WriteString("\n")
		body.WriteString(fmt.Sprintf("%-36s %-20s %10s", truncateSizeLabel(index.Schema+"."+index.Name, 36), truncateSizeLabel(index.TableName, 20), formatHealthBytes(index.SizeBytes)))
	}
	return body.String()
}

func truncateSizeLabel(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}
