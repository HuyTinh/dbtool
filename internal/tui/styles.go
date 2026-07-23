package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Color palette
	colorPrimary = lipgloss.Color("#7C3AED") // violet-600
	colorAccent  = lipgloss.Color("#A78BFA") // violet-400
	colorSuccess = lipgloss.Color("#34D399") // emerald-400
	colorWarning = lipgloss.Color("#FBBF24") // amber-400
	colorMuted   = lipgloss.Color("#6B7280") // gray-500
	colorBg      = lipgloss.Color("#1F2937") // gray-800
	colorBgLight = lipgloss.Color("#374151") // gray-700
	colorText    = lipgloss.Color("#F9FAFB") // gray-50
	colorSubtext = lipgloss.Color("#D1D5DB") // gray-300

	// Panel / container
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	warningStyle = lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444")).
			Bold(true)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	subtextStyle = lipgloss.NewStyle().
			Foreground(colorSubtext)

	dangerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FCA5A5")).
			Background(lipgloss.Color("#7F1D1D")).
			Bold(true).
			Padding(0, 1)

	// Labels
	labelStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(12)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorSubtext)
)

func renderDanger(message string) string {
	return dangerStyle.Render("! " + message)
}
