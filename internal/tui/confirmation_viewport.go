package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type confirmationReview int

const (
	restoreConfirmationReview confirmationReview = iota
	dumpConfirmationReview
	migrateConfirmationReview
)

const confirmationReviewFrameGaps = 2

func (m Model) confirmationReviewViewport(review confirmationReview, lines []string) fixedDocumentViewport {
	title, subtitle := m.confirmationReviewTitle(review)
	header := renderAppHeader(title, subtitle, m.width)
	return newFixedDocumentViewport(lines, m.height, header, confirmationReviewFrameGaps, m.confirmationReviewOffset(review), func(viewport scrollViewport) string {
		return renderCommandBar(m.width, m.confirmationReviewFooterHints(review, viewport))
	})
}

func (m Model) confirmationReviewTitle(review confirmationReview) (string, string) {
	switch review {
	case dumpConfirmationReview:
		return "Confirm Dump", "Pre-flight review before exporting source database"
	case migrateConfirmationReview:
		return "Confirm Migrate", "Review source and target before copying data"
	default:
		return "Confirm Restore", "Pre-flight review before database restore"
	}
}

func (m Model) confirmationReviewOffset(review confirmationReview) int {
	switch review {
	case dumpConfirmationReview:
		return m.dumpConfirmOffset
	case migrateConfirmationReview:
		return m.migrateConfirmOffset
	default:
		return m.restoreConfirmOffset
	}
}

func (m *Model) setConfirmationReviewOffset(review confirmationReview, offset int) {
	switch review {
	case dumpConfirmationReview:
		m.dumpConfirmOffset = offset
	case migrateConfirmationReview:
		m.migrateConfirmOffset = offset
	default:
		m.restoreConfirmOffset = offset
	}
}

func (m Model) confirmationReviewFooterHints(review confirmationReview, viewport scrollViewport) []keyHint {
	var hints []keyHint
	switch review {
	case dumpConfirmationReview:
		hints = []keyHint{{Key: "Enter/Y", Label: "dump"}, {Key: "B", Label: "browse objects"}, {Key: "T/H", Label: "filters"}}
	case migrateConfirmationReview:
		hints = []keyHint{{Key: "C", Label: "clean"}, {Key: "M", Label: "create-db"}, {Key: "S/A", Label: "mode"}, {Key: "O", Label: "optimize"}, {Key: "B", Label: "browse objects"}, {Key: "T/H", Label: "filters"}, {Key: "Enter", Label: "proceed"}}
	default:
		hints = []keyHint{{Key: "C", Label: "clean"}, {Key: "M", Label: "create-db"}, {Key: "O", Label: "optimize"}, {Key: "+/-", Label: "jobs"}, {Key: "T/H", Label: "filters"}, {Key: "Enter", Label: "proceed"}}
	}
	if viewport.Total > viewport.Visible {
		hints = append(hints,
			keyHint{Key: fmt.Sprintf("%d–%d / %d", viewport.Offset+1, viewport.End, viewport.Total), Label: "position"},
			keyHint{Key: "↑/k", Label: "up"}, keyHint{Key: "↓/j", Label: "down"}, keyHint{Key: "PgUp/PgDn", Label: "page"}, keyHint{Key: "Home/End", Label: "bounds"})
	}
	return append(hints, keyHint{Key: "Esc", Label: "back", Danger: true})
}

func (m Model) confirmationReviewVisibleHeight(review confirmationReview) int {
	return m.confirmationReviewViewport(review, m.confirmationReviewContentLines(review)).Visible
}

func (m Model) confirmationReviewMaxOffset(review confirmationReview) int {
	return scrollMaxOffset(len(m.confirmationReviewContentLines(review)), m.confirmationReviewVisibleHeight(review))
}

func (m *Model) clampConfirmationReviewOffset(review confirmationReview) {
	m.setConfirmationReviewOffset(review, clampScrollOffset(m.confirmationReviewOffset(review), len(m.confirmationReviewContentLines(review)), m.confirmationReviewVisibleHeight(review)))
}

func (m *Model) clampConfirmationReviewOffsets() {
	m.clampConfirmationReviewOffset(restoreConfirmationReview)
	m.clampConfirmationReviewOffset(dumpConfirmationReview)
	m.clampConfirmationReviewOffset(migrateConfirmationReview)
}

func (m *Model) updateConfirmationReviewScroll(review confirmationReview, msg tea.KeyMsg) bool {
	if m.confirmationReviewMaxOffset(review) == 0 {
		return false
	}
	offset := m.confirmationReviewOffset(review)
	switch msg.String() {
	case "up", "k", "K":
		offset--
	case "down", "j", "J":
		offset++
	case "pgup":
		offset -= m.confirmationReviewVisibleHeight(review)
	case "pgdown":
		offset += m.confirmationReviewVisibleHeight(review)
	case "home":
		offset = 0
	case "end":
		offset = m.confirmationReviewMaxOffset(review)
	default:
		return false
	}
	m.setConfirmationReviewOffset(review, offset)
	m.clampConfirmationReviewOffset(review)
	return true
}

func (m Model) confirmationReviewContentLines(review confirmationReview) []string {
	switch review {
	case dumpConfirmationReview:
		return m.dumpConfirmContentLines()
	case migrateConfirmationReview:
		return m.migrateConfirmContentLines()
	default:
		return m.restoreConfirmContentLines()
	}
}
