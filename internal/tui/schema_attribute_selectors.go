package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

type schemaAttributeSchemasLoadedMsg struct {
	schemas []string
	err     error
}

type schemaAttributeTablesLoadedMsg struct {
	tables []driver.CatalogTable
	err    error
}

type schemaAttributeColumnsLoadedMsg struct {
	columns []string
	err     error
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (m *Model) startSchemaAttributeSuggestions() tea.Cmd {
	state := &m.schemaAttributes
	state.selectorLoading = true
	state.selectorErr = ""
	state.suggestionIdx = 0
	profile := m.result.Profile
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		state.selectorLoading = false
		state.selectorErr = err.Error()
		return nil
	}
	catalog, ok := drv.(driver.CatalogLister)
	if !ok {
		state.selectorLoading = false
		state.selectorErr = fmt.Sprintf("%s does not support catalog suggestions; type values manually", drv.Name())
		return nil
	}
	return loadSchemaAttributeSchemas(catalog, profile)
}

func (m *Model) startSchemaAttributeTableSuggestions(schema string) tea.Cmd {
	state := &m.schemaAttributes
	state.selectorLoading = true
	state.selectorErr = ""
	state.tables = nil
	state.columns = nil
	state.tableSchema = schema
	state.columnSchema = ""
	state.columnTable = ""
	state.suggestionIdx = 0
	profile := m.result.Profile
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		state.selectorLoading = false
		state.selectorErr = err.Error()
		return nil
	}
	catalog, ok := drv.(driver.CatalogLister)
	if !ok {
		state.selectorLoading = false
		state.selectorErr = fmt.Sprintf("%s does not support table suggestions; type values manually", drv.Name())
		return nil
	}
	return loadSchemaAttributeTables(catalog, profile, schema)
}

func (m *Model) startSchemaAttributeColumnSuggestions(schema, table string) tea.Cmd {
	state := &m.schemaAttributes
	state.selectorLoading = true
	state.selectorErr = ""
	state.columns = nil
	state.columnSchema = schema
	state.columnTable = table
	state.suggestionIdx = 0
	profile := m.result.Profile
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		state.selectorLoading = false
		state.selectorErr = err.Error()
		return nil
	}
	catalog, ok := drv.(driver.CatalogColumnLister)
	if !ok {
		state.selectorLoading = false
		state.selectorErr = fmt.Sprintf("%s does not support column suggestions; type values manually", drv.Name())
		return nil
	}
	return loadSchemaAttributeColumns(catalog, profile, schema, table)
}

func loadSchemaAttributeSchemas(catalog driver.CatalogLister, profile config.Profile) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		schemas, err := catalog.ListSchemas(ctx, profile)
		return schemaAttributeSchemasLoadedMsg{schemas: schemas, err: err}
	}
}

func loadSchemaAttributeTables(catalog driver.CatalogLister, profile config.Profile, schema string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tables, err := catalog.ListTables(ctx, profile, []string{schema})
		return schemaAttributeTablesLoadedMsg{tables: tables, err: err}
	}
}

func loadSchemaAttributeColumns(catalog driver.CatalogColumnLister, profile config.Profile, schema, table string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		columns, err := catalog.ListColumns(ctx, profile, schema, table)
		return schemaAttributeColumnsLoadedMsg{columns: columns, err: err}
	}
}

func (m Model) visibleSchemaAttributeSuggestions() []string {
	state := m.schemaAttributes
	if len(state.inputs) < 3 || state.focused > 2 {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(state.inputs[state.focused].Value()))
	matches := make([]string, 0)
	appendMatch := func(value string) {
		if query == "" || strings.Contains(strings.ToLower(value), query) {
			matches = append(matches, value)
		}
	}
	switch state.focused {
	case 0:
		for _, schema := range state.schemas {
			appendMatch(schema)
		}
	case 1:
		if state.tableSchema != strings.TrimSpace(state.inputs[0].Value()) {
			return nil
		}
		for _, table := range state.tables {
			appendMatch(table.Name)
		}
	case 2:
		if state.columnSchema != strings.TrimSpace(state.inputs[0].Value()) || state.columnTable != strings.TrimSpace(state.inputs[1].Value()) {
			return nil
		}
		for _, column := range state.columns {
			appendMatch(column)
		}
	}
	return matches
}

