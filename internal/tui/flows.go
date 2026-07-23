package tui

import (
	"fmt"
	"strings"

	"dbtool/internal/flows"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const flowsFrameGaps = 2

func (m *Model) loadFlows() error {
	store, err := flows.Open()
	if err != nil {
		return err
	}
	data, err := store.Load()
	if err != nil {
		return err
	}
	m.flowRecent = data.Recent
	m.flowPinned = data.Pinned
	m.flowIdx = 0
	m.flowViewportOffset = 0
	m.flowPinnedTab = len(data.Pinned) > 0
	m.flowErr = ""
	return nil
}

func (m Model) activeFlows() []flows.Flow {
	if m.flowPinnedTab {
		return m.flowPinned
	}
	return m.flowRecent
}

func (m Model) flowTabName() string {
	if m.flowPinnedTab {
		return "Pinned"
	}
	return "Recent"
}

func (m Model) flowHeader() string {
	return renderAppHeader("Reusable Flows", m.flowTabName()+" flows — selecting one only opens its review; it never executes", m.width)
}

func (m *Model) applyFlow(flow flows.Flow) error {
	profile, ok := m.cfg.GetProfile(flow.Profile)
	if !ok {
		return fmt.Errorf("profile %q no longer exists", flow.Profile)
	}
	m.result.Profile = profile
	s := flow.Settings
	switch flow.Operation {
	case flows.OperationDump:
		m.result.Mode = ModeDump
		m.result.DumpFile = flow.FilePath
		m.dumpOutputInput.SetValue(flow.FilePath)
		m.result.DumpSettings = DumpSettings{Format: s.Format, IncludeTable: s.IncludeTables, ExcludeTable: s.ExcludeTables, IncludeSchema: s.IncludeSchemas, ExcludeSchema: s.ExcludeSchemas}
		m.step = stepDumpConfirm
	case flows.OperationRestore:
		m.result.Mode = ModeRestore
		m.result.File = flow.FilePath
		m.result.Settings = RestoreSettings{Format: s.Format, Jobs: s.Jobs, Clean: s.Clean, CreateIfMissing: s.CreateIfMissing, Optimize: s.Optimize, IncludeTable: s.IncludeTables, ExcludeTable: s.ExcludeTables, IncludeSchema: s.IncludeSchemas, ExcludeSchema: s.ExcludeSchemas}
		m.step = stepConfirm
	case flows.OperationMigrate:
		destination, ok := m.cfg.GetProfile(flow.DestinationProfile)
		if !ok {
			return fmt.Errorf("destination profile %q no longer exists", flow.DestinationProfile)
		}
		m.result.Mode = ModeMigrate
		m.result.DestProfile = destination
		m.result.MigrateSettings = MigrateSettings{Format: s.Format, Jobs: s.Jobs, Clean: s.Clean, CreateIfMissing: s.CreateIfMissing, Optimize: s.Optimize, SchemaOnly: s.SchemaOnly, DataOnly: s.DataOnly, IncludeTable: s.IncludeTables, ExcludeTable: s.ExcludeTables, IncludeSchema: s.IncludeSchemas, ExcludeSchema: s.ExcludeSchemas}
		// The preflight is read-only and must complete before the normal migration review.
		m.step = stepMigratePreflight
	default:
		return fmt.Errorf("unsupported flow operation %q", flow.Operation)
	}
	return nil
}

func (m Model) currentFlow() (flows.Flow, bool) {
	switch m.result.Mode {
	case ModeDump:
		if m.result.Profile.Name == "" || m.result.DumpFile == "" {
			return flows.Flow{}, false
		}
		s := m.result.DumpSettings
		return flows.Flow{Operation: flows.OperationDump, Profile: m.result.Profile.Name, FilePath: m.result.DumpFile, Settings: flows.Settings{Format: s.Format, IncludeTables: s.IncludeTable, ExcludeTables: s.ExcludeTable, IncludeSchemas: s.IncludeSchema, ExcludeSchemas: s.ExcludeSchema}}, true
	case ModeRestore:
		if m.result.Profile.Name == "" || m.result.File == "" {
			return flows.Flow{}, false
		}
		s := m.result.Settings
		return flows.Flow{Operation: flows.OperationRestore, Profile: m.result.Profile.Name, FilePath: m.result.File, Settings: flows.Settings{Format: s.Format, Jobs: s.Jobs, Clean: s.Clean, CreateIfMissing: s.CreateIfMissing, Optimize: s.Optimize, IncludeTables: s.IncludeTable, ExcludeTables: s.ExcludeTable, IncludeSchemas: s.IncludeSchema, ExcludeSchemas: s.ExcludeSchema}}, true
	case ModeMigrate:
		if m.result.Profile.Name == "" || m.result.DestProfile.Name == "" {
			return flows.Flow{}, false
		}
		s := m.result.MigrateSettings
		return flows.Flow{Operation: flows.OperationMigrate, Profile: m.result.Profile.Name, DestinationProfile: m.result.DestProfile.Name, Settings: flows.Settings{Format: s.Format, Jobs: s.Jobs, Clean: s.Clean, CreateIfMissing: s.CreateIfMissing, Optimize: s.Optimize, SchemaOnly: s.SchemaOnly, DataOnly: s.DataOnly, IncludeTables: s.IncludeTable, ExcludeTables: s.ExcludeTable, IncludeSchemas: s.IncludeSchema, ExcludeSchemas: s.ExcludeSchema}}, true
	}
	return flows.Flow{}, false
}

func (m *Model) recordCurrentFlow() {
	flow, ok := m.currentFlow()
	if !ok {
		return
	}
	if store, err := flows.Open(); err == nil {
		_ = store.Record(flow)
	}
}

func (m Model) updateFlows(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	entries := m.activeFlows()
	page := m.flowVisibleHeight()
	switch msg.String() {
	case "up", "k", "K":
		m.flowIdx--
	case "down", "j", "J":
		m.flowIdx++
	case "pgup":
		m.flowIdx -= page
	case "pgdown":
		m.flowIdx += page
	case "home":
		m.flowIdx = 0
	case "end":
		m.flowIdx = len(entries) - 1
	case "tab":
		m.flowPinnedTab = !m.flowPinnedTab
		m.flowIdx = 0
		m.flowViewportOffset = 0
	case "p", "P":
		if len(entries) == 0 {
			return m, nil
		}
		flow := entries[m.flowIdx]
		store, err := flows.Open()
		if err != nil {
			m.flowErr = err.Error()
			return m, nil
		}
		if err := store.SetPinned(flow, !m.flowPinnedTab); err != nil {
			m.flowErr = err.Error()
			return m, nil
		}
		_ = m.loadFlows()
	case "enter", " ":
		if len(entries) == 0 {
			return m, nil
		}
		if err := m.applyFlow(entries[m.flowIdx]); err != nil {
			m.flowErr = err.Error()
			return m, nil
		}
		if m.step == stepMigratePreflight {
			return m, m.startMigratePreflight()
		}
	case "esc", "q", "ctrl+c":
		m.step = stepSelectMode
	}
	m.clampFlowsViewport()
	return m, nil
}

func (m Model) flowVisibleHeight() int {
	total := len(m.activeFlows())
	header := m.flowHeader()
	footer := renderCommandBar(m.width, m.flowFooterHints(total, total, 0))
	visible := max(1, m.height-lipgloss.Height(header)-flowsFrameGaps-lipgloss.Height(footer))
	if total <= visible {
		return visible
	}
	footer = renderCommandBar(m.width, m.flowFooterHints(total, visible, 0))
	return max(1, m.height-lipgloss.Height(header)-flowsFrameGaps-lipgloss.Height(footer))
}

func (m Model) flowMaxOffset() int {
	return scrollMaxOffset(len(m.activeFlows()), m.flowVisibleHeight())
}

func (m *Model) clampFlowsViewport() {
	entries := m.activeFlows()
	if len(entries) == 0 {
		m.flowIdx = 0
		m.flowViewportOffset = 0
		return
	}
	m.flowIdx = min(max(0, m.flowIdx), len(entries)-1)
	m.flowViewportOffset = keepScrollIndexVisible(m.flowIdx, len(entries), m.flowVisibleHeight(), m.flowViewportOffset)
}

func (m Model) flowFooterHints(total, visibleHeight, offset int) []keyHint {
	pinLabel := "pin"
	if m.flowPinnedTab {
		pinLabel = "unpin"
	}
	hints := []keyHint{{Key: "Tab", Label: "recent/pinned"}, {Key: "Enter", Label: "review"}, {Key: "P", Label: pinLabel}}
	if total > visibleHeight {
		viewport := newScrollViewport(total, visibleHeight, offset)
		hints = append(hints,
			keyHint{Key: fmt.Sprintf("%d–%d / %d", viewport.Offset+1, viewport.End, total), Label: "position"},
			keyHint{Key: "↑/k", Label: "up"}, keyHint{Key: "↓/j", Label: "down"},
			keyHint{Key: "PgUp/PgDn", Label: "page"}, keyHint{Key: "Home/End", Label: "bounds"})
	}
	return append(hints, keyHint{Key: "Esc", Label: "back", Danger: true})
}

func (m Model) viewFlows() string {
	entries := m.activeFlows()
	visibleHeight := m.flowVisibleHeight()
	viewport := newScrollViewport(len(entries), visibleHeight, m.flowViewportOffset)
	header := m.flowHeader()
	footer := renderCommandBar(m.width, m.flowFooterHints(len(entries), visibleHeight, viewport.Offset))

	var body string
	if len(entries) == 0 {
		body = renderCard(m.width, "No "+m.flowTabName()+" Flows", "Confirmed dump, restore, and migrate reviews appear here. Flows contain only profile names and safe options.")
	} else {
		rows := make([]string, 0, viewport.End-viewport.Offset)
		for i := viewport.Offset; i < viewport.End; i++ {
			rows = append(rows, m.renderFlowRow(entries[i], i == m.flowIdx))
		}
		body = strings.Join(rows, "\n")
	}
	if m.flowErr != "" {
		body += "\n" + errorStyle.Render(truncateMiddle(m.flowErr, safePanelWidth(m.width)))
	}
	return header + "\n\n" + body + "\n\n" + footer
}

func (m Model) renderFlowRow(flow flows.Flow, selected bool) string {
	marker := "  "
	if selected {
		marker = "> "
	}
	target := flow.Profile
	if flow.DestinationProfile != "" {
		target += " → " + flow.DestinationProfile
	}
	if flow.FilePath != "" {
		target += "  " + flow.FilePath
	}
	return marker + strings.ToUpper(string(flow.Operation)) + "  " + truncateMiddle(target, max(1, safePanelWidth(m.width)-12))
}
