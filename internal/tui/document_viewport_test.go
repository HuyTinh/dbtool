package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestFixedDocumentViewportKeepsFrameFixedAndReachesLastLine(t *testing.T) {
	lines := []string{"first", "second", "third", "last"}
	viewport := newFixedDocumentViewport(lines, 5, "Header", 2, 99, func(viewport scrollViewport) string {
		return fmt.Sprintf("Footer %d–%d / %d", viewport.Offset+1, viewport.End, viewport.Total)
	})

	if viewport.Offset != 3 || viewport.End != 4 || viewport.Visible != 1 {
		t.Fatalf("viewport = %+v, want offset=3 end=4 visible=1", viewport.scrollViewport)
	}
	view := viewport.Render(lines)
	if !strings.Contains(view, "Header") || !strings.Contains(view, "last") || !strings.Contains(view, "Footer 4–4 / 4") {
		t.Fatalf("fixed frame must preserve header, final document line, and footer:\n%s", view)
	}
	if got := lipgloss.Height(view); got != 5 {
		t.Fatalf("rendered height = %d, want 5", got)
	}
}

func TestFixedDocumentViewportOmitsOverflowFooterWhenDocumentFits(t *testing.T) {
	lines := []string{"one", "two", "three", "four"}
	viewport := newFixedDocumentViewport(lines, 9, "Header", 2, 3, func(viewport scrollViewport) string {
		if viewport.Total > viewport.Visible {
			return "scroll controls"
		}
		return "Footer"
	})

	if viewport.Offset != 0 || viewport.Visible != 5 || viewport.End != len(lines) {
		t.Fatalf("viewport = %+v, want all content visible", viewport.scrollViewport)
	}
	view := viewport.Render(lines)
	if strings.Contains(view, "scroll controls") || !strings.Contains(view, "Footer") {
		t.Fatalf("non-overflow frame must render its compact footer:\n%s", view)
	}
}
