package tui

import (
	"context"
	"fmt"
	"strings"

	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

type schemaAttributeCurrentLoadedMsg struct {
	schema   string
	table    string
	column   string
	metadata *driver.ColumnAttributeMetadata
	err      error
}

func (m *Model) startSchemaAttributeCurrentLoad() tea.Cmd {
	state := &m.schemaAttributes
	if len(state.inputs) < 3 {
		return nil
	}
	schema := strings.TrimSpace(state.inputs[0].Value())
	table := strings.TrimSpace(state.inputs[1].Value())
	column := strings.TrimSpace(state.inputs[2].Value())
	if schema == "" || table == "" || column == "" {
		return nil
	}
	state.targetEditing = false
	state.current = nil
	state.currentErr = ""
	state.currentLoading = true
	profile := m.result.Profile
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		state.currentLoading = false
		state.currentErr = err.Error()
		return nil
	}
	inspector, ok := drv.(driver.ColumnAttributeInspector)
	if !ok {
		state.currentLoading = false
		state.currentErr = fmt.Sprintf("%s does not support current column attributes", drv.Name())
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), schemaAttributeTimeout)
		defer cancel()
		metadata, err := inspector.GetColumnAttributeMetadata(ctx, profile, schema, table, column)
		return schemaAttributeCurrentLoadedMsg{schema: schema, table: table, column: column, metadata: metadata, err: err}
	}
}

func (m Model) schemaAttributeCurrentBody() string {
	state := m.schemaAttributes
	if state.currentLoading {
		return renderCard(m.width, "Current Attributes", mutedStyle.Render("Loading current PostgreSQL column attributes…"))
	}
	if state.currentErr != "" {
		return renderCard(m.width, "Current Attributes", warningStyle.Render(state.currentErr+"; requested changes remain manual."))
	}
	if state.current == nil {
		return renderCard(m.width, "Current Attributes", mutedStyle.Render("Choose a column to load its current state before requesting changes."))
	}
	defaultValue := state.current.Default
	if defaultValue == "" {
		defaultValue = "none"
	}
	uniqueValue := "none"
	if len(state.current.UniqueConstraints) > 0 {
		uniqueValue = strings.Join(state.current.UniqueConstraints, ", ")
	}
	rows := []kvRow{
		{Label: "Nullable", Value: nullableCurrentLabel(state.current.Nullable), Kind: attributeBadge(&state.current.Nullable)},
		{Label: "Default", Value: defaultValue, Kind: badgeNeutral},
		{Label: "Unique", Value: uniqueValue, Kind: badgeNeutral},
	}
	if len(state.current.PrimaryKeyConstraints) > 0 {
		rows = append(rows, kvRow{Label: "Primary key", Value: strings.Join(state.current.PrimaryKeyConstraints, ", ") + " (not editable in v1)", Kind: badgeWarning})
	}
	return renderCard(m.width, "Current Attributes", renderKeyValueGrid(rows, 14))
}

func nullableCurrentLabel(nullable bool) string {
	if nullable {
		return "NULL"
	}
	return "NOT NULL"
}
