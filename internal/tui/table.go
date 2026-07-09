package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"dbtool/internal/config"

	"github.com/charmbracelet/lipgloss"
)

func renderProfileRows(width int, profiles []config.Profile, selectedIdx int, sourceName string) string {
	var sb strings.Builder
	for i, p := range profiles {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(renderProfileOptionCard(width, p, i == selectedIdx, sourceName != "" && p.Name == sourceName))
	}
	return sb.String()
}

func renderProfileOptionCard(width int, p config.Profile, selected, source bool) string {
	marker := " "
	if selected {
		marker = "▶"
	}
	parts := []string{
		renderStatusBadge(marker, badgePrimary),
		lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(p.Name),
		renderStatusBadge(strings.ToUpper(p.Driver), badgeNeutral),
	}
	if source {
		parts = append(parts, renderStatusBadge("SOURCE", badgeWarning))
	}
	if runtimeLabel, runtimeKind := profileRuntimeBadge(p.Runtime); runtimeLabel != "" {
		parts = append(parts, renderStatusBadge(runtimeLabel, runtimeKind))
	}
	if sourceLabel, sourceKind := profileRuntimeSourceBadge(p.Runtime); sourceLabel != "" {
		parts = append(parts, renderStatusBadge(sourceLabel, sourceKind))
	}

	conn := fmt.Sprintf("%s:%d/%s", p.Host, p.Port, p.Database)
	meta := conn
	if runtimeMeta := profileRuntimeMeta(p.Runtime); runtimeMeta != "" {
		meta += " • " + runtimeMeta
	}
	body := strings.Join(parts, " ") + "\n" + lipgloss.NewStyle().Foreground(colorSubtext).Render(meta)
	return renderCard(width, "", body)
}

func profileRuntimeBadge(runtimeProfile *config.RuntimeProfile) (string, badgeKind) {
	if runtimeProfile == nil {
		return "", badgeNeutral
	}
	return "DOCKER", badgeSuccess
}

func profileRuntimeSourceBadge(runtimeProfile *config.RuntimeProfile) (string, badgeKind) {
	if runtimeProfile == nil {
		return "", badgeNeutral
	}
	switch runtimeProfile.Source {
	case "docker-cli":
		return "LIVE", badgeSuccess
	case "docker-compose":
		return "COMPOSE", badgeWarning
	default:
		return strings.ToUpper(runtimeProfile.Source), badgePrimary
	}
}

func profileRuntimeMeta(runtimeProfile *config.RuntimeProfile) string {
	if runtimeProfile == nil {
		return ""
	}
	if runtimeProfile.ServiceName != "" {
		return runtimeProfile.ServiceName
	}
	if runtimeProfile.Container != "" {
		return runtimeProfile.Container
	}
	if runtimeProfile.SourceFile != "" {
		return filepath.Base(runtimeProfile.SourceFile)
	}
	return "docker runtime"
}

func renderFileEntryRow(width int, entry fileEntry, selected bool) string {
	kind := "FILE"
	kindBadge := badgeSuccess
	meta := formatBytes(entry.Size)
	if entry.IsDir {
		kind = "DIR"
		kindBadge = badgeWarning
		meta = "folder"
	}
	if entry.Name == ".." {
		kind = "PARENT"
		kindBadge = badgePrimary
		meta = "go up"
	}

	panelWidth := safePanelWidth(width)
	nameWidth := panelWidth - 26
	if nameWidth < 12 {
		nameWidth = 12
	}
	name := truncateMiddle(entry.Name, nameWidth)
	row := renderRow(selected, fmt.Sprintf("%-*s", nameWidth, name))
	metaText := lipgloss.NewStyle().Foreground(colorMuted).Render(meta)
	return row + " " + renderStatusBadge(kind, kindBadge) + " " + metaText
}