func (m *Model) moveSchemaAttributeSuggestion(delta int) bool {
	items := m.visibleSchemaAttributeSuggestions()
	if len(items) == 0 {
		return false
	}
	m.schemaAttributes.suggestionIdx = min(max(0, m.schemaAttributes.suggestionIdx+delta), len(items)-1)
	return true
}

func (m *Model) acceptSchemaAttributeSuggestion() tea.Cmd {
	state := &m.schemaAttributes
	items := m.visibleSchemaAttributeSuggestions()
	if len(items) == 0 || state.suggestionIdx >= len(items) {
		return nil
	}
	selected := items[state.suggestionIdx]
	switch state.focused {
	case 0:
		state.inputs[0].SetValue(selected)
		state.inputs[1].SetValue("")
		state.inputs[2].SetValue("")
		state.notice = ""
		state.resetRequestedChanges()
		state.current = nil
		state.currentLoading = false
		state.currentErr = ""
		return m.startSchemaAttributeTableSuggestions(selected)
	case 1:
		state.inputs[1].SetValue(selected)
		state.inputs[2].SetValue("")
		state.targetEditing = true
		state.workspace = false
		state.notice = ""
		state.resetRequestedChanges()
		state.current = nil
		state.currentLoading = false
		state.currentErr = ""
		return m.startSchemaAttributeColumnSuggestions(strings.TrimSpace(state.inputs[0].Value()), selected)
	case 2:
		state.inputs[2].SetValue(selected)
		state.notice = ""
		state.resetRequestedChanges()
		return m.startSchemaAttributeCurrentLoad()
	}
	return nil
}

func (m *Model) loadSchemaAttributeDependentSuggestions() tea.Cmd {
	state := &m.schemaAttributes
	if len(state.inputs) < 3 {
		return nil
	}
	switch state.focused {
	case 0:
		schema := strings.TrimSpace(state.inputs[0].Value())
		if schema != "" && (state.tableSchema != schema || len(state.tables) == 0) && !state.selectorLoading {
			return m.startSchemaAttributeTableSuggestions(schema)
		}
	case 1:
		schema := strings.TrimSpace(state.inputs[0].Value())
		table := strings.TrimSpace(state.inputs[1].Value())
		if schema != "" && table != "" && (state.columnSchema != schema || state.columnTable != table || len(state.columns) == 0) && !state.selectorLoading {
			return m.startSchemaAttributeColumnSuggestions(schema, table)
		}
	case 2:
		if strings.TrimSpace(state.inputs[0].Value()) != "" && strings.TrimSpace(state.inputs[1].Value()) != "" && strings.TrimSpace(state.inputs[2].Value()) != "" && !state.currentLoading && state.current == nil {
			return m.startSchemaAttributeCurrentLoad()
		}
	}
	return nil
}

func (m Model) schemaAttributeSuggestionBody() string {
	state := m.schemaAttributes
	if len(state.inputs) < 3 || state.focused > 2 {
		return ""
	}
	titles := []string{"Schema suggestions", "Table suggestions", "Column suggestions"}
	if state.selectorLoading {
		return renderCard(m.width, titles[state.focused], mutedStyle.Render("Loading catalog suggestions…"))
	}
	if state.selectorErr != "" {
		return renderCard(m.width, titles[state.focused], warningStyle.Render(state.selectorErr))
	}
	items := m.visibleSchemaAttributeSuggestions()
	if len(items) == 0 {
		return renderCard(m.width, titles[state.focused], mutedStyle.Render("No matching catalog values; continue typing a value manually."))
	}
	start := min(state.suggestionIdx, max(0, len(items)-5))
	end := min(start+5, len(items))
	rows := make([]string, 0, end-start+1)
	for i := start; i < end; i++ {
		marker := "  "
		if i == state.suggestionIdx {
			marker = "> "
		}
		rows = append(rows, marker+items[i])
	}
	if len(items) > end {
		rows = append(rows, mutedStyle.Render(fmt.Sprintf("… %d more", len(items)-end)))
	}
	return renderCard(m.width, titles[state.focused], strings.Join(rows, "\n"))
}
