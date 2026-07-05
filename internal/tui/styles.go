package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Color palette
	colorPrimary  = lipgloss.Color("#7C3AED") // violet-600
	colorAccent   = lipgloss.Color("#A78BFA") // violet-400
	colorSuccess  = lipgloss.Color("#34D399") // emerald-400
	colorWarning  = lipgloss.Color("#FBBF24") // amber-400
	colorMuted    = lipgloss.Color("#6B7280") // gray-500
	colorBg       = lipgloss.Color("#1F2937") // gray-800
	colorBgLight  = lipgloss.Color("#374151") // gray-700
	colorText     = lipgloss.Color("#F9FAFB") // gray-50
	colorSubtext  = lipgloss.Color("#D1D5DB") // gray-300

	// Title bar
	titleStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorPrimary).
			Bold(true).
			Padding(0, 2).
			MarginBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			MarginBottom(1)

	// List item styles
	selectedItemStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorPrimary).
				Bold(true).
				Padding(0, 1)

	normalItemStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			Padding(0, 1)

	dimItemStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	// Panel / container
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(0, 1)

	// Footer / hint bar
	hintStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginTop(1)

	hintKeyStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	// Status
	successStyle = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true)

	// Labels
	labelStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(12)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorSubtext)

	// Profile badge (driver type)
	driverBadgeStyle = lipgloss.NewStyle().
				Foreground(colorBg).
				Background(colorAccent).
				Bold(true).
				Padding(0, 1).
				MarginLeft(1)

	// File extension badge
	fileBadgeStyle = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorSuccess).
			Padding(0, 1).
			MarginLeft(1)

	dirBadgeStyle = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorWarning).
			Padding(0, 1).
			MarginLeft(1)
)
