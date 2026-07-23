package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dbtool/internal/driver"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const schemaAttributeTimeout = 10 * time.Second
const schemaAttributeFrameGaps = 2

type schemaDefaultMode int

const (
	schemaDefaultUnchanged schemaDefaultMode = iota
	schemaDefaultSet
	schemaDefaultDrop
)

type schemaAttributeAction int

const (
	schemaActionNullable schemaAttributeAction = iota
	schemaActionSetDefault
	schemaActionDropDefault
	schemaActionUnique
	schemaActionClearDraft
)

type schemaAttributeActionOption struct {
	Kind  schemaAttributeAction
	Label string
}

type schemaGuidedStep int

const (
	schemaGuidedChooseIntent schemaGuidedStep = iota
	schemaGuidedSearchField
	schemaGuidedChooseField
	schemaGuidedDraft
	schemaGuidedAdvanced
)

type schemaGuidedIntent int

const (
	schemaIntentRequireValue schemaGuidedIntent = iota
	schemaIntentAllowBlank
	schemaIntentSetDefault
	schemaIntentRemoveDefault
	schemaIntentPreventDuplicates
)

const (
	schemaAttributeFocusSchema = iota
	schemaAttributeFocusTable
	schemaAttributeFocusColumn
	schemaAttributeFocusNullable
	schemaAttributeFocusDefault
	schemaAttributeFocusDefaultExpression
	schemaAttributeFocusUnique
	schemaAttributeFocusUniqueConstraint
	schemaAttributeFocusCount
)

type schemaAttributeState struct {
	inputs          []textinput.Model
	search          textinput.Model
	guidedStep      schemaGuidedStep
	guidedIntent    schemaGuidedIntent
	guidedIdx       int
	searchResults   []driver.CatalogColumnSearchResult
	searchLoading   bool
	searchErr       string
	focused         int
	nullable        *bool
	unique          *bool
	defaultMode     schemaDefaultMode
	plan            *driver.ColumnAttributePlan
	batchPlan       *driver.ColumnAttributeBatchPlan
	selectedColumns map[string]bool
	loading         bool
	applying        bool
	err             error
	offset          int
	schemas         []string
	tables          []driver.CatalogTable
	columns         []string
	tableSchema     string
	columnSchema    string
	columnTable     string
	current         *driver.ColumnAttributeMetadata
	currentLoading  bool
	currentErr      string
	targetEditing   bool
	workspace       bool
	columnIdx       int
	actionMenu      bool
	actionIdx       int
	notice          string
	selectorLoading bool
	selectorErr     string
	suggestionIdx   int
}

type schemaAttributePreflightLoadedMsg struct {
	plan      *driver.ColumnAttributePlan
	batchPlan *driver.ColumnAttributeBatchPlan
	err       error
}

type schemaAttributeApplyFinishedMsg struct {
	plan      *driver.ColumnAttributePlan
	batchPlan *driver.ColumnAttributeBatchPlan
	err       error
}

func (m *Model) startSchemaAttributeForm() tea.Cmd {
	inputs := make([]textinput.Model, 5)
	for i := range inputs {
		input := textinput.New()
		input.CharLimit = 1024
		input.Width = 58
		inputs[i] = input
	}
	inputs[0].Placeholder = "Schema (default: public)"
	inputs[0].SetValue("public")
	inputs[0].Focus()
	inputs[1].Placeholder = "Table name"
	inputs[2].Placeholder = "Column name"
	inputs[3].Placeholder = "PostgreSQL DEFAULT expression"
	inputs[4].Placeholder = "UNIQUE constraint name (optional)"
	search := textinput.New()
	search.Placeholder = "e.g. customer email"
	search.CharLimit = 256
	search.Width = 58
	m.schemaAttributes = schemaAttributeState{inputs: inputs, search: search, guidedStep: schemaGuidedSearchField, targetEditing: true}
	m.schemaAttributes.search.Focus()
	return nil
}

