package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

const sessionExplorerLimit = 20

type sessionExplorerState struct {
	snapshot *driver.SessionSnapshot
	loading  bool
	err      error
	plan     *driver.SessionEntry
	planErr  string
	focus    int
	offset   int
}

type sessionsLoadedMsg struct {
	snapshot *driver.SessionSnapshot
	err      error
}

func (m *Model) startSessionsRefresh() tea.Cmd {
	m.sessions.loading = true
	m.sessions.err = nil
	m.sessions.planErr = ""
	m.sessions.focus = 0
	m.sessions.offset = 0
	profile := m.result.Profile
	load := func() tea.Msg {
		if profile.Driver != "postgres" {
			return sessionsLoadedMsg{err: fmt.Errorf("session explorer is available only for PostgreSQL profiles")}
		}
		drv, err := driver.Get(profile.Driver)
		if err != nil {
			return sessionsLoadedMsg{err: err}
		}
		collector, ok := drv.(driver.SessionCollector)
		if !ok {
			return sessionsLoadedMsg{err: fmt.Errorf("driver %q does not support session snapshots", profile.Driver)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		snapshot, err := collector.CollectSessions(ctx, profile, "", sessionExplorerLimit)
		return sessionsLoadedMsg{snapshot: snapshot, err: err}
	}
	return tea.Batch(load, m.startReadOnlyActivity())
}

func (m Model) updateSessionExplorer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r", "R":
		return m, m.startSessionsRefresh()
	case "up", "k", "K":
		if m.sessions.focus > 0 {
			m.sessions.focus--
		}
	case "down", "j", "J":
		if m.sessions.snapshot != nil && m.sessions.focus < len(m.sessions.snapshot.Sessions)-1 {
			m.sessions.focus++
		}
	case "pgup":
		m.sessions.focus = m.sessionExplorerPageIndex(-1)
	case "pgdown":
		m.sessions.focus = m.sessionExplorerPageIndex(1)
	case "home":
		m.sessions.focus = 0
	case "end":
		if m.sessions.snapshot != nil {
			m.sessions.focus = len(m.sessions.snapshot.Sessions) - 1
		}
	case "p", "P":
		if m.sessions.snapshot != nil && len(m.sessions.snapshot.Sessions) > 0 {
			m.sessions.focus = clampIndex(m.sessions.focus, len(m.sessions.snapshot.Sessions))
			selected := &m.sessions.snapshot.Sessions[m.sessions.focus]
			if selected.PID == m.sessions.snapshot.SelfPID {
				m.sessions.plan = nil
				m.sessions.planErr = fmt.Sprintf("PLAN ONLY refused the focused collector self PID %d.", m.sessions.snapshot.SelfPID)
				return m, nil
			}
			if selected.State == "" {
				m.sessions.plan = nil
				m.sessions.planErr = "PLAN ONLY refused the focused background worker; choose a client session with a state."
				return m, nil
			}
			m.sessions.plan = selected
			m.sessions.planErr = ""
			m.step = stepSessionActionPlan
		}
	case "esc":
		m.step = stepHealthDashboard
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	m.syncSessionExplorerViewport()
	return m, nil
}

const sessionExplorerFrameGaps = 2

func (m Model) viewSessionExplorer() string {
	document := m.sessionExplorerDocument()
	return document.fixed.Render(document.lines)
}

type sessionExplorerDocument struct {
	fixed fixedDocumentViewport
	lines []string
	rows  []int
}

func (m Model) sessionExplorerDocument() sessionExplorerDocument {
	var body strings.Builder
	p := m.result.Profile
	body.WriteString(renderProfileSummaryCard(m.width, "Session Explorer Profile", p.Name, p.Driver, p.Host, p.Port, p.Database, p.User))
	body.WriteString("\n\n")

	if p.Driver != "postgres" {
		body.WriteString(renderCard(m.width, "Session Explorer unavailable", errorStyle.Render("Session Explorer is supported only for PostgreSQL profiles.")))
	} else if m.sessions.loading {
		body.WriteString(renderCard(m.width, "Session Snapshot", mutedStyle.Render("Collecting read-only PostgreSQL session metadata…")+"\n\n"+renderActivityProgress(progressWidth(m.width), m.activity.frame, m.activity.elapsed)))
	} else if m.sessions.err != nil {
		body.WriteString(renderCard(m.width, "Session Snapshot Failed", errorStyle.Render(m.sessions.err.Error())))
	} else if m.sessions.snapshot == nil {
		body.WriteString(renderCard(m.width, "Session Snapshot", mutedStyle.Render("Press R to collect a read-only session snapshot.")))
	} else {
		body.WriteString(renderCard(m.width, "Sessions", renderFocusedSessions(m.sessions.snapshot.Sessions, m.sessions.focus)))
	}
	if m.sessions.planErr != "" {
		body.WriteString("\n\n")
		body.WriteString(renderCard(m.width, "Plan Safety", warningStyle.Render(m.sessions.planErr)))
	}
	body.WriteString("\n\n")
	body.WriteString(mutedStyle.Render("Read-only session metadata — query text and connection secrets are never collected or shown."))

	lines := strings.Split(body.String(), "\n")
	rows := m.sessionExplorerRowLines(lines)
	header := renderAppHeader("Session Explorer", "PostgreSQL session state, waits, and blockers", m.width)
	newViewport := func(offset int) fixedDocumentViewport {
		return newFixedDocumentViewport(lines, m.height, header, sessionExplorerFrameGaps, offset, func(viewport scrollViewport) string {
			return renderCommandBar(m.width, m.sessionExplorerFooterHints(viewport))
		})
	}
	fixed := newViewport(m.sessions.offset)
	if len(rows) > 0 {
		focus := clampIndex(m.sessions.focus, len(rows))
		fixed = newViewport(keepScrollIndexVisible(rows[focus], len(lines), fixed.Visible, fixed.Offset))
	}
	return sessionExplorerDocument{fixed: fixed, lines: lines, rows: rows}
}

func (m Model) sessionExplorerRowLines(lines []string) []int {
	if m.sessions.snapshot == nil || len(m.sessions.snapshot.Sessions) == 0 {
		return nil
	}
	for line, value := range lines {
		if strings.Contains(value, "PID     Database") {
			rows := make([]int, len(m.sessions.snapshot.Sessions))
			for index := range rows {
				rows[index] = line + index + 1
			}
			return rows
		}
	}
	return nil
}

func (m Model) sessionExplorerPageIndex(direction int) int {
	document := m.sessionExplorerDocument()
	return selectorPageIndex(m.sessions.focus, document.rows, document.fixed.Visible, direction)
}

func (m *Model) syncSessionExplorerViewport() {
	if m.sessions.snapshot == nil || len(m.sessions.snapshot.Sessions) == 0 {
		m.sessions.focus = 0
		m.sessions.offset = 0
		return
	}
	document := m.sessionExplorerDocument()
	m.sessions.focus = clampIndex(m.sessions.focus, len(document.rows))
	if len(document.rows) == 0 {
		m.sessions.offset = 0
		return
	}
	m.sessions.offset = keepScrollIndexVisible(document.rows[m.sessions.focus], len(document.lines), document.fixed.Visible, document.fixed.Offset)
}

func (m Model) sessionExplorerFooterHints(viewport scrollViewport) []keyHint {
	hints := []keyHint{{Key: "R", Label: "refresh"}, {Key: "P", Label: "plan detail"}}
	if viewport.Total > viewport.Visible {
		hints = append([]keyHint{{Key: fmt.Sprintf("%d–%d / %d", viewport.Offset+1, viewport.End, viewport.Total), Label: "position"}}, hints...)
		hints = append(hints, keyHint{Key: "↑/k", Label: "focus"}, keyHint{Key: "↓/j", Label: "focus"}, keyHint{Key: "PgUp/PgDn", Label: "page"}, keyHint{Key: "Home/End", Label: "bounds"})
	}
	return append(hints, keyHint{Key: "Esc", Label: "health", Danger: true}, keyHint{Key: "Q", Label: "quit", Danger: true})
}

func (m Model) updateSessionActionPlan(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.step = stepSessionExplorer
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewSessionActionPlan() string {
	if m.sessions.plan == nil {
		return m.viewSessionExplorer()
	}
	session := *m.sessions.plan
	blockers := "-"
	if len(session.BlockingPIDs) > 0 {
		pids := make([]string, len(session.BlockingPIDs))
		for i, pid := range session.BlockingPIDs {
			pids[i] = fmt.Sprintf("%d", pid)
		}
		blockers = strings.Join(pids, ",")
	}

	var body strings.Builder
	body.WriteString(renderDanger("PLAN ONLY — this screen never executes SQL or changes PostgreSQL."))
	body.WriteString("\n\n")
	body.WriteString(renderCard(m.width, "Selected Session", renderKeyValueGrid([]kvRow{
		{Label: "PID", Value: fmt.Sprintf("%d", session.PID)},
		{Label: "State", Value: session.State},
		{Label: "Wait", Value: strings.TrimSpace(session.WaitEventType + " " + session.WaitEvent)},
		{Label: "Blockers", Value: blockers},
		{Label: "Query age", Value: fmt.Sprintf("%dms", session.QueryAgeMS)},
	}, 14)))
	body.WriteString("\n\n")
	body.WriteString(renderCard(m.width, "SQL Preview", warningStyle.Render(fmt.Sprintf("Cancel preview: SELECT pg_cancel_backend(%d);\nTerminate preview: SELECT pg_terminate_backend(%d);", session.PID, session.PID))))
	body.WriteString("\n\n")
	body.WriteString(mutedStyle.Render("Risk: either preview can disrupt in-flight work; investigate blockers and application ownership first. This PLAN ONLY detail never executes SQL."))
	return renderScreenFrame(m.width, "Session Action Plan", "PLAN ONLY — read-only preview", body.String(), []keyHint{
		{Key: "Esc", Label: "sessions"},
		{Key: "Q", Label: "quit", Danger: true},
	})
}

func renderSessions(sessions []driver.SessionEntry) string {
	return renderFocusedSessions(sessions, -1)
}

func renderFocusedSessions(sessions []driver.SessionEntry, focus int) string {
	if len(sessions) == 0 {
		return mutedStyle.Render("No sessions found for the current filter.")
	}
	var body strings.Builder
	body.WriteString(mutedStyle.Render("PID     Database     User         State          Wait type  Wait event   Age    Blockers"))
	for index, session := range sessions {
		blockers := "-"
		if len(session.BlockingPIDs) > 0 {
			pids := make([]string, len(session.BlockingPIDs))
			for i, pid := range session.BlockingPIDs {
				pids[i] = fmt.Sprintf("%d", pid)
			}
			blockers = strings.Join(pids, ",")
		}
		body.WriteString("\n")
		marker := "  "
		if index == focus {
			marker = "> "
		}
		body.WriteString(marker + fmt.Sprintf("%-7d %-12s %-12s %-14s %-10s %-12s %6dms %s", session.PID, truncateSizeLabel(session.Database, 12), truncateSizeLabel(session.User, 12), truncateSizeLabel(session.State, 14), truncateSizeLabel(session.WaitEventType, 10), truncateSizeLabel(session.WaitEvent, 12), session.QueryAgeMS, blockers))
	}
	return body.String()
}
