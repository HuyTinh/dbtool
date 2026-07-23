package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// fixedDocumentViewport keeps a document body within a fixed header and footer.
// The footer is rendered from the final viewport so overflow-specific controls
// remain accurate while the frame height stays bounded.
type fixedDocumentViewport struct {
	scrollViewport
	Header        string
	Footer        string
	frameGapLines int
}

func newFixedDocumentViewport(lines []string, height int, header string, frameGapLines, offset int, renderFooter func(scrollViewport) string) fixedDocumentViewport {
	total := len(lines)
	// Reserve space for an overflow footer first. A second pass then allows a
	// compact footer to return its space when the full document fits.
	footer := renderFooter(newScrollViewport(total, max(1, total-1), offset))
	visible := fixedDocumentVisibleHeight(height, header, footer, frameGapLines)
	viewport := newScrollViewport(total, visible, offset)

	footer = renderFooter(viewport)
	visible = fixedDocumentVisibleHeight(height, header, footer, frameGapLines)
	viewport = newScrollViewport(total, visible, offset)
	footer = renderFooter(viewport)

	return fixedDocumentViewport{
		scrollViewport: viewport,
		Header:         header,
		Footer:         footer,
		frameGapLines:  frameGapLines,
	}
}

func fixedDocumentVisibleHeight(height int, header, footer string, frameGapLines int) int {
	return max(1, height-lipgloss.Height(header)-frameGapLines-lipgloss.Height(footer))
}

func (v fixedDocumentViewport) Render(lines []string) string {
	separator := strings.Repeat("\n", max(1, v.frameGapLines/2+1))
	return v.Header + separator + strings.Join(lines[v.Offset:v.End], "\n") + separator + v.Footer
}
