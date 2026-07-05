package tui

import (
	"dbtool/internal/config"

	tea "github.com/charmbracelet/bubbletea"
)

// TUI mode: restore or dump
type Mode int

const (
	ModeRestore Mode = iota
	ModeDump
	ModeMigrate
)

// RestoreSettings holds target configuration for dbtool restore command
type RestoreSettings struct {
	Format          string
	Jobs            int
	Clean           bool
	DryRun          bool
	CreateIfMissing bool
	Optimize        bool
	IncludeTable    []string
	ExcludeTable    []string
	IncludeSchema   []string
	ExcludeSchema   []string
}

// DumpSettings holds target configuration for dbtool dump command
type DumpSettings struct {
	Format        string
	IncludeTable  []string
	ExcludeTable  []string
	IncludeSchema []string
	ExcludeSchema []string
}

// MigrateSettings holds configuration for dbtool migrate command
type MigrateSettings struct {
	Format          string
	SchemaOnly      bool
	DataOnly        bool
	Clean           bool
	CreateIfMissing bool
	Jobs            int
	Optimize        bool
	IncludeTable    []string
	ExcludeTable    []string
	IncludeSchema   []string
	ExcludeSchema   []string
}

// Run launches the interactive TUI (starts at mode-select screen) and returns the user selection.
func Run(cfg *config.Config, initSettings RestoreSettings) (Result, error) {
	m := NewModel(cfg, initSettings)
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return Result{}, err
	}

	final, ok := finalModel.(Model)
	if !ok {
		return Result{}, nil
	}

	return final.result, nil
}