func (m Model) updateSchemaAttributeForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &m.schemaAttributes
	if state.guidedStep != schemaGuidedAdvanced {
		return m.updateGuidedSchemaAttributeForm(msg)
	}
	switch msg.String() {
	case "esc":
		if state.actionMenu {
			state.actionMenu = false
			return m, nil
		}
		m.step = stepSelectMode
		return m, nil
	case "t", "T":
		state.targetEditing = true
		state.workspace = false
		state.actionMenu = false
		state.notice = ""
		state.focused = schemaAttributeFocusSchema
		state.focusInput()
		return m, nil
	case "n", "N":
		return m, m.selectNextSchemaAttributeColumn()
	case "tab":
		cmd := m.loadSchemaAttributeDependentSuggestions()
		state.advanceFocus(1)
		state.focusInput()
		return m, cmd
	case "shift+tab":
		state.advanceFocus(-1)
		state.focusInput()
		return m, nil
	case "down":
		if state.workspace {
			if state.actionMenu {
				actions := m.schemaAttributeActions()
				if len(actions) > 0 {
					state.actionIdx = min(state.actionIdx+1, len(actions)-1)
				}
				return m, nil
			}
			return m, m.moveSchemaAttributeWorkspaceColumn(1)
		}
		if m.moveSchemaAttributeSuggestion(1) {
			return m, nil
		}
	case "up":
		if state.workspace {
			if state.actionMenu {
				state.actionIdx = max(0, state.actionIdx-1)
				return m, nil
			}
			return m, m.moveSchemaAttributeWorkspaceColumn(-1)
		}
		if m.moveSchemaAttributeSuggestion(-1) {
			return m, nil
		}
	case "r", "R", "ctrl+enter", "f5":
		if _, err := m.schemaAttributeChange(); err != nil {
			state.err = err
			return m, nil
		}
		m.step = stepSchemaAttributePreflight
		return m, m.startSchemaAttributePreflight()
	case "enter":
		if state.workspace {
			if state.actionMenu {
				m.applySchemaAttributeAction()
				return m, nil
			}
			if state.current != nil && !state.currentLoading {
				state.actionMenu = true
				state.actionIdx = 0
			}
			return m, nil
		}
		if state.focused <= schemaAttributeFocusColumn && len(m.visibleSchemaAttributeSuggestions()) > 0 {
			return m, m.acceptSchemaAttributeSuggestion()
		}
		if state.focused >= schemaAttributeFocusNullable && state.focused <= schemaAttributeFocusUnique {
			m.activateSchemaAttributeControl()
		}
		return m, nil
	case " ", "space":
		if state.workspace && !state.actionMenu && state.columnIdx >= 0 && state.columnIdx < len(state.columns) {
			if state.selectedColumns == nil {
				state.selectedColumns = make(map[string]bool)
			}
			column := state.columns[state.columnIdx]
			state.selectedColumns[column] = !state.selectedColumns[column]
			if !state.selectedColumns[column] {
				delete(state.selectedColumns, column)
			}
			return m, nil
		}
		if state.focused >= schemaAttributeFocusNullable && state.focused <= schemaAttributeFocusUnique {
			m.activateSchemaAttributeControl()
		}
		return m, nil
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	if inputIndex, ok := state.inputIndexForFocus(); ok {
		previous := state.inputs[inputIndex].Value()
		state.inputs[inputIndex], cmd = state.inputs[inputIndex].Update(msg)
		if inputIndex <= schemaAttributeFocusColumn && state.inputs[inputIndex].Value() != previous {
			state.notice = ""
			state.resetRequestedChanges()
			state.current = nil
			state.currentLoading = false
			state.currentErr = ""
		}
	}
	if state.focused <= schemaAttributeFocusColumn {
		state.suggestionIdx = 0
	}
	return m, cmd
}

func (s *schemaAttributeState) focusInput() {
	for i := range s.inputs {
		if inputIndex, ok := s.inputIndexForFocus(); ok && i == inputIndex {
			s.inputs[i].Focus()
		} else {
			s.inputs[i].Blur()
		}
	}
}

func (s *schemaAttributeState) inputIndexForFocus() (int, bool) {
	switch s.focused {
	case schemaAttributeFocusSchema, schemaAttributeFocusTable, schemaAttributeFocusColumn:
		return s.focused, true
	case schemaAttributeFocusDefaultExpression:
		return 3, true
	case schemaAttributeFocusUniqueConstraint:
		return 4, true
	default:
		return 0, false
	}
}

func (s *schemaAttributeState) focusAvailable(focus int) bool {
	if s.targetEditing && focus == schemaAttributeFocusColumn {
		return false
	}
	if s.workspace && focus <= schemaAttributeFocusColumn {
		return false
	}
	switch focus {
	case schemaAttributeFocusDefaultExpression:
		return s.defaultMode == schemaDefaultSet
	case schemaAttributeFocusUniqueConstraint:
		return s.unique != nil && *s.unique
	default:
		return true
	}
}

func (s *schemaAttributeState) advanceFocus(delta int) {
	for range schemaAttributeFocusCount {
		s.focused = (s.focused + delta + schemaAttributeFocusCount) % schemaAttributeFocusCount
		if s.focusAvailable(s.focused) {
			return
		}
	}
}

func (s *schemaAttributeState) activateAttributeControl() {
	switch s.focused {
	case schemaAttributeFocusNullable:
		s.nullable = cycleNullable(s.nullable)
	case schemaAttributeFocusDefault:
		s.defaultMode = (s.defaultMode + 1) % 3
		if s.defaultMode == schemaDefaultSet {
			s.focused = schemaAttributeFocusDefaultExpression
			s.focusInput()
		}
	case schemaAttributeFocusUnique:
		s.unique = cycleUnique(s.unique)
	}
}

func (m *Model) activateSchemaAttributeControl() {
	state := &m.schemaAttributes
	state.err = nil
	if state.focused == schemaAttributeFocusUnique && state.current != nil && len(state.current.PrimaryKeyConstraints) > 0 {
		state.err = fmt.Errorf("UNIQUE is protected because this column is covered by primary key %s", strings.Join(state.current.PrimaryKeyConstraints, ", "))
		return
	}
	state.activateAttributeControl()
}

