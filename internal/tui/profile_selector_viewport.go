package tui

import (
	"fmt"
	"strings"
)

type profileSelectorDocument struct {
	viewport scrollViewport
	lines    []string
	starts   []int
	render   func() string
}

func (m Model) profileSelectorDocument() profileSelectorDocument {
	lines := []string{renderSectionTitle(profileSelectorPrompt(m.result.Mode)), ""}
	starts := make([]int, 0, len(m.profiles))
	for i, profile := range m.profiles {
		if i > 0 {
			lines = append(lines, "")
		}
		cardLines := strings.Split(renderProfileOptionCard(m.width, profile, i == m.profileIdx, false), "\n")
		starts = append(starts, len(lines)+profileCardSelectionLine(cardLines, profile.Name))
		lines = append(lines, cardLines...)
	}

	return m.newProfileSelectorDocument(
		"Select Connection Profile",
		profileSelectorSubtitle(m.result.Mode),
		lines,
		starts,
		m.profileViewportOffset,
		[]keyHint{
			{Key: "Enter", Label: "select"},
			{Key: "A", Label: "add"},
			{Key: "E", Label: "edit"},
			{Key: "D", Label: "delete"},
			{Key: "Esc", Label: "back", Danger: true},
			{Key: "Q", Label: "quit", Danger: true},
		},
	)
}

func (m Model) migrateDestSelectorDocument() profileSelectorDocument {
	source := m.result.Profile
	lines := strings.Split(renderProfileSummaryCard(m.width, "Source Profile", source.Name, source.Driver, source.Host, source.Port, source.Database, source.User), "\n")
	lines = append(lines, "", renderSectionTitle("Choose a destination profile"), "")
	starts := make([]int, 0, len(m.profiles))
	for i, profile := range m.profiles {
		if i > 0 {
			lines = append(lines, "")
		}
		cardLines := strings.Split(renderProfileOptionCard(m.width, profile, i == m.migrateDestIdx, profile.Name == source.Name), "\n")
		starts = append(starts, len(lines)+profileCardSelectionLine(cardLines, profile.Name))
		lines = append(lines, cardLines...)
	}

	return m.newProfileSelectorDocument(
		"Select Destination Profile",
		"Target database for profile-to-profile copy",
		lines,
		starts,
		m.migrateDestViewportOffset,
		[]keyHint{
			{Key: "Enter", Label: "select"},
			{Key: "Esc", Label: "back", Danger: true},
			{Key: "Q", Label: "quit", Danger: true},
		},
	)
}

func profileCardSelectionLine(lines []string, name string) int {
	for i, line := range lines {
		if strings.Contains(line, name) {
			return i
		}
	}
	return 0
}

func (m Model) newProfileSelectorDocument(title, subtitle string, lines []string, starts []int, offset int, hints []keyHint) profileSelectorDocument {
	header := renderAppHeader(title, subtitle, m.width)
	fixed := newFixedDocumentViewport(lines, m.height, header, 2, offset, func(viewport scrollViewport) string {
		return renderSelectorFooter(m.width, hints, viewport)
	})
	return profileSelectorDocument{
		viewport: fixed.scrollViewport,
		lines:    lines,
		starts:   starts,
		render: func() string {
			return fixed.Render(lines)
		},
	}
}

func renderSelectorFooter(width int, hints []keyHint, viewport scrollViewport) string {
	if viewport.Total <= viewport.Visible {
		return renderCommandBar(width, hints)
	}
	position := mutedStyle.Render(fmt.Sprintf("Showing %d–%d of %d lines", viewport.Offset+1, viewport.End, viewport.Total))
	scrollHints := append(append([]keyHint{}, hints...),
		keyHint{Key: "↑/↓", Label: "navigate"},
		keyHint{Key: "PgUp/PgDn", Label: "page"},
		keyHint{Key: "Home/End", Label: "bounds"},
	)
	return position + "\n" + renderCommandBar(width, scrollHints)
}

func (m Model) profileSelectorPageIndex(direction int) int {
	document := m.profileSelectorDocument()
	return selectorPageIndex(m.profileIdx, document.starts, document.viewport.Visible, direction)
}

func (m Model) migrateDestSelectorPageIndex(direction int) int {
	document := m.migrateDestSelectorDocument()
	return selectorPageIndex(m.migrateDestIdx, document.starts, document.viewport.Visible, direction)
}

func selectorPageIndex(index int, starts []int, visible, direction int) int {
	if len(starts) == 0 {
		return 0
	}
	index = min(max(0, index), len(starts)-1)
	if direction < 0 {
		target := starts[index] - max(1, visible)
		for i := index - 1; i >= 0; i-- {
			if starts[i] <= target {
				return i
			}
		}
		return 0
	}
	target := starts[index] + max(1, visible)
	for i := min(index+1, len(starts)-1); i < len(starts); i++ {
		if starts[i] > target {
			return max(index+1, i-1)
		}
	}
	return len(starts) - 1
}

func (m *Model) syncProfileSelectorViewport() {
	if len(m.profiles) == 0 {
		m.profileViewportOffset = 0
		return
	}
	document := m.profileSelectorDocument()
	m.profileIdx = min(max(0, m.profileIdx), len(m.profiles)-1)
	m.profileViewportOffset = keepScrollIndexVisible(document.starts[m.profileIdx], len(document.lines), document.viewport.Visible, document.viewport.Offset)
}

func (m *Model) syncMigrateDestSelectorViewport() {
	if len(m.profiles) == 0 {
		m.migrateDestViewportOffset = 0
		return
	}
	document := m.migrateDestSelectorDocument()
	m.migrateDestIdx = min(max(0, m.migrateDestIdx), len(m.profiles)-1)
	m.migrateDestViewportOffset = keepScrollIndexVisible(document.starts[m.migrateDestIdx], len(document.lines), document.viewport.Visible, document.viewport.Offset)
}
