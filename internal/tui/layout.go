package tui

import "strings"

type screenSection struct {
	Title string
	Body  string
}

func renderScreen(title string, body string, footer string) string {
	var sb strings.Builder
	sb.WriteString(renderTitle(title))
	sb.WriteString("\n\n")
	sb.WriteString(body)
	if footer != "" {
		if !strings.HasSuffix(body, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString(renderFooter(footer))
	}
	return sb.String()
}

func renderPanel(width int, body string) string {
	return panelStyle.Width(safePanelWidth(width)).Render(body)
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
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	remaining := max - 3
	front := remaining / 2
	back := remaining - front
	return s[:front] + "..." + s[len(s)-back:]
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