func (m *Model) selectNextSchemaAttributeColumn() tea.Cmd {
	state := &m.schemaAttributes
	if len(state.columns) == 0 || len(state.inputs) < 3 {
		state.err = fmt.Errorf("load a table and column target before selecting the next column")
		return nil
	}
	current := strings.TrimSpace(state.inputs[2].Value())
	for i, column := range state.columns {
		if column == current && i+1 < len(state.columns) {
			state.inputs[2].SetValue(state.columns[i+1])
			state.focused = schemaAttributeFocusColumn
			state.targetEditing = false
			state.notice = ""
			state.resetRequestedChanges()
			return m.startSchemaAttributeCurrentLoad()
		}
	}
	state.err = fmt.Errorf("%s is the last loaded column; press T to choose another target", current)
	return nil
}

func (s *schemaAttributeState) resetRequestedChanges() {
	s.nullable = nil
	s.unique = nil
	s.defaultMode = schemaDefaultUnchanged
	s.plan = nil
	s.err = nil
	if len(s.inputs) > 3 {
		s.inputs[3].SetValue("")
	}
	if len(s.inputs) > 4 {
		s.inputs[4].SetValue("")
	}
	if s.focused > schemaAttributeFocusColumn {
		s.focused = schemaAttributeFocusNullable
	}
	s.focusInput()
}

func (m Model) schemaAttributeActions() []schemaAttributeActionOption {
	state := m.schemaAttributes
	if state.current == nil {
		return nil
	}
	actions := []schemaAttributeActionOption{}
	if state.current.Nullable {
		actions = append(actions, schemaAttributeActionOption{Kind: schemaActionNullable, Label: "Set NOT NULL"})
	} else {
		actions = append(actions, schemaAttributeActionOption{Kind: schemaActionNullable, Label: "Allow NULL"})
	}
	actions = append(actions, schemaAttributeActionOption{Kind: schemaActionSetDefault, Label: "Set default expression…"})
	if state.current.Default != "" {
		actions = append(actions, schemaAttributeActionOption{Kind: schemaActionDropDefault, Label: "Drop default"})
	}
	if len(state.current.PrimaryKeyConstraints) == 0 {
		if len(state.current.UniqueConstraints) == 0 {
			actions = append(actions, schemaAttributeActionOption{Kind: schemaActionUnique, Label: "Add UNIQUE"})
		} else {
			actions = append(actions, schemaAttributeActionOption{Kind: schemaActionUnique, Label: "Drop UNIQUE"})
		}
	}
	if state.nullable != nil || state.unique != nil || state.defaultMode != schemaDefaultUnchanged {
		actions = append(actions, schemaAttributeActionOption{Kind: schemaActionClearDraft, Label: "Clear requested changes"})
	}
	return actions
}

func (m *Model) moveSchemaAttributeWorkspaceColumn(delta int) tea.Cmd {
	state := &m.schemaAttributes
	if len(state.columns) == 0 {
		return nil
	}
	next := min(max(0, state.columnIdx+delta), len(state.columns)-1)
	if next == state.columnIdx {
		return nil
	}
	state.columnIdx = next
	state.inputs[2].SetValue(state.columns[next])
	state.notice = ""
	state.actionMenu = false
	state.resetRequestedChanges()
	return m.startSchemaAttributeCurrentLoad()
}

func (m *Model) applySchemaAttributeAction() {
	state := &m.schemaAttributes
	actions := m.schemaAttributeActions()
	if state.actionIdx < 0 || state.actionIdx >= len(actions) || state.current == nil {
		return
	}
	state.err = nil
	switch actions[state.actionIdx].Kind {
	case schemaActionNullable:
		value := !state.current.Nullable
		state.nullable = &value
		state.focused = schemaAttributeFocusNullable
	case schemaActionSetDefault:
		state.defaultMode = schemaDefaultSet
		state.focused = schemaAttributeFocusDefaultExpression
		state.focusInput()
	case schemaActionDropDefault:
		state.defaultMode = schemaDefaultDrop
		state.focused = schemaAttributeFocusDefault
	case schemaActionUnique:
		value := len(state.current.UniqueConstraints) == 0
		state.unique = &value
		if value {
			state.focused = schemaAttributeFocusUniqueConstraint
			state.focusInput()
		} else {
			state.focused = schemaAttributeFocusUnique
		}
	case schemaActionClearDraft:
		state.resetRequestedChanges()
	}
	state.actionMenu = false
}

func cycleNullable(value *bool) *bool {
	if value == nil {
		falseValue := false
		return &falseValue
	}
	if !*value {
		trueValue := true
		return &trueValue
	}
	return nil
}

func cycleUnique(value *bool) *bool {
	if value == nil {
		trueValue := true
		return &trueValue
	}
	if *value {
		falseValue := false
		return &falseValue
	}
	return nil
}

