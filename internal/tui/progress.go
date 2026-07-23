package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

const readOnlyActivityTickInterval = 120 * time.Millisecond

var readOnlyActivityFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type readOnlyActivityState struct {
	frame     int
	startedAt time.Time
	elapsed   time.Duration
}

type readOnlyActivityTickMsg time.Time

func nextReadOnlyActivityTick() tea.Cmd {
	return tea.Tick(readOnlyActivityTickInterval, func(at time.Time) tea.Msg {
		return readOnlyActivityTickMsg(at)
	})
}

func (m *Model) startReadOnlyActivity() tea.Cmd {
	m.activity = readOnlyActivityState{startedAt: time.Now()}
	return nextReadOnlyActivityTick()
}

func (m Model) readOnlyActivityLoading() bool {
	return m.health.loading || m.size.loading || m.sessions.loading
}

// renderActivityProgress marks an ongoing operation whose completion cannot be
// estimated without implying a misleading percentage.
func renderActivityProgress(width, frame int, elapsed time.Duration) string {
	if width < 4 {
		width = 4
	}
	filled := width / 3
	if filled < 1 {
		filled = 1
	}
	spinner := readOnlyActivityFrames[frame%len(readOnlyActivityFrames)]
	bar := lipgloss.NewStyle().Foreground(colorAccent).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Repeat("░", width-filled))
	return fmt.Sprintf("%s Collecting… Elapsed: %s\n%s", spinner, elapsed.Round(time.Second), bar)
}
