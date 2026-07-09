package tui

import (
	"dbtool/internal/config"
	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

type progressMsg driver.Progress

type restoreFinishedMsg struct {
	err error
}

func listenToProgress(ch <-chan driver.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return restoreFinishedMsg{}
		}
		return progressMsg(p)
	}
}

type dumpProgressMsg driver.Progress

type dumpFinishedMsg struct {
	err error
}

func listenToDumpProgress(ch <-chan driver.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return dumpFinishedMsg{}
		}
		return dumpProgressMsg(p)
	}
}

type migrateProgressMsg driver.Progress

type migratePhaseFinishedMsg struct {
	err   error
	phase int // 0=dump, 1=restore
}

func listenToMigrateProgress(ch <-chan driver.Progress, phase int) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return migratePhaseFinishedMsg{phase: phase}
		}
		return migrateProgressMsg(p)
	}
}

type runtimeDetectProgressMsg struct {
	Percent float64
	Message string
	Ch      <-chan tea.Msg
}

type runtimeDetectFinishedMsg struct {
	Profile config.Profile
	Message string
	Err     error
}

func listenToRuntimeDetect(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return runtimeDetectFinishedMsg{}
		}
		return msg
	}
}
