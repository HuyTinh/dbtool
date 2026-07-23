package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"dbtool/internal/pitr"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type pitrDashboardState struct {
	config             *pitr.PITRConfig
	status             pitr.Status
	notice             string
	planTitle          string
	planBody           string
	target             textinput.Model
	plan               pitr.RecoveryPlan
	dashboardOffset    int
	recoveryPlanOffset int
}

const pitrDocumentFrameGaps = 2

func (m *Model) refreshPITRDashboard() {
	m.pitr = pitrDashboardState{}
	if m.result.Profile.Driver != "postgres" {
		m.pitr.notice = "PITR is available only for PostgreSQL profiles"
		return
	}
	cfg, err := pitr.LoadPITRConfig(m.result.Profile.Name)
	if err != nil {
		m.pitr.notice = "PITR is not configured for this profile. Review the setup plan before using the CLI setup command."
		return
	}
	backups, backupErr := pitr.ListBackups(cfg.BaseBackupDir)
	walFiles, walErr := pitr.ListWALFiles(cfg.ArchiveDir)
	m.pitr.config = cfg
	m.pitr.status = pitr.BuildStatus(cfg, backups, walFiles)
	var notices []string
	if backupErr != nil {
		notices = append(notices, "Cannot read base backups: "+backupErr.Error())
	}
	if walErr != nil {
		notices = append(notices, "Cannot read WAL archive: "+walErr.Error())
	}
	m.pitr.notice = strings.Join(notices, "\n")
}

func (m Model) updatePITRDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.updatePITRDashboardScroll(msg) {
		return m, nil
	}
	if m.result.Profile.Driver != "postgres" {
		switch msg.String() {
		case "esc":
			m.step = stepSelectProfile
		case "q", "ctrl+c":
			return m, tea.Quit
		default:
			m.pitr.notice = "PITR plans are unavailable because the selected profile is not PostgreSQL"
		}
		return m, nil
	}
	switch msg.String() {
	case "r", "R":
		m.refreshPITRDashboard()
	case "s", "S":
		m.pitr.dashboardOffset = 0
		m.pitr.planTitle = "Setup Plan"
		profileDir, err := pitr.GetProfilePITRDir(m.result.Profile.Name)
		if err != nil {
			m.pitr.planBody = "Cannot determine default PITR directories: " + err.Error()
		} else {
			retention := pitr.DefaultRetentionPolicy()
			m.pitr.planBody = renderKeyValueGrid([]kvRow{
				{Label: "Archive dir", Value: filepath.Join(profileDir, "wal")},
				{Label: "Base backups", Value: filepath.Join(profileDir, "base")},
				{Label: "Retention", Value: fmt.Sprintf("%d backups / %d WAL days", retention.KeepBaseBackups, retention.KeepWALDays)},
				{Label: "Safety", Value: "Review only — does not change PostgreSQL", Kind: badgeWarning},
			}, 12)
		}
	case "b", "B":
		m.pitr.dashboardOffset = 0
		m.pitr.planTitle = "Backup Plan"
		if !m.pitr.status.Configured {
			m.pitr.planBody = "PITR setup is required before a base backup can be created."
		} else {
			m.pitr.planBody = renderKeyValueGrid([]kvRow{
				{Label: "Profile", Value: m.result.Profile.Name},
				{Label: "Archive", Value: m.pitr.config.ArchiveDir},
				{Label: "Retention", Value: fmt.Sprintf("keep %d base backups", m.pitr.config.Retention.KeepBaseBackups)},
				{Label: "Safety", Value: "Plan only — no backup is started", Kind: badgeWarning},
			}, 10)
		}
	case "c", "C":
		m.pitr.dashboardOffset = 0
		m.pitr.planTitle = "Cleanup Plan"
		if !m.pitr.status.Configured {
			m.pitr.planBody = "PITR setup is required before cleanup can be planned."
		} else {
			deleteCount := m.pitr.status.BackupCount - m.pitr.config.Retention.KeepBaseBackups
			if deleteCount < 0 {
				deleteCount = 0
			}
			m.pitr.planBody = renderKeyValueGrid([]kvRow{
				{Label: "Base backups", Value: fmt.Sprintf("%d candidate(s) after retention", deleteCount)},
				{Label: "WAL policy", Value: fmt.Sprintf("older than %d day(s)", m.pitr.config.Retention.KeepWALDays)},
				{Label: "Safety", Value: "Plan only — nothing is deleted", Kind: badgeDanger},
			}, 12)
		}
	case "t", "T":
		m.pitr.target = textinput.New()
		m.pitr.target.Placeholder = "YYYY-MM-DD HH:MM:SS"
		m.pitr.target.CharLimit = 32
		m.pitr.target.Width = 32
		m.pitr.target.Focus()
		m.step = stepPITRRestoreTarget
	case "esc":
		if m.pitr.planTitle != "" {
			m.pitr.planTitle = ""
			m.pitr.planBody = ""
		} else {
			m.step = stepSelectProfile
		}
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updatePITRRestoreTarget(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.step = stepPITRDashboard
		return m, nil
	case "enter":
		target, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(m.pitr.target.Value()), time.Local)
		if err != nil {
			m.pitr.notice = "Invalid recovery time. Use YYYY-MM-DD HH:MM:SS."
			m.step = stepPITRDashboard
			return m, nil
		}
		m.pitr.plan = pitr.BuildValidatedRecoveryPlan(m.pitr.config, m.pitr.status.Backups, target)
		m.pitr.recoveryPlanOffset = 0
		m.step = stepPITRRecoveryPlan
		return m, nil
	}
	var cmd tea.Cmd
	m.pitr.target, cmd = m.pitr.target.Update(msg)
	return m, cmd
}

