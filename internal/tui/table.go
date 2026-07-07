package tui

import (
	"fmt"
	"strings"

	"dbtool/internal/config"

	"github.com/charmbracelet/lipgloss"
)

func maxProfileNameLen(profiles []config.Profile) int {
	maxNameLen := 0
	for _, p := range profiles {
		if len(p.Name) > maxNameLen {
			maxNameLen = len(p.Name)
		}
	}
	if maxNameLen < 12 {
		return 12
	}
	return maxNameLen
}

func renderProfileRow(p config.Profile, selected bool, nameWidth int, suffix string) string {
	badge := renderBadge(strings.ToUpper(p.Driver), "#A78BFA")
	connStr := fmt.Sprintf("%s:%d/%s", p.Host, p.Port, p.Database)
	namePart := fmt.Sprintf("%-*s", nameWidth, p.Name)
	if suffix != "" {
		namePart += lipgloss.NewStyle().Foreground(colorMuted).Render(" " + suffix)
	}
	row := renderRow(selected, namePart)
	connPart := lipgloss.NewStyle().Foreground(colorMuted).Render(" " + connStr)
	return "  " + row + " " + badge + connPart
}

func renderProfileRows(profiles []config.Profile, selectedIdx int, sourceName string) string {
	var sb strings.Builder
	nameWidth := maxProfileNameLen(profiles) + 2
	for i, p := range profiles {
		suffix := ""
		if sourceName != "" && p.Name == sourceName {
			suffix = "(source)"
		}
		sb.WriteString(renderProfileRow(p, i == selectedIdx, nameWidth, suffix))
		sb.WriteString("\n")
	}
	return sb.String()
}
