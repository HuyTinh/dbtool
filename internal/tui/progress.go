package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func clampPercent(percent float64) float64 {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func progressWidth(termWidth int) int {
	width := termWidth - 10
	if width < 20 {
		return 20
	}
	if width > 60 {
		return 60
	}
	return width
}

func renderProgressBar(width int, percent float64) string {
	if width < 1 {
		width = 1
	}
	percent = clampPercent(percent)
	filledW := int(float64(width) * (percent / 100.0))
	if filledW < 0 {
		filledW = 0
	}
	if filledW > width {
		filledW = width
	}
	emptyW := width - filledW

	filledStr := lipgloss.NewStyle().Foreground(colorSuccess).Render(strings.Repeat("█", filledW))
	emptyStr := lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Repeat("░", emptyW))
	return filledStr + emptyStr
}