func (m Model) schemaAttributeChange() (driver.ColumnAttributeChange, error) {
	state := m.schemaAttributes
	if len(state.inputs) != 5 {
		return driver.ColumnAttributeChange{}, fmt.Errorf("schema attribute form is not initialized")
	}
	change := driver.ColumnAttributeChange{
		Schema:     strings.TrimSpace(state.inputs[0].Value()),
		Table:      strings.TrimSpace(state.inputs[1].Value()),
		Column:     strings.TrimSpace(state.inputs[2].Value()),
		Nullable:   state.nullable,
		Unique:     state.unique,
		Constraint: strings.TrimSpace(state.inputs[4].Value()),
	}
	if change.Schema == "" || change.Table == "" || change.Column == "" {
		return driver.ColumnAttributeChange{}, fmt.Errorf("schema, table, and column are required")
	}
	if change.Unique != nil && state.current != nil && len(state.current.PrimaryKeyConstraints) > 0 {
		return driver.ColumnAttributeChange{}, fmt.Errorf("UNIQUE is protected because this column is covered by primary key %s", strings.Join(state.current.PrimaryKeyConstraints, ", "))
	}
	switch state.defaultMode {
	case schemaDefaultSet:
		value := strings.TrimSpace(state.inputs[3].Value())
		if value == "" {
			return driver.ColumnAttributeChange{}, fmt.Errorf("a PostgreSQL DEFAULT expression is required")
		}
		change.Default = &value
	case schemaDefaultDrop:
		change.DropDefault = true
	}
	if change.Nullable == nil && change.Unique == nil && change.Default == nil && !change.DropDefault {
		return driver.ColumnAttributeChange{}, fmt.Errorf("choose a NULL, DEFAULT, or UNIQUE change before preflight")
	}
	return change, nil
}

func (m *Model) startSchemaAttributePreflight() tea.Cmd {
	change, err := m.schemaAttributeChange()
	m.schemaAttributes.plan = nil
	m.schemaAttributes.batchPlan = nil
	m.schemaAttributes.err = err
	m.schemaAttributes.loading = err == nil
	m.schemaAttributes.offset = 0
	if err != nil {
		return nil
	}
	profile := m.result.Profile
	batch := m.schemaAttributeBatchChange(change)
	return func() tea.Msg {
		if profile.Driver != "postgres" {
			return schemaAttributePreflightLoadedMsg{err: fmt.Errorf("schema attributes are available only for PostgreSQL profiles")}
		}
		drv, err := driver.Get(profile.Driver)
		if err != nil {
			return schemaAttributePreflightLoadedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), schemaAttributeTimeout)
		defer cancel()
		if batch != nil {
			editor, ok := drv.(driver.ColumnAttributeBatchEditor)
			if !ok {
				return schemaAttributePreflightLoadedMsg{err: fmt.Errorf("driver %q does not support atomic field batches", profile.Driver)}
			}
			plan, err := editor.PreflightColumnAttributeBatch(ctx, profile, *batch)
			return schemaAttributePreflightLoadedMsg{batchPlan: plan, err: err}
		}
		editor, ok := drv.(driver.ColumnAttributeEditor)
		if !ok {
			return schemaAttributePreflightLoadedMsg{err: fmt.Errorf("driver %q does not support column attribute changes", profile.Driver)}
		}
		plan, err := editor.PreflightColumnAttributes(ctx, profile, change)
		return schemaAttributePreflightLoadedMsg{plan: plan, err: err}
	}
}

func (m Model) schemaAttributeBatchChange(change driver.ColumnAttributeChange) *driver.ColumnAttributeBatchChange {
	if len(m.schemaAttributes.selectedColumns) < 2 {
		return nil
	}
	columns := make([]string, 0, len(m.schemaAttributes.selectedColumns))
	for _, column := range m.schemaAttributes.columns {
		if m.schemaAttributes.selectedColumns[column] {
			columns = append(columns, column)
		}
	}
	if len(columns) < 2 {
		return nil
	}
	return &driver.ColumnAttributeBatchChange{Schema: change.Schema, Table: change.Table, Columns: columns, Nullable: change.Nullable, Default: change.Default, DropDefault: change.DropDefault, Unique: change.Unique}
}

