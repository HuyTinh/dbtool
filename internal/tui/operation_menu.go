package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type operationMenuItem struct {
	key         string
	group       string
	title       string
	tag         string
	description string
	mode        Mode
}

var operationMenuItems = []operationMenuItem{
	{key: "R", group: "DATA MOVEMENT", title: "Restore", tag: "IMPORT", description: "Import a dump into a database", mode: ModeRestore},
	{key: "D", group: "DATA MOVEMENT", title: "Dump", tag: "BACKUP", description: "Export a database to a dump file", mode: ModeDump},
	{key: "M", group: "DATA MOVEMENT", title: "Migrate", tag: "SOURCE → TARGET", description: "Copy a database between profiles", mode: ModeMigrate},
	{key: "S", group: "DATABASE OPERATIONS", title: "Schema", tag: "REVIEW → APPLY", description: "Edit nullable, default, and unique settings", mode: ModeSchemaAttributes},
	{key: "H", group: "DATABASE OPERATIONS", title: "Health", tag: "READ ONLY", description: "Review status, sessions, and blockers", mode: ModeHealth},
	{key: "I", group: "RECOVERY", title: "PITR", tag: "PLAN ONLY", description: "Review recovery readiness and plans", mode: ModePITR},
}

func (m Model) selectedOperation() operationMenuItem {
	return operationMenuItems[clampIndex(m.operationIdx, len(operationMenuItems))]
}

func (m Model) renderOperationMenuBody() string {
	var body strings.Builder
	if m.hasActiveProfile() {
		p := m.result.Profile
		profile := fmt.Sprintf("Profile: %s  %s  %s", p.Name, strings.ToUpper(p.Driver), p.Database)
		body.WriteString(renderStatusBadge("ACTIVE", badgeSuccess) + " " + subtextStyle.Render(truncateMiddle(profile, safePanelWidth(m.width)-12)))
	} else {
		body.WriteString(renderStatusBadge("NO PROFILE", badgeWarning) + " " + mutedStyle.Render("Choose an operation; select a profile when needed."))
	}
	body.WriteString("\n\n")

	lastGroup := ""
	for i, item := range operationMenuItems {
		if item.group != lastGroup {
			if lastGroup != "" {
				body.WriteString("\n")
			}
			body.WriteString(renderSectionTitle(item.group))
			body.WriteString("\n")
			lastGroup = item.group
		}
		body.WriteString(renderOperationMenuRow(m.width, item, i == clampIndex(m.operationIdx, len(operationMenuItems))))
		body.WriteString("\n")
	}
	return strings.TrimRight(body.String(), "\n")
}

func renderOperationMenuRow(width int, item operationMenuItem, selected bool) string {
	marker := "  "
	if selected {
		marker = "> "
	}
	key := renderStatusBadge("["+item.key+"]", badgePrimary)
	title := lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(item.title)
	tag := renderStatusBadge(item.tag, badgeNeutral)
	fixedWidth := lipgloss.Width(marker + key + " " + item.title + "  " + item.tag + "  ")
	descriptionWidth := safePanelWidth(width) - fixedWidth
	if descriptionWidth < 16 {
		descriptionWidth = 16
	}
	line := marker + key + " " + title + "  " + tag + "  " + subtextStyle.Render(truncateMiddle(item.description, descriptionWidth))
	if selected {
		return lipgloss.NewStyle().Background(colorBgLight).Width(safePanelWidth(width)).Render(line)
	}
	return line
}

func (m Model) operationMenuFooterHints() []keyHint {
	hints := []keyHint{
		{Key: "↑↓", Label: "move"},
		{Key: "Enter", Label: "open"},
		{Key: "P", Label: "profile"},
		{Key: "F", Label: "flows"},
		{Key: "Q", Label: "quit", Danger: true},
	}
	if safePanelWidth(m.width) < 70 {
		return []keyHint{hints[0], hints[1], hints[2], hints[4]}
	}
	return hints
}

func (m Model) startSelectedOperation() (tea.Model, tea.Cmd) {
	m.result.Mode = m.selectedOperation().mode
	if !m.hasActiveProfile() {
		m.step = stepSelectProfile
		return m, nil
	}
	switch m.result.Mode {
	case ModeRestore:
		m.step = stepSelectFile
	case ModeDump:
		m.dumpOutputInput.SetValue("")
		m.dumpOutputInput.Focus()
		m.step = stepDumpOutputPath
	case ModeMigrate:
		m.step = stepMigrateSelectDest
	case ModePITR:
		m.refreshPITRDashboard()
		m.step = stepPITRDashboard
	case ModeHealth:
		m.step = stepHealthDashboard
		return m, m.startHealthRefresh()
	case ModeSchemaAttributes:
		m.step = stepSchemaAttributeForm
		return m, m.startSchemaAttributeForm()
	}
	return m, nil
}

func operationIndexForKey(key string) (int, bool) {
	switch key {
	case "1":
		return 0, true
	case "2":
		return 1, true
	case "3":
		return 2, true
	case "4":
		return 5, true
	case "5":
		return 4, true
	case "6":
		return 3, true
	}
	for i, item := range operationMenuItems {
		if strings.EqualFold(item.key, key) {
			return i, true
		}
	}
	return 0, false
}
