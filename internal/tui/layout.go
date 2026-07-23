package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type keyHint struct {
	Key    string
	Label  string
	Danger bool
}

type kvRow struct {
	Label string
	Value string
	Kind  badgeKind
}

type badgeKind int

const (
	badgeNeutral badgeKind = iota
	badgePrimary
	badgeSuccess
	badgeWarning
	badgeDanger
)

func renderScreenFrame(width int, title, subtitle, body string, hints []keyHint) string {
	var sb strings.Builder
	sb.WriteString(renderAppHeader(title, subtitle, width))
	sb.WriteString("\n\n")
	sb.WriteString(body)
	if len(hints) > 0 {
		if !strings.HasSuffix(body, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString(renderCommandBar(width, hints))
	}
	return sb.String()
}

func renderAppHeader(title, subtitle string, width int) string {
	innerWidth := safePanelWidth(width)
	brand := lipgloss.NewStyle().Foreground(colorText).Background(colorPrimary).Bold(true).Padding(0, 1).Render("dbtool")
	titleText := lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(" " + title)
	line := brand + titleText
	if subtitle == "" {
		return line
	}
	sub := lipgloss.NewStyle().Foreground(colorMuted).Render(truncateMiddle(subtitle, innerWidth))
	return line + "\n" + sub
}

func renderCommandBar(width int, items []keyHint) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		keyStyle := lipgloss.NewStyle().Foreground(colorText).Background(colorBgLight).Bold(true).Padding(0, 1)
		labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
		if item.Danger {
			keyStyle = keyStyle.Background(lipgloss.Color("#7F1D1D")).Foreground(lipgloss.Color("#FCA5A5"))
			labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FCA5A5"))
		}
		parts = append(parts, keyStyle.Render(item.Key)+" "+labelStyle.Render(item.Label))
	}
	bar := strings.Join(parts, "   ")
	return footerStyle.Width(safePanelWidth(width)).Render(bar)
}

func renderCard(width int, title string, body string) string {
	cardTitle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
	content := cardTitle
	if body != "" {
		content += "\n" + body
	}
	return panelStyle.Width(safePanelWidth(width)).Render(content)
}

func renderSectionTitle(title string) string {
	return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
}

func renderKeyValueGrid(rows []kvRow, labelWidth int) string {
	var sb strings.Builder
	if labelWidth < 8 {
		labelWidth = 8
	}
	for _, row := range rows {
		if w := lipgloss.Width(row.Label + ":"); w > labelWidth {
			labelWidth = w
		}
	}
	for _, row := range rows {
		label := lipgloss.NewStyle().Foreground(colorMuted).Width(labelWidth).Render(row.Label + ":")
		value := valueStyle.Render(row.Value)
		if row.Kind != badgeNeutral && row.Value != "" {
			value = renderStatusBadge(row.Value, row.Kind)
		}
		sb.WriteString(label + " " + value + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func renderStatusBadge(label string, kind badgeKind) string {
	bg := colorBgLight
	fg := colorText
	switch kind {
	case badgePrimary:
		bg = colorPrimary
	case badgeSuccess:
		bg = colorSuccess
		fg = colorBg
	case badgeWarning:
		bg = colorWarning
		fg = colorBg
	case badgeDanger:
		bg = lipgloss.Color("#7F1D1D")
		fg = lipgloss.Color("#FCA5A5")
	}
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Bold(true).Padding(0, 1).Render(label)
}

func renderOperationCard(width int, key, title, tag, description string) string {
	badge := renderStatusBadge("["+key+"]", badgePrimary)
	head := fmt.Sprintf("%s %s  %s", badge, lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(title), renderStatusBadge(tag, badgeNeutral))
	body := head + "\n" + lipgloss.NewStyle().Foreground(colorSubtext).Render(description)
	return renderCard(width, "", body)
}

func renderProfileSummaryCard(width int, title string, pName, driver, host string, port int, database, user string) string {
	rows := []kvRow{
		{Label: "Profile", Value: pName},
		{Label: "Driver", Value: strings.ToUpper(driver), Kind: badgePrimary},
		{Label: "Host", Value: fmt.Sprintf("%s:%d", host, port)},
		{Label: "Database", Value: database},
	}
	if user != "" {
		rows = append(rows, kvRow{Label: "User", Value: user})
	}
	return renderCard(width, title, renderKeyValueGrid(rows, 10))
}

func renderFilterRows(includeSchema, excludeSchema, includeTable, excludeTable []string) string {
	rows := []kvRow{}
	if len(includeSchema) > 0 {
		rows = append(rows, kvRow{Label: "Inc Schema", Value: strings.Join(includeSchema, ", ")})
	}
	if len(excludeSchema) > 0 {
		rows = append(rows, kvRow{Label: "Exc Schema", Value: strings.Join(excludeSchema, ", ")})
	}
	if len(includeTable) > 0 {
		rows = append(rows, kvRow{Label: "Inc Table", Value: strings.Join(includeTable, ", ")})
	}
	if len(excludeTable) > 0 {
		rows = append(rows, kvRow{Label: "Exc Table", Value: strings.Join(excludeTable, ", ")})
	}
	if len(rows) == 0 {
		return mutedStyle.Render("No schema/table filters")
	}
	return renderKeyValueGrid(rows, 12)
}

func safePanelWidth(width int) int {
	panelWidth := width - 4
	if panelWidth < 20 {
		return 20
	}
	return panelWidth
}

func truncateMiddle(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	remaining := max - 3
	front := remaining / 2
	back := remaining - front
	return string(runes[:front]) + "..." + string(runes[len(runes)-back:])
}

func clampIndex(idx, length int) int {
	if length <= 0 {
		return 0
	}
	if idx < 0 {
		return 0
	}
	if idx >= length {
		return length - 1
	}
	return idx
}