func (m Model) updateSchemaAttributePreflight(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.updateSchemaAttributeScroll(msg) {
		return m, nil
	}
	switch msg.String() {
	case "r", "R":
		if !m.schemaAttributes.loading {
			return m, m.startSchemaAttributePreflight()
		}
	case "a", "A":
		if !m.schemaAttributes.loading && m.schemaAttributes.err == nil && (m.schemaAttributes.plan != nil || m.schemaAttributes.batchPlan != nil) {
			m.step = stepSchemaAttributeApplying
			return m, m.startSchemaAttributeApply()
		}
	case "esc":
		m.step = stepSchemaAttributeForm
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateSchemaAttributeConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.updateSchemaAttributeScroll(msg) {
		return m, nil
	}
	switch msg.String() {
	case "enter", "y":
		m.step = stepSchemaAttributeApplying
		return m, m.startSchemaAttributeApply()
	case "esc", "n":
		m.step = stepSchemaAttributePreflight
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) startSchemaAttributeApply() tea.Cmd {
	if m.schemaAttributes.plan == nil && m.schemaAttributes.batchPlan == nil {
		m.schemaAttributes.applying = false
		m.schemaAttributes.err = fmt.Errorf("a successful preflight plan is required before apply")
		return nil
	}
	m.schemaAttributes.applying = true
	m.schemaAttributes.err = nil
	profile := m.result.Profile
	plan := m.schemaAttributes.plan
	batchPlan := m.schemaAttributes.batchPlan
	return func() tea.Msg {
		if profile.Driver != "postgres" {
			return schemaAttributeApplyFinishedMsg{err: fmt.Errorf("schema attributes are available only for PostgreSQL profiles")}
		}
		drv, err := driver.Get(profile.Driver)
		if err != nil {
			return schemaAttributeApplyFinishedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), schemaAttributeTimeout)
		defer cancel()
		if batchPlan != nil {
			editor, ok := drv.(driver.ColumnAttributeBatchEditor)
			if !ok {
				return schemaAttributeApplyFinishedMsg{err: fmt.Errorf("driver %q does not support atomic field batches", profile.Driver)}
			}
			applied, err := editor.ApplyColumnAttributeBatch(ctx, profile, batchPlan.Change)
			return schemaAttributeApplyFinishedMsg{batchPlan: applied, err: err}
		}
		editor, ok := drv.(driver.ColumnAttributeEditor)
		if !ok {
			return schemaAttributeApplyFinishedMsg{err: fmt.Errorf("driver %q does not support column attribute changes", profile.Driver)}
		}
		applied, err := editor.ApplyColumnAttributes(ctx, profile, plan.Change)
		return schemaAttributeApplyFinishedMsg{plan: applied, err: err}
	}
}

func (m Model) updateSchemaAttributeResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	default:
		m.step = stepSelectMode
	}
	return m, nil
}

func (m *Model) updateSchemaAttributeScroll(msg tea.KeyMsg) bool {
	if m.schemaAttributeMaxOffset() == 0 {
		return false
	}
	offset := m.schemaAttributes.offset
	switch msg.String() {
	case "up", "k", "K":
		offset--
	case "down", "j", "J":
		offset++
	case "pgup":
		offset -= m.schemaAttributeVisibleHeight()
	case "pgdown":
		offset += m.schemaAttributeVisibleHeight()
	case "home":
		offset = 0
	case "end":
		offset = m.schemaAttributeMaxOffset()
	default:
		return false
	}
	m.schemaAttributes.offset = clampScrollOffset(offset, len(m.schemaAttributeContentLines()), m.schemaAttributeVisibleHeight())
	return true
}

func (m Model) schemaAttributeControlRows() string {
	state := m.schemaAttributes
	rows := []struct {
		focus int
		label string
		value string
	}{
		{schemaAttributeFocusNullable, "Nullable", nullableLabel(state.nullable)},
		{schemaAttributeFocusDefault, "Default", defaultLabel(state.defaultMode)},
		{schemaAttributeFocusUnique, "Unique", uniqueLabel(state.unique)},
	}
	var body strings.Builder
	for i, row := range rows {
		marker := "  "
		if state.focused == row.focus {
			marker = "> "
		}
		body.WriteString(marker + row.label + ": " + row.value)
		if i < len(rows)-1 {
			body.WriteByte('\n')
		}
	}
	return body.String()
}

func (m Model) schemaAttributeTargetBody() string {
	state := m.schemaAttributes
	if state.workspace && len(state.inputs) >= 2 {
		table := strings.TrimSpace(state.inputs[0].Value()) + "." + strings.TrimSpace(state.inputs[1].Value())
		return renderCard(m.width, "Table Workspace", valueStyle.Render(table)+"  "+mutedStyle.Render("[T] change table"))
	}
	if !state.targetEditing && len(state.inputs) >= 3 {
		target := strings.Join([]string{
			strings.TrimSpace(state.inputs[0].Value()),
			strings.TrimSpace(state.inputs[1].Value()),
			strings.TrimSpace(state.inputs[2].Value()),
		}, ".")
		return renderCard(m.width, "Target", valueStyle.Render(target)+"  "+mutedStyle.Render("[T] change target · [N] next column"))
	}
	labels := []string{"Schema", "Table"}
	var rows strings.Builder
	for i, label := range labels {
		marker := "  "
		if i == state.focused {
			marker = "> "
		}
		rows.WriteString(marker + label + ": " + state.inputs[i].View())
		if i < len(labels)-1 {
			rows.WriteByte('\n')
		}
	}
	return renderCard(m.width, "Change Target", rows.String())
}

func (m Model) schemaAttributeWorkspaceColumnsBody() string {
	state := m.schemaAttributes
	if !state.workspace {
		return ""
	}
	if state.selectorLoading {
		return renderCard(m.width, "Columns", mutedStyle.Render("Loading columns…"))
	}
	if state.selectorErr != "" {
		return renderCard(m.width, "Columns", warningStyle.Render(state.selectorErr))
	}
	if len(state.columns) == 0 {
		return renderCard(m.width, "Columns", mutedStyle.Render("No columns found; press T to choose another table."))
	}
	start := max(0, state.columnIdx-3)
	end := min(len(state.columns), start+7)
	start = max(0, end-7)
	rows := make([]string, 0, end-start+1)
	for i := start; i < end; i++ {
		marker := "  "
		if i == state.columnIdx {
			marker = "> "
		}
		checked := "[ ] "
		if state.selectedColumns[state.columns[i]] {
			checked = "[x] "
		}
		rows = append(rows, marker+checked+state.columns[i])
	}
	if len(state.columns) > len(rows) {
		rows = append(rows, mutedStyle.Render(fmt.Sprintf("%d–%d / %d", start+1, end, len(state.columns))))
	}
	return renderCard(m.width, fmt.Sprintf("Fields · %d selected", len(state.selectedColumns)), strings.Join(rows, "\n"))
}

