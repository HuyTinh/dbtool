package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

const migratePreflightTimeout = 10 * time.Second

type migratePreflightState struct {
	source  *driver.SchemaSnapshot
	target  *driver.SchemaSnapshot
	diff    driver.SchemaDiff
	loading bool
	err     error
	offset  int
}

type migratePreflightLoadedMsg struct {
	source *driver.SchemaSnapshot
	target *driver.SchemaSnapshot
	diff   driver.SchemaDiff
	err    error
}

// startMigratePreflight gathers read-only catalog snapshots outside Update.
func (m *Model) startMigratePreflight() tea.Cmd {
	m.migratePreflight = migratePreflightState{loading: true, offset: 0}
	source := m.result.Profile
	target := m.result.DestProfile
	return func() tea.Msg {
		if source.Name == target.Name {
			return migratePreflightLoadedMsg{err: fmt.Errorf("source and destination profiles must be different")}
		}
		if source.Driver != target.Driver {
			return migratePreflightLoadedMsg{err: fmt.Errorf("cross-driver schema preflight is not supported (source: %s, dest: %s)", source.Driver, target.Driver)}
		}
		if source.Driver != "postgres" {
			return migratePreflightLoadedMsg{err: fmt.Errorf("schema preflight is available only for PostgreSQL profiles")}
		}

		drv, err := driver.Get(source.Driver)
		if err != nil {
			return migratePreflightLoadedMsg{err: err}
		}
		inspector, ok := drv.(driver.SchemaInspector)
		if !ok {
			return migratePreflightLoadedMsg{err: fmt.Errorf("driver %q does not support schema snapshots", source.Driver)}
		}

		ctx, cancel := context.WithTimeout(context.Background(), migratePreflightTimeout)
		defer cancel()
		sourceSnapshot, err := inspector.CollectSchema(ctx, source)
		if err != nil {
			return migratePreflightLoadedMsg{err: fmt.Errorf("collect source schema: %w", err)}
		}
		targetSnapshot, err := inspector.CollectSchema(ctx, target)
		if err != nil {
			return migratePreflightLoadedMsg{err: fmt.Errorf("collect target schema: %w", err)}
		}
		return migratePreflightLoadedMsg{
			source: sourceSnapshot,
			target: targetSnapshot,
			diff:   driver.CompareSchemaSnapshots(sourceSnapshot, targetSnapshot),
		}
	}
}

func (m Model) updateMigratePreflight(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r", "R":
		if !m.migratePreflight.loading {
			return m, m.startMigratePreflight()
		}
	case "up", "k", "K":
		m.migratePreflight.offset--
	case "down", "j", "J":
		m.migratePreflight.offset++
	case "pgup":
		m.migratePreflight.offset -= m.migratePreflightVisibleHeight()
	case "pgdown":
		m.migratePreflight.offset += m.migratePreflightVisibleHeight()
	case "home":
		m.migratePreflight.offset = 0
	case "end":
		m.migratePreflight.offset = m.migratePreflightMaxOffset()
	case "enter", "y":
		if !m.migratePreflight.loading {
			m.step = stepMigrateConfirm
		}
	case "esc", "n":
		m.step = stepMigrateSelectDest
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	m.clampMigratePreflightOffset()
	return m, nil
}

const migratePreflightFrameGaps = 2

func (m Model) viewMigratePreflight() string {
	lines := m.migratePreflightContentLines()
	return m.migratePreflightViewport(lines).Render(lines)
}

