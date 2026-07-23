package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

const healthLongQueryThreshold = 5 * time.Minute
const healthDashboardFrameGaps = 2

type healthDashboardState struct {
	snapshot *driver.HealthSnapshot
	loading  bool
	err      error
	offset   int
}

type healthLoadedMsg struct {
	snapshot *driver.HealthSnapshot
	err      error
}

func (m *Model) startHealthRefresh() tea.Cmd {
	m.health.loading = true
	m.health.err = nil
	m.health.offset = 0
	profile := m.result.Profile
	load := func() tea.Msg {
		if profile.Driver != "postgres" {
			return healthLoadedMsg{err: fmt.Errorf("health is available only for PostgreSQL profiles")}
		}
		drv, err := driver.Get(profile.Driver)
		if err != nil {
			return healthLoadedMsg{err: err}
		}
		collector, ok := drv.(driver.HealthCollector)
		if !ok {
			return healthLoadedMsg{err: fmt.Errorf("driver %q does not support health snapshots", profile.Driver)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		snapshot, err := collector.CollectHealth(ctx, profile, healthLongQueryThreshold)
		return healthLoadedMsg{snapshot: snapshot, err: err}
	}
	return tea.Batch(load, m.startReadOnlyActivity())
}

func (m Model) updateHealthDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.updateHealthDashboardScroll(msg) {
		return m, nil
	}
	switch msg.String() {
	case "r", "R":
		return m, m.startHealthRefresh()
	case "s", "S":
		m.step = stepSizeExplorer
		return m, m.startSizeRefresh()
	case "j", "J":
		m.step = stepSessionExplorer
		return m, m.startSessionsRefresh()
	case "esc":
		m.step = stepSelectProfile
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) updateHealthDashboardScroll(msg tea.KeyMsg) bool {
	if m.healthDashboardMaxOffset() == 0 {
		return false
	}
	offset := m.health.offset
	switch msg.String() {
	case "up", "k", "K":
		offset--
	case "down", "j":
		offset++
	case "pgup":
		offset -= m.healthDashboardVisibleHeight()
	case "pgdown":
		offset += m.healthDashboardVisibleHeight()
	case "home":
		offset = 0
	case "end":
		offset = m.healthDashboardMaxOffset()
	default:
		return false
	}
	m.health.offset = clampScrollOffset(offset, len(m.healthDashboardContentLines()), m.healthDashboardVisibleHeight())
	return true
}

func (m Model) viewHealthDashboard() string {
	lines := m.healthDashboardContentLines()
	return m.healthDashboardViewport(lines).Render(lines)
}

func (m Model) healthDashboardContentLines() []string {
	var body strings.Builder
	p := m.result.Profile
	body.WriteString(renderProfileSummaryCard(m.width, "Health Profile", p.Name, p.Driver, p.Host, p.Port, p.Database, p.User))
	body.WriteString("\n\n")

	if p.Driver != "postgres" {
		body.WriteString(renderCard(m.width, "Health unavailable", errorStyle.Render("Health is supported only for PostgreSQL profiles.")))
	} else if m.health.loading {
		body.WriteString(renderCard(m.width, "Health Snapshot", mutedStyle.Render("Collecting read-only database metrics…")+"\n\n"+renderActivityProgress(progressWidth(m.width), m.activity.frame, m.activity.elapsed)))
	} else if m.health.err != nil {
		body.WriteString(renderCard(m.width, "Health Snapshot Failed", errorStyle.Render(m.health.err.Error())))
	} else if m.health.snapshot == nil {
		body.WriteString(renderCard(m.width, "Health Snapshot", mutedStyle.Render("Press R to collect a read-only database health snapshot.")))
	} else {
		snapshot := m.health.snapshot
		status, kind := healthStatus(snapshot)
		body.WriteString(renderCard(m.width, "Health Snapshot", renderKeyValueGrid([]kvRow{
			{Label: "Status", Value: status, Kind: kind},
			{Label: "Server", Value: snapshot.ServerVersion},
			{Label: "Latency", Value: snapshot.Latency.Round(time.Millisecond).String()},
			{Label: "Database size", Value: formatHealthBytes(snapshot.DatabaseSize)},
			{Label: "Connections", Value: fmt.Sprintf("%d / %d", snapshot.CurrentConnections, snapshot.MaxConnections)},
			{Label: "Active / idle", Value: fmt.Sprintf("%d / %d", snapshot.ActiveConnections, snapshot.IdleConnections)},
			{Label: "Idle transaction", Value: fmt.Sprintf("%d", snapshot.IdleInTransaction)},
			{Label: "Long-running", Value: fmt.Sprintf("%d (> %s)", snapshot.LongRunningQueries, healthLongQueryThreshold)},
			{Label: "Blocked sessions", Value: fmt.Sprintf("%d", snapshot.BlockedSessions)},
		}, 16)))
		if unavailable := unavailableHealthCapabilities(snapshot.Capabilities); len(unavailable) > 0 {
			body.WriteString("\n\n")
			body.WriteString(renderCard(m.width, "Unavailable Metrics", warningStyle.Render(strings.Join(unavailable, "\n"))))
		}
	}
	body.WriteString("\n\n")
	body.WriteString(mutedStyle.Render("Read-only dashboard — no query is cancelled and no database setting is changed."))
	return strings.Split(body.String(), "\n")
}

func (m Model) healthDashboardViewport(lines []string) fixedDocumentViewport {
	header := renderAppHeader("Health Dashboard", "Read-only PostgreSQL status, sessions, and blockers", m.width)
	return newFixedDocumentViewport(lines, m.height, header, healthDashboardFrameGaps, m.health.offset, func(viewport scrollViewport) string {
		return renderCommandBar(m.width, m.healthDashboardFooterHints(viewport))
	})
}

func (m Model) healthDashboardVisibleHeight() int {
	return m.healthDashboardViewport(m.healthDashboardContentLines()).Visible
}

func (m Model) healthDashboardMaxOffset() int {
	return scrollMaxOffset(len(m.healthDashboardContentLines()), m.healthDashboardVisibleHeight())
}

func (m *Model) clampHealthDashboardOffset() {
	m.health.offset = clampScrollOffset(m.health.offset, len(m.healthDashboardContentLines()), m.healthDashboardVisibleHeight())
}

func (m Model) healthDashboardFooterHints(viewport scrollViewport) []keyHint {
	hints := []keyHint{
		{Key: "R", Label: "refresh"},
		{Key: "S", Label: "size explorer"},
		{Key: "J", Label: "session explorer"},
	}
	if viewport.Total > viewport.Visible {
		hints = append(hints,
			keyHint{Key: fmt.Sprintf("%d–%d / %d", viewport.Offset+1, viewport.End, viewport.Total), Label: "position"},
			keyHint{Key: "↑/k", Label: "up"}, keyHint{Key: "↓/j", Label: "down"},
			keyHint{Key: "PgUp/PgDn", Label: "page"}, keyHint{Key: "Home/End", Label: "bounds"})
	}
	return append(hints, keyHint{Key: "Esc", Label: "profiles", Danger: true}, keyHint{Key: "Q", Label: "quit", Danger: true})
}

func healthStatus(snapshot *driver.HealthSnapshot) (string, badgeKind) {
	if snapshot.IdleInTransaction > 0 || snapshot.LongRunningQueries > 0 || snapshot.BlockedSessions > 0 || len(unavailableHealthCapabilities(snapshot.Capabilities)) > 0 {
		return "WARN", badgeWarning
	}
	return "OK", badgeSuccess
}

func unavailableHealthCapabilities(capabilities []driver.HealthCapability) []string {
	var unavailable []string
	for _, capability := range capabilities {
		if !capability.Available {
			unavailable = append(unavailable, capability.Name+": "+capability.Message)
		}
	}
	return unavailable
}

func formatHealthBytes(size int64) string {
	switch {
	case size < 1024:
		return fmt.Sprintf("%d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(size)/1024)
	case size < 1024*1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(size)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GiB", float64(size)/(1024*1024*1024))
	}
}