func (m Model) schemaAttributeActionMenuBody() string {
	state := m.schemaAttributes
	if !state.workspace || !state.actionMenu {
		return ""
	}
	actions := m.schemaAttributeActions()
	if len(actions) == 0 {
		return renderCard(m.width, "Change Action", mutedStyle.Render("Current attributes are still loading."))
	}
	rows := make([]string, 0, len(actions))
	for i, action := range actions {
		marker := "  "
		if i == state.actionIdx {
			marker = "> "
		}
		rows = append(rows, marker+action.Label)
	}
	return renderCard(m.width, "Change "+strings.TrimSpace(state.inputs[2].Value()), strings.Join(rows, "\n"))
}

func (m Model) schemaAttributeChangeRows() string {
	state := m.schemaAttributes
	if state.currentLoading {
		return renderCard(m.width, "Attribute Changes", mutedStyle.Render("Loading current PostgreSQL column attributes…"))
	}
	if state.currentErr != "" {
		return renderCard(m.width, "Attribute Changes", warningStyle.Render(state.currentErr+"; changes remain available once the target is known."))
	}
	if state.current == nil {
		return renderCard(m.width, "Attribute Changes", mutedStyle.Render("Choose a target column to load current values."))
	}
	defaultCurrent := state.current.Default
	if defaultCurrent == "" {
		defaultCurrent = "none"
	}
	uniqueCurrent := "none"
	if len(state.current.UniqueConstraints) > 0 {
		uniqueCurrent = strings.Join(state.current.UniqueConstraints, ", ")
	}
	rows := []struct {
		focus     int
		attribute string
		current   string
		requested string
	}{
		{schemaAttributeFocusNullable, "Nullable", nullableCurrentLabel(state.current.Nullable), nullableLabel(state.nullable)},
		{schemaAttributeFocusDefault, "Default", defaultCurrent, defaultLabel(state.defaultMode)},
		{schemaAttributeFocusUnique, "Unique", uniqueCurrent, uniqueLabel(state.unique)},
	}
	var body strings.Builder
	body.WriteString(mutedStyle.Render("Attribute        Current                         Requested"))
	for _, row := range rows {
		marker := "  "
		if state.focused == row.focus {
			marker = "> "
		}
		requested := row.requested
		current := row.current
		if row.focus == schemaAttributeFocusUnique && len(state.current.PrimaryKeyConstraints) > 0 {
			requested = "PROTECTED"
			current = "covered by primary key " + strings.Join(state.current.PrimaryKeyConstraints, ", ")
		}
		body.WriteString("\n" + marker + fmt.Sprintf("%-16s %-38s %s", row.attribute, truncateMiddle(current, 38), requested))
	}
	if len(state.current.PrimaryKeyConstraints) > 0 {
		body.WriteString("\n" + warningStyle.Render("Primary key: "+strings.Join(state.current.PrimaryKeyConstraints, ", ")+" · UNIQUE changes are protected in v1."))
	}
	title := "Attribute Changes"
	if state.workspace && len(state.inputs) >= 3 {
		title = "Selected Column · " + strings.TrimSpace(state.inputs[2].Value())
	}
	return renderCard(m.width, title, body.String())
}