func (m Model) migratePreflightContentLines() []string {
	var body strings.Builder
	source := m.result.Profile
	target := m.result.DestProfile
	body.WriteString(renderProfileSummaryCard(m.width, "Source Profile", source.Name, source.Driver, source.Host, source.Port, source.Database, source.User))
	body.WriteString("\n")
	body.WriteString(renderProfileSummaryCard(m.width, "Target Profile", target.Name, target.Driver, target.Host, target.Port, target.Database, target.User))
	body.WriteString("\n\n")

	switch {
	case m.migratePreflight.loading:
		body.WriteString(renderCard(m.width, "Schema Comparison", mutedStyle.Render("Collecting read-only PostgreSQL catalog snapshots…")))
	case m.migratePreflight.err != nil:
		body.WriteString(renderCard(m.width, "Schema Preflight Unavailable", errorStyle.Render(m.migratePreflight.err.Error())))
	case m.migratePreflight.source == nil || m.migratePreflight.target == nil:
		body.WriteString(renderCard(m.width, "Schema Comparison", mutedStyle.Render("Press R to collect a read-only schema comparison.")))
	default:
		body.WriteString(renderCard(m.width, "Schema Comparison", renderKeyValueGrid([]kvRow{
			{Label: "Source tables", Value: fmt.Sprintf("%d", len(m.migratePreflight.source.Tables))},
			{Label: "Target tables", Value: fmt.Sprintf("%d", len(m.migratePreflight.target.Tables))},
			{Label: "Missing from target", Value: fmt.Sprintf("%d", len(m.migratePreflight.diff.MissingTables)), Kind: badgeWarning},
			{Label: "Extra on target", Value: fmt.Sprintf("%d", len(m.migratePreflight.diff.ExtraTables)), Kind: badgeWarning},
			{Label: "Column differences", Value: fmt.Sprintf("%d", len(m.migratePreflight.diff.ColumnDifferences)), Kind: badgeWarning},
		}, 20)))
		body.WriteString("\n\n")
		body.WriteString(renderCard(m.width, "Schema Differences", renderMigrateSchemaDiff(m.migratePreflight.diff)))
	}

	body.WriteString("\n\n")
	body.WriteString(mutedStyle.Render("Read-only preflight — catalog metadata is compared; no database objects or data are changed."))
	return strings.Split(body.String(), "\n")
}

func (m Model) migratePreflightVisibleHeight() int {
	return m.migratePreflightViewport(m.migratePreflightContentLines()).Visible
}

func (m Model) migratePreflightViewport(lines []string) fixedDocumentViewport {
	header := renderAppHeader("Migration Preflight", "Review source and target schema differences before migration", m.width)
	return newFixedDocumentViewport(lines, m.height, header, migratePreflightFrameGaps, m.migratePreflight.offset, func(viewport scrollViewport) string {
		return renderCommandBar(m.width, m.migratePreflightFooterHints(len(lines), viewport.Visible, viewport.Offset))
	})
}

func (m Model) migratePreflightMaxOffset() int {
	return scrollMaxOffset(len(m.migratePreflightContentLines()), m.migratePreflightVisibleHeight())
}

func (m *Model) clampMigratePreflightOffset() {
	m.migratePreflight.offset = clampScrollOffset(m.migratePreflight.offset, len(m.migratePreflightContentLines()), m.migratePreflightVisibleHeight())
}

func (m Model) migratePreflightFooterHints(total, visibleHeight, offset int) []keyHint {
	hints := []keyHint{{Key: "R", Label: "refresh"}, {Key: "Enter", Label: "proceed"}}
	if total > visibleHeight {
		viewport := newScrollViewport(total, visibleHeight, offset)
		hints = append(hints,
			keyHint{Key: fmt.Sprintf("%d–%d / %d", viewport.Offset+1, viewport.End, total), Label: "position"},
			keyHint{Key: "↑/k", Label: "up"}, keyHint{Key: "↓/j", Label: "down"},
			keyHint{Key: "PgUp/PgDn", Label: "page"}, keyHint{Key: "Home/End", Label: "bounds"})
	}
	return append(hints, keyHint{Key: "Esc", Label: "back", Danger: true}, keyHint{Key: "Q", Label: "quit", Danger: true})
}

func renderMigrateSchemaDiff(diff driver.SchemaDiff) string {
	var sections []string
	if len(diff.MissingTables) > 0 {
		sections = append(sections, "Missing from target:\n"+strings.Join(diff.MissingTables, "\n"))
	}
	if len(diff.ExtraTables) > 0 {
		sections = append(sections, "Extra on target:\n"+strings.Join(diff.ExtraTables, "\n"))
	}
	if len(diff.ColumnDifferences) > 0 {
		lines := make([]string, 0, len(diff.ColumnDifferences))
		for _, difference := range diff.ColumnDifferences {
			lines = append(lines, difference.Table+"."+difference.Column)
		}
		sections = append(sections, "Column differences:\n"+strings.Join(lines, "\n"))
	}
	if len(sections) == 0 {
		return renderStatusBadge("NO DIFFERENCES", badgeSuccess)
	}
	return strings.Join(sections, "\n\n")
}
