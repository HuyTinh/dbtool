package tui

import (
	"context"
	"fmt"
	"strings"

	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

type schemaAttributeSearchLoadedMsg struct {
	results []driver.CatalogColumnSearchResult
	err     error
}

func guidedIntentLabels() []string {
	return []string{
		"Make a field required",
		"Allow a field to be left blank",
		"Set an automatic default value",
		"Remove an automatic default value",
		"Prevent duplicate values",
	}
}

func guidedIntentSummary(intent schemaGuidedIntent) string {
	return guidedIntentLabels()[intent]
}

func (m *Model) startSchemaAttributeSearch() tea.Cmd {
	state := &m.schemaAttributes
	query := strings.TrimSpace(state.search.Value())
	if query == "" {
		state.searchErr = "Type a field name, table name, or both before searching."
		return nil
	}
	profile := m.result.Profile
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		state.searchErr = err.Error()
		return nil
	}
	searcher, ok := drv.(driver.CatalogColumnSearcher)
	if !ok {
		state.searchErr = fmt.Sprintf("%s does not support field search; press X for the advanced table browser", drv.Name())
		return nil
	}
	state.searchLoading = true
	state.searchErr = ""
	state.searchResults = nil
	state.guidedIdx = 0
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), schemaAttributeTimeout)
		defer cancel()
		results, err := searcher.SearchColumns(ctx, profile, query, 20)
		return schemaAttributeSearchLoadedMsg{results: results, err: err}
	}
}

func (m Model) updateGuidedSchemaAttributeForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &m.schemaAttributes
	switch state.guidedStep {
	case schemaGuidedChooseIntent:
		switch msg.String() {
		case "down", "j":
			state.guidedIdx = min(state.guidedIdx+1, len(guidedIntentLabels())-1)
		case "up", "k":
			state.guidedIdx = max(0, state.guidedIdx-1)
		case "enter":
			state.guidedIntent = schemaGuidedIntent(state.guidedIdx)
			state.guidedStep = schemaGuidedSearchField
			state.search.Focus()
		case "x", "X":
			state.guidedStep = schemaGuidedAdvanced
			state.targetEditing = true
			state.workspace = false
			state.focused = schemaAttributeFocusSchema
			state.focusInput()
			return m, m.startSchemaAttributeSuggestions()
		case "esc":
			m.step = stepSelectMode
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil

	case schemaGuidedSearchField:
		switch msg.String() {
		case "enter":
			state.guidedStep = schemaGuidedChooseField
			return m, m.startSchemaAttributeSearch()
		case "esc":
			m.step = stepSelectMode
			state.search.Blur()
			return m, nil
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		state.search, cmd = state.search.Update(msg)
		return m, cmd

	case schemaGuidedChooseField:
		if state.searchLoading {
			if msg.String() == "esc" {
				state.guidedStep = schemaGuidedSearchField
				state.search.Focus()
			}
			return m, nil
		}
		switch msg.String() {
		case "down", "j":
			state.guidedIdx = min(state.guidedIdx+1, max(0, len(state.searchResults)-1))
		case "up", "k":
			state.guidedIdx = max(0, state.guidedIdx-1)
		case "enter":
			if state.guidedIdx >= len(state.searchResults) {
				return m, nil
			}
			selected := state.searchResults[state.guidedIdx]
			state.inputs[0].SetValue(selected.Schema)
			state.inputs[1].SetValue(selected.Table)
			state.inputs[2].SetValue(selected.Column)
			state.targetEditing = false
			state.workspace = true
			state.selectedColumns = map[string]bool{selected.Column: true}
			state.actionMenu = false
			state.resetRequestedChanges()
			state.guidedStep = schemaGuidedAdvanced
			return m, m.startSchemaAttributeColumnSuggestions(selected.Schema, selected.Table)
		case "esc":
			state.guidedStep = schemaGuidedSearchField
			state.search.Focus()
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil

	case schemaGuidedDraft:
		switch msg.String() {
		case "r", "R", "ctrl+enter", "f5":
			if _, err := m.schemaAttributeChange(); err != nil {
				state.err = err
				return m, nil
			}
			m.step = stepSchemaAttributePreflight
			return m, m.startSchemaAttributePreflight()
		case "t", "T":
			state.guidedStep = schemaGuidedChooseIntent
			state.guidedIdx = 0
			state.current = nil
			state.currentLoading = false
			state.currentErr = ""
			state.resetRequestedChanges()
			return m, nil
		case "esc":
			state.guidedStep = schemaGuidedChooseField
			state.search.Focus()
			return m, nil
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		if state.defaultMode == schemaDefaultSet {
			var cmd tea.Cmd
			state.inputs[3], cmd = state.inputs[3].Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *Model) applyGuidedIntent() {
	state := &m.schemaAttributes
	switch state.guidedIntent {
	case schemaIntentRequireValue:
		value := false
		state.nullable = &value
	case schemaIntentAllowBlank:
		value := true
		state.nullable = &value
	case schemaIntentSetDefault:
		state.defaultMode = schemaDefaultSet
		state.focused = schemaAttributeFocusDefaultExpression
		state.focusInput()
	case schemaIntentRemoveDefault:
		state.defaultMode = schemaDefaultDrop
	case schemaIntentPreventDuplicates:
		value := true
		state.unique = &value
		state.focused = schemaAttributeFocusUniqueConstraint
		state.focusInput()
	}
}

func (m Model) guidedSchemaAttributeBody() string {
	state := m.schemaAttributes
	switch state.guidedStep {
	case schemaGuidedChooseIntent:
		rows := make([]string, 0, len(guidedIntentLabels()))
		for i, label := range guidedIntentLabels() {
			marker := "  "
			if i == state.guidedIdx {
				marker = "> "
			}
			rows = append(rows, marker+label)
		}
		body := strings.Join(rows, "\n")
		if state.notice != "" {
			body = subtextStyle.Render(state.notice) + "\n\n" + body
		}
		return renderCard(m.width, "What do you want to change?", body)
	case schemaGuidedSearchField:
		body := "Find field: " + state.search.View() + "\n" + mutedStyle.Render("Search finds the table, then you can select one or more fields.")
		if state.searchErr != "" {
			body += "\n" + errorStyle.Render(state.searchErr)
		}
		return renderCard(m.width, "Find fields to change", body)
	case schemaGuidedChooseField:
		if state.searchLoading {
			return renderCard(m.width, "Finding fields", mutedStyle.Render("Searching the database catalog…"))
		}
		if state.searchErr != "" {
			return renderCard(m.width, "Find field", errorStyle.Render(state.searchErr))
		}
		if len(state.searchResults) == 0 {
			return renderCard(m.width, "Find field", mutedStyle.Render("No fields matched. Press Esc to change your search."))
		}
		rows := make([]string, 0, len(state.searchResults))
		for i, result := range state.searchResults {
			marker := "  "
			if i == state.guidedIdx {
				marker = "> "
			}
			rows = append(rows, marker+result.Schema+"."+result.Table+"."+result.Column+"  "+mutedStyle.Render(result.Type))
		}
		return renderCard(m.width, "Choose the field", strings.Join(rows, "\n"))
	case schemaGuidedDraft:
		target := strings.Join([]string{state.inputs[0].Value(), state.inputs[1].Value(), state.inputs[2].Value()}, ".")
		body := "Field: " + valueStyle.Render(target) + "\n" + "Change: " + valueStyle.Render(guidedIntentSummary(state.guidedIntent))
		if state.currentLoading {
			body += "\n" + mutedStyle.Render("Checking the current field setting…")
		} else if state.currentErr != "" {
			body += "\n" + warningStyle.Render(state.currentErr)
		} else if state.current != nil {
			body += "\n" + "Current: " + nullableCurrentLabel(state.current.Nullable)
		}
		if state.defaultMode == schemaDefaultSet {
			body += "\n\nDefault value: " + state.inputs[3].View() + "\n" + warningStyle.Render("This is a PostgreSQL SQL expression.")
		}
		if state.unique != nil && *state.unique {
			body += "\n\nUNIQUE name (optional): " + state.inputs[4].View()
		}
		if state.err != nil {
			body += "\n\n" + errorStyle.Render(state.err.Error())
		}
		return renderCard(m.width, "Ready to review", body)
	}
	return ""
}

func (m Model) viewGuidedSchemaAttributeForm() string {
	state := m.schemaAttributes
	hints := []keyHint{{Key: "Esc", Label: "back", Danger: true}, {Key: "Q", Label: "quit", Danger: true}}
	switch state.guidedStep {
	case schemaGuidedChooseIntent:
		hints = append([]keyHint{{Key: "↑/↓", Label: "choose"}, {Key: "Enter", Label: "continue"}, {Key: "X", Label: "advanced"}}, hints...)
	case schemaGuidedSearchField:
		hints = append([]keyHint{{Key: "Enter", Label: "search"}}, hints...)
	case schemaGuidedChooseField:
		hints = append([]keyHint{{Key: "↑/↓", Label: "choose"}, {Key: "Enter", Label: "continue"}}, hints...)
	case schemaGuidedDraft:
		hints = append([]keyHint{{Key: "R", Label: "review"}, {Key: "T", Label: "start over"}}, hints...)
	}
	return renderScreenFrame(m.width, "Guided Field Change", "Choose the outcome first; technical details stay in Review", m.guidedSchemaAttributeBody(), hints)
}