func (m Model) viewSchemaAttributeForm() string {
	state := m.schemaAttributes
	if state.guidedStep != schemaGuidedAdvanced {
		return m.viewGuidedSchemaAttributeForm()
	}
	body := m.schemaAttributeTargetBody()
	if state.targetEditing {
		if suggestions := m.schemaAttributeSuggestionBody(); suggestions != "" {
			body += "\n\n" + suggestions
		}
	}
	if workspace := m.schemaAttributeWorkspaceColumnsBody(); workspace != "" {
		body += "\n\n" + workspace
	}
	body += "\n\n" + m.schemaAttributeChangeRows()
	if actionMenu := m.schemaAttributeActionMenuBody(); actionMenu != "" {
		body += "\n\n" + actionMenu
	}
	if state.defaultMode == schemaDefaultSet {
		marker := "  "
		if state.focused == schemaAttributeFocusDefaultExpression {
			marker = "> "
		}
		body += "\n\n" + renderCard(m.width, "Default Value", marker+"PostgreSQL SQL expression: "+state.inputs[3].View())
	}
	if state.unique != nil && *state.unique {
		marker := "  "
		if state.focused == schemaAttributeFocusUniqueConstraint {
			marker = "> "
		}
		body += "\n\n" + renderCard(m.width, "UNIQUE Constraint", marker+"Constraint name (optional): "+state.inputs[4].View())
	}
	if state.err != nil {
		body += "\n\n" + renderCard(m.width, "Form Error", errorStyle.Render(state.err.Error()))
	}
	if state.notice != "" {
		body += "\n\n" + renderCard(m.width, "Updated", subtextStyle.Render(state.notice))
	}
	if state.workspace {
		body += "\n\n" + mutedStyle.Render("↑/↓ selects a column. Enter opens a concrete change action. R reviews requested changes and runs read-only preflight.")
	} else {
		body += "\n\n" + mutedStyle.Render("Tab selects a target field. R reviews requested changes and runs read-only preflight.")
	}
	lines := strings.Split(body, "\n")
	header := renderAppHeader("Edit Column Attributes", "PostgreSQL NULL, DEFAULT, and single-column UNIQUE • profile "+m.result.Profile.Name, m.width)
	viewport := newFixedDocumentViewport(lines, m.height, header, schemaAttributeFrameGaps, state.offset, func(viewport scrollViewport) string {
		hints := []keyHint{{Key: "T", Label: "table"}, {Key: "R", Label: "review"}}
		if state.workspace {
			if state.actionMenu {
				hints = append(hints, keyHint{Key: "↑/↓", Label: "action"}, keyHint{Key: "Enter", Label: "choose"})
			} else {
				hints = append(hints, keyHint{Key: "↑/↓", Label: "field"}, keyHint{Key: "Space", Label: "select"}, keyHint{Key: "Enter", Label: "change"})
			}
		} else {
			hints = append(hints, keyHint{Key: "Tab", Label: "field"})
		}
		if state.targetEditing {
			hints = append(hints, keyHint{Key: "↑/↓", Label: "suggestion"})
		}
		if viewport.Total > viewport.Visible {
			hints = append(hints, keyHint{Key: fmt.Sprintf("%d–%d / %d", viewport.Offset+1, viewport.End, viewport.Total), Label: "position"})
		}
		return renderCommandBar(m.width, append(hints, keyHint{Key: "Esc", Label: "menu", Danger: true}))
	})
	return viewport.Render(lines)
}

func (m Model) viewSchemaAttributePreflight() string {
	lines := m.schemaAttributeContentLines()
	return m.schemaAttributeViewport("Review Schema Change", "Read-only validation and SQL preview before PostgreSQL DDL", lines, false).Render(lines)
}

func (m Model) viewSchemaAttributeConfirm() string {
	lines := m.schemaAttributeContentLines()
	return m.schemaAttributeViewport("Confirm Column Change", "Review locked preflight evidence before applying DDL", lines, true).Render(lines)
}

func (m Model) viewSchemaAttributeApplying() string {
	body := renderCard(m.width, "Applying Column Change", mutedStyle.Render("Re-running preflight while holding ACCESS EXCLUSIVE table lock…"))
	return renderScreenFrame(m.width, "Applying Column Change", "PostgreSQL transaction in progress", body, []keyHint{{Key: "Please wait", Label: "locked preflight and DDL"}})
}

func (m Model) viewSchemaAttributeResult() string {
	state := m.schemaAttributes
	if state.err != nil {
		return renderScreenFrame(m.width, "Column Change Failed", "Transaction was rolled back", renderCard(m.width, "Error", errorStyle.Render(state.err.Error())), []keyHint{{Key: "Any key", Label: "back to operations"}, {Key: "Q/Esc", Label: "quit", Danger: true}})
	}
	body := ""
	if state.batchPlan != nil {
		body = schemaAttributeBatchPlanBody(state.batchPlan)
	} else if state.plan != nil {
		body = strings.Join(state.plan.Statements, ";\n") + ";"
	}
	return renderScreenFrame(m.width, "Column Change Applied", "Locked preflight and PostgreSQL DDL completed", renderCard(m.width, "Applied SQL", body), []keyHint{{Key: "Any key", Label: "back to operations"}, {Key: "Q/Esc", Label: "quit", Danger: true}})
}

func (m Model) schemaAttributeContentLines() []string {
	state := m.schemaAttributes
	var body strings.Builder
	body.WriteString(renderProfileSummaryCard(m.width, "Schema Profile", m.result.Profile.Name, m.result.Profile.Driver, m.result.Profile.Host, m.result.Profile.Port, m.result.Profile.Database, m.result.Profile.User))
	body.WriteString("\n\n")
	switch {
	case state.loading:
		body.WriteString(renderCard(m.width, "Read-only Preflight", mutedStyle.Render("Checking NULLs, duplicate non-NULL values, and UNIQUE constraints…")))
	case state.err != nil:
		body.WriteString(renderCard(m.width, "Preflight Blocked", errorStyle.Render(state.err.Error())))
	case state.batchPlan != nil:
		body.WriteString(renderCard(m.width, "Batch Preflight Report", schemaAttributeBatchPlanBody(state.batchPlan)))
	case state.plan == nil:
		body.WriteString(renderCard(m.width, "Read-only Preflight", mutedStyle.Render("Press R to collect a new preflight report.")))
	default:
		body.WriteString(renderCard(m.width, "Preflight Report", schemaAttributePlanBody(state.plan)))
	}
	body.WriteString("\n\n")
	body.WriteString(renderCard(m.width, "Safety Boundary", warningStyle.Render("Preview is read-only. Apply repeats this preflight inside a transaction after acquiring ACCESS EXCLUSIVE on the target table.")))
	return strings.Split(body.String(), "\n")
}