func (m Model) updatePITRRecoveryPlan(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.updatePITRRecoveryPlanScroll(msg) {
		return m, nil
	}
	switch msg.String() {
	case "esc", "enter":
		m.step = stepPITRDashboard
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewPITRDashboard() string {
	lines := m.pitrDashboardContentLines()
	return m.pitrDashboardViewport(lines).Render(lines)
}

func (m Model) pitrDashboardContentLines() []string {
	var body strings.Builder
	body.WriteString(renderProfileSummaryCard(m.width, "PITR Profile", m.result.Profile.Name, m.result.Profile.Driver, m.result.Profile.Host, m.result.Profile.Port, m.result.Profile.Database, m.result.Profile.User))
	body.WriteString("\n\n")
	if m.result.Profile.Driver != "postgres" {
		body.WriteString(renderCard(m.width, "PITR unavailable", errorStyle.Render("PITR is supported only for PostgreSQL profiles.")))
	} else if !m.pitr.status.Configured {
		body.WriteString(renderCard(m.width, "PITR Setup Required", warningStyle.Render("No local PITR configuration is available. Select Setup Plan to review safe defaults.")))
	} else {
		status := m.pitr.status
		body.WriteString(renderCard(m.width, "PITR Metadata", renderKeyValueGrid([]kvRow{
			{Label: "Configuration", Value: "CONFIGURED", Kind: badgePrimary},
			{Label: "Base backups", Value: fmt.Sprintf("%d (%s)", status.BackupCount, formatPITRBytes(status.TotalBackupSize))},
			{Label: "WAL archive", Value: fmt.Sprintf("%d (%s)", status.WALCount, formatPITRBytes(status.TotalWALSize))},
			{Label: "Recorded timestamps", Value: formatRecoveryRange(status.EarliestBackupTime, status.LatestWALModTime)},
		}, 14)))
	}
	if m.pitr.notice != "" {
		body.WriteString("\n\n")
		body.WriteString(renderCard(m.width, "Notice", warningStyle.Render(m.pitr.notice)))
	}
	if m.pitr.planTitle != "" {
		body.WriteString("\n\n")
		body.WriteString(renderCard(m.width, m.pitr.planTitle, m.pitr.planBody))
	}
	body.WriteString("\n\n")
	body.WriteString(renderCard(m.width, "Plan-First Actions", "Setup plan (S)\nBackup plan (B)\nRestore plan (T)\nCleanup plan (C)"))
	return strings.Split(body.String(), "\n")
}

func (m Model) viewPITRRestoreTarget() string {
	body := renderProfileSummaryCard(m.width, "PITR Source", m.result.Profile.Name, m.result.Profile.Driver, m.result.Profile.Host, m.result.Profile.Port, m.result.Profile.Database, m.result.Profile.User) + "\n\n" + renderCard(m.width, "Target Recovery Time", "Enter a local timestamp:\n\n"+m.pitr.target.View())
	return renderScreenFrame(m.width, "PITR Restore Plan", "This generates a plan only and does not stop PostgreSQL or alter data.", body, []keyHint{{Key: "Enter", Label: "build plan"}, {Key: "Esc", Label: "back", Danger: true}})
}

func (m Model) viewPITRRecoveryPlan() string {
	lines := m.pitrRecoveryPlanContentLines()
	return m.pitrRecoveryPlanViewport(lines).Render(lines)
}

func (m Model) pitrRecoveryPlanContentLines() []string {
	plan := m.pitr.plan
	var body strings.Builder
	if len(plan.Blockers) > 0 {
		body.WriteString(renderCard(m.width, "Recovery Blocked", errorStyle.Render(strings.Join(plan.Blockers, "\n"))))
	} else {
		backup := "none"
		if plan.SelectedBackup != nil {
			backup = plan.SelectedBackup.ID + " @ " + plan.SelectedBackup.StartTime.Format("2006-01-02 15:04:05")
		}
		body.WriteString(renderCard(m.width, "Recovery Preflight", renderKeyValueGrid([]kvRow{
			{Label: "Target time", Value: plan.TargetTime.Format("2006-01-02 15:04:05")},
			{Label: "Base backup", Value: backup},
			{Label: "Execution", Value: "PLAN ONLY", Kind: badgeWarning},
		}, 12)))
	}
	if len(plan.Warnings) > 0 {
		body.WriteString("\n\n" + renderCard(m.width, "Warnings", warningStyle.Render(strings.Join(plan.Warnings, "\n"))))
	}
	body.WriteString("\n\n" + mutedStyle.Render("This preflight validates local backup and WAL artifacts, but cannot prove timestamp-to-WAL mapping. No database service or data directory has been changed."))
	return strings.Split(body.String(), "\n")
}

func (m Model) pitrDashboardViewport(lines []string) fixedDocumentViewport {
	header := renderAppHeader("PITR Dashboard", "Review plans first; mutations remain explicit CLI operations", m.width)
	return newFixedDocumentViewport(lines, m.height, header, pitrDocumentFrameGaps, m.pitr.dashboardOffset, func(viewport scrollViewport) string {
		return renderCommandBar(m.width, m.pitrDashboardFooterHints(viewport))
	})
}

func (m Model) pitrRecoveryPlanViewport(lines []string) fixedDocumentViewport {
	header := renderAppHeader("PITR Recovery Preflight", "Plan only — execution remains unavailable in the TUI.", m.width)
	return newFixedDocumentViewport(lines, m.height, header, pitrDocumentFrameGaps, m.pitr.recoveryPlanOffset, func(viewport scrollViewport) string {
		return renderCommandBar(m.width, m.pitrRecoveryPlanFooterHints(viewport))
	})
}

func (m Model) pitrDashboardVisibleHeight() int {
	return m.pitrDashboardViewport(m.pitrDashboardContentLines()).Visible
}

func (m Model) pitrDashboardMaxOffset() int {
	return scrollMaxOffset(len(m.pitrDashboardContentLines()), m.pitrDashboardVisibleHeight())
}

func (m Model) pitrRecoveryPlanVisibleHeight() int {
	return m.pitrRecoveryPlanViewport(m.pitrRecoveryPlanContentLines()).Visible
}

func (m Model) pitrRecoveryPlanMaxOffset() int {
	return scrollMaxOffset(len(m.pitrRecoveryPlanContentLines()), m.pitrRecoveryPlanVisibleHeight())
}

func (m *Model) clampPITRDocumentOffsets() {
	m.pitr.dashboardOffset = clampScrollOffset(m.pitr.dashboardOffset, len(m.pitrDashboardContentLines()), m.pitrDashboardVisibleHeight())
	m.pitr.recoveryPlanOffset = clampScrollOffset(m.pitr.recoveryPlanOffset, len(m.pitrRecoveryPlanContentLines()), m.pitrRecoveryPlanVisibleHeight())
}

func (m Model) pitrDashboardFooterHints(viewport scrollViewport) []keyHint {
	hints := []keyHint{{Key: "R", Label: "refresh"}, {Key: "S", Label: "setup plan"}, {Key: "B", Label: "backup plan"}, {Key: "T", Label: "restore plan"}, {Key: "C", Label: "cleanup plan"}}
	return m.pitrDocumentFooterHints(hints, viewport, []keyHint{{Key: "Esc", Label: "profiles", Danger: true}, {Key: "Q", Label: "quit", Danger: true}})
}

func (m Model) pitrRecoveryPlanFooterHints(viewport scrollViewport) []keyHint {
	return m.pitrDocumentFooterHints([]keyHint{{Key: "Enter/Esc", Label: "dashboard"}}, viewport, []keyHint{{Key: "Q", Label: "quit", Danger: true}})
}

func (m Model) pitrDocumentFooterHints(hints []keyHint, viewport scrollViewport, trailing []keyHint) []keyHint {
	if viewport.Total > viewport.Visible {
		hints = append(hints, keyHint{Key: fmt.Sprintf("%d–%d / %d", viewport.Offset+1, viewport.End, viewport.Total), Label: "position"}, keyHint{Key: "↑/k", Label: "up"}, keyHint{Key: "↓/j", Label: "down"}, keyHint{Key: "PgUp/PgDn", Label: "page"}, keyHint{Key: "Home/End", Label: "bounds"})
	}
	return append(hints, trailing...)
}

func (m *Model) updatePITRDashboardScroll(msg tea.KeyMsg) bool {
	if m.pitrDashboardMaxOffset() == 0 {
		return false
	}
	offset, ok := updateDocumentScrollOffset(msg, m.pitr.dashboardOffset, m.pitrDashboardVisibleHeight(), m.pitrDashboardMaxOffset())
	if !ok {
		return false
	}
	m.pitr.dashboardOffset = offset
	return true
}

func (m *Model) updatePITRRecoveryPlanScroll(msg tea.KeyMsg) bool {
	if m.pitrRecoveryPlanMaxOffset() == 0 {
		return false
	}
	offset, ok := updateDocumentScrollOffset(msg, m.pitr.recoveryPlanOffset, m.pitrRecoveryPlanVisibleHeight(), m.pitrRecoveryPlanMaxOffset())
	if !ok {
		return false
	}
	m.pitr.recoveryPlanOffset = offset
	return true
}

func updateDocumentScrollOffset(msg tea.KeyMsg, offset, page, maxOffset int) (int, bool) {
	switch msg.String() {
	case "up", "k", "K":
		offset--
	case "down", "j":
		offset++
	case "pgup":
		offset -= page
	case "pgdown":
		offset += page
	case "home":
		offset = 0
	case "end":
		offset = maxOffset
	default:
		return 0, false
	}
	return min(max(0, offset), maxOffset), true
}

func formatRecoveryRange(start, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "unavailable"
	}
	return start.Format("2006-01-02 15:04") + " → " + end.Format("2006-01-02 15:04")
}

func formatPITRBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(size)/(1024*1024))
}