func (m Model) schemaAttributeViewport(title, subtitle string, lines []string, confirm bool) fixedDocumentViewport {
	header := renderAppHeader(title, subtitle, m.width)
	return newFixedDocumentViewport(lines, m.height, header, schemaAttributeFrameGaps, m.schemaAttributes.offset, func(viewport scrollViewport) string {
		hints := m.schemaAttributeFooterHints(viewport, confirm)
		return renderCommandBar(m.width, hints)
	})
}

func (m Model) schemaAttributeVisibleHeight() int {
	return m.schemaAttributeViewport("Schema Change Preflight", "", m.schemaAttributeContentLines(), false).Visible
}

func (m Model) schemaAttributeMaxOffset() int {
	return scrollMaxOffset(len(m.schemaAttributeContentLines()), m.schemaAttributeVisibleHeight())
}

func (m *Model) clampSchemaAttributeOffset() {
	m.schemaAttributes.offset = clampScrollOffset(m.schemaAttributes.offset, len(m.schemaAttributeContentLines()), m.schemaAttributeVisibleHeight())
}

func (m Model) schemaAttributeFooterHints(viewport scrollViewport, confirm bool) []keyHint {
	var hints []keyHint
	if confirm {
		hints = append(hints, keyHint{Key: "Enter/Y", Label: "apply", Danger: true})
	} else {
		hints = append(hints, keyHint{Key: "R", Label: "refresh"})
		if (m.schemaAttributes.plan != nil || m.schemaAttributes.batchPlan != nil) && m.schemaAttributes.err == nil && !m.schemaAttributes.loading {
			hints = append(hints, keyHint{Key: "A", Label: "apply", Danger: true})
		}
	}
	if viewport.Total > viewport.Visible {
		hints = append(hints, keyHint{Key: fmt.Sprintf("%d–%d / %d", viewport.Offset+1, viewport.End, viewport.Total), Label: "position"}, keyHint{Key: "↑/k", Label: "up"}, keyHint{Key: "↓/j", Label: "down"}, keyHint{Key: "PgUp/PgDn", Label: "page"}, keyHint{Key: "Home/End", Label: "bounds"})
	}
	back := "form"
	if confirm {
		back = "preflight"
	}
	return append(hints, keyHint{Key: "Esc", Label: back, Danger: true}, keyHint{Key: "Q", Label: "quit", Danger: true})
}

func schemaAttributePlanBody(plan *driver.ColumnAttributePlan) string {
	rows := []kvRow{{Label: "Target", Value: plan.Change.Schema + "." + plan.Change.Table + "." + plan.Change.Column}}
	if plan.NullRows > 0 {
		rows = append(rows, kvRow{Label: "NULL rows", Value: fmt.Sprintf("%d", plan.NullRows), Kind: badgeWarning})
	}
	if plan.DuplicateGroups > 0 {
		rows = append(rows, kvRow{Label: "Duplicate groups", Value: fmt.Sprintf("%d (%d rows)", plan.DuplicateGroups, plan.DuplicateRows), Kind: badgeWarning})
	}
	if len(plan.ExistingUniqueConstraints) > 0 {
		rows = append(rows, kvRow{Label: "Existing UNIQUE", Value: strings.Join(plan.ExistingUniqueConstraints, ", ")})
	}
	body := renderKeyValueGrid(rows, 18) + "\n\nSQL:\n" + strings.Join(plan.Statements, ";\n") + ";"
	if len(plan.Warnings) > 0 {
		body += "\n\nWarnings:\n" + strings.Join(plan.Warnings, "\n")
	}
	return body
}

func schemaAttributeBatchPlanBody(plan *driver.ColumnAttributeBatchPlan) string {
	var body strings.Builder
	body.WriteString(fmt.Sprintf("%d selected fields · one table lock · all-or-nothing\n", len(plan.Plans)))
	for index, columnPlan := range plan.Plans {
		if index > 0 {
			body.WriteString("\n\n")
		}
		body.WriteString(schemaAttributePlanBody(&columnPlan))
	}
	return body.String()
}

func nullableLabel(value *bool) string {
	if value == nil {
		return "KEEP"
	}
	if *value {
		return "ALLOW NULL"
	}
	return "SET NOT NULL"
}

func uniqueLabel(value *bool) string {
	if value == nil {
		return "KEEP"
	}
	if *value {
		return "ADD UNIQUE"
	}
	return "DROP UNIQUE"
}

func defaultLabel(mode schemaDefaultMode) string {
	switch mode {
	case schemaDefaultSet:
		return "SET EXPRESSION"
	case schemaDefaultDrop:
		return "DROP DEFAULT"
	default:
		return "KEEP"
	}
}

func attributeBadge(value *bool) badgeKind {
	if value == nil {
		return badgeNeutral
	}
	if *value {
		return badgeSuccess
	}
	return badgeWarning
}

func defaultBadge(mode schemaDefaultMode) badgeKind {
	if mode == schemaDefaultUnchanged {
		return badgeNeutral
	}
	return badgeWarning
}
