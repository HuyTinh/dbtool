package tui

import (
	"strings"
	"testing"

	"dbtool/internal/config"
	"dbtool/internal/driver"
	_ "dbtool/internal/driver/postgres"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestSchemaAttributeModeRoutesProfileToEditorForm(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{
		"local": {Name: "local", Driver: "postgres", Host: "localhost", Port: 5432, Database: "app"},
	}}, RestoreSettings{})

	updated, _ := m.updateSelectMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)
	if m.result.Mode != ModeSchemaAttributes || m.step != stepSelectProfile {
		t.Fatalf("mode/step = %v/%v, want schema attributes/profile selector", m.result.Mode, m.step)
	}

	updated, _ = m.updateProfileSelector(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.step != stepSchemaAttributeForm {
		t.Fatalf("step = %v, want schema attribute form", m.step)
	}
	if got := m.schemaAttributes.inputs[0].Value(); got != "public" {
		t.Fatalf("schema input = %q, want public", got)
	}
}

func TestSchemaAttributeFormBuildsExplicitChanges(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.startSchemaAttributeForm()
	m.schemaAttributes.guidedStep = schemaGuidedAdvanced
	m.schemaAttributes.inputs[1].SetValue("users")
	m.schemaAttributes.inputs[2].SetValue("email")
	m.schemaAttributes.targetEditing = false
	m.schemaAttributes.workspace = true
	m.schemaAttributes.focused = schemaAttributeFocusNullable
	updated, _ := m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	m.schemaAttributes.focused = schemaAttributeFocusUnique
	updated, _ = m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)

	change, err := m.schemaAttributeChange()
	if err != nil {
		t.Fatalf("schemaAttributeChange: %v", err)
	}
	if change.Nullable == nil || *change.Nullable || change.Unique == nil || !*change.Unique {
		t.Fatalf("change = %#v, want NOT NULL plus UNIQUE", change)
	}
}

func TestSchemaAttributeReviewAppliesFromTheReviewedPlan(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 100
	m.height = 80
	m.result.Profile = config.Profile{Name: "local", Driver: "postgres", Database: "app"}
	m.step = stepSchemaAttributePreflight
	m.schemaAttributes.plan = &driver.ColumnAttributePlan{
		Change:     driver.ColumnAttributeChange{Schema: "public", Table: "users", Column: "email"},
		Statements: []string{`ALTER TABLE "public"."users" ALTER COLUMN "email" SET NOT NULL`},
	}

	if view := m.View(); !strings.Contains(view, "Review Schema Change") || !strings.Contains(view, "SET NOT NULL") || !strings.Contains(view, "apply") {
		t.Fatalf("preflight view missing review markers:\n%s", view)
	}
	updated, cmd := m.updateSchemaAttributePreflight(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	got := updated.(Model)
	if got.step != stepSchemaAttributeApplying || !got.schemaAttributes.applying || cmd == nil {
		t.Fatalf("step/applying/cmd = %v/%v/%v, want applying/true/non-nil", got.step, got.schemaAttributes.applying, cmd != nil)
	}
}

func TestSchemaAttributeConfirmationStartsAsyncApply(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 100
	m.height = 80
	m.result.Profile = config.Profile{Name: "local", Driver: "postgres", Database: "app"}
	m.step = stepSchemaAttributeConfirm
	m.schemaAttributes.plan = &driver.ColumnAttributePlan{
		Change:     driver.ColumnAttributeChange{Schema: "public", Table: "users", Column: "email"},
		Statements: []string{`ALTER TABLE "public"."users" ADD CONSTRAINT "uq_users_email" UNIQUE ("email")`},
	}

	if view := m.View(); !strings.Contains(view, "ACCESS") || !strings.Contains(view, "EXCLUSIVE") || !strings.Contains(view, "apply") {
		t.Fatalf("confirmation view missing lock/apply warning:\n%s", view)
	}
	updated, cmd := m.updateSchemaAttributeConfirm(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.step != stepSchemaAttributeApplying || !got.schemaAttributes.applying || cmd == nil {
		t.Fatalf("step/applying/cmd = %v/%v/%v, want applying/true/non-nil", got.step, got.schemaAttributes.applying, cmd != nil)
	}
}

func TestSchemaAttributeAsyncMessagesKeepPreflightAndApplySeparated(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepSchemaAttributePreflight
	m.schemaAttributes.loading = true
	plan := &driver.ColumnAttributePlan{Change: driver.ColumnAttributeChange{Schema: "public", Table: "users", Column: "email"}}

	updated, _ := m.Update(schemaAttributePreflightLoadedMsg{plan: plan})
	m = updated.(Model)
	if m.schemaAttributes.loading || m.schemaAttributes.plan != plan || m.schemaAttributes.err != nil || m.step != stepSchemaAttributePreflight {
		t.Fatalf("preflight message state = %#v, want loaded plan while remaining on preflight", m.schemaAttributes)
	}

	m.step = stepSchemaAttributeApplying
	m.schemaAttributes.applying = true
	updated, _ = m.Update(schemaAttributeApplyFinishedMsg{plan: plan})
	m = updated.(Model)
	if m.schemaAttributes.applying || m.schemaAttributes.plan != plan || m.step != stepSchemaAttributeResult {
		t.Fatalf("apply message state = %#v / step %v, want completed result", m.schemaAttributes, m.step)
	}
}

func TestSchemaAttributeFormFitsTypicalTerminal(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 100
	m.height = 24
	m.result.Profile = config.Profile{Name: "local", Driver: "postgres", Database: "app"}
	m.startSchemaAttributeForm()
	m.schemaAttributes.guidedStep = schemaGuidedAdvanced
	m.step = stepSchemaAttributeForm

	if got := lipgloss.Height(m.View()); got > m.height {
		t.Fatalf("schema attribute form height = %d, exceeds terminal height %d", got, m.height)
	}
}

func TestSchemaAttributeTypeAheadSelectsSchemaTableAndColumn(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 100
	m.result.Profile = config.Profile{Name: "local", Driver: "postgres", Database: "app"}
	m.startSchemaAttributeForm()
	m.schemaAttributes.guidedStep = schemaGuidedAdvanced
	m.step = stepSchemaAttributeForm
	m.schemaAttributes.selectorLoading = false
	m.schemaAttributes.selectorErr = ""
	m.schemaAttributes.schemas = []string{"archive", "public"}
	m.schemaAttributes.inputs[0].SetValue("pub")

	if view := m.View(); !strings.Contains(view, "Schema suggestions") || !strings.Contains(view, "public") {
		t.Fatalf("schema type-ahead view missing filtered suggestion:\n%s", view)
	}
	updated, _ := m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if got := m.schemaAttributes.inputs[0].Value(); got != "public" {
		t.Fatalf("schema selection = %q, want public", got)
	}

	updated, _ = m.Update(schemaAttributeTablesLoadedMsg{tables: []driver.CatalogTable{{Schema: "public", Name: "users"}}})
	m = updated.(Model)
	updated, cmd := m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.schemaAttributes.focused != 1 || cmd != nil || !strings.Contains(m.View(), "Table suggestions") || !strings.Contains(m.View(), "users") {
		t.Fatalf("table selector did not retain loaded suggestions:\n%s", m.View())
	}

	updated, _ = m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if got := m.schemaAttributes.inputs[1].Value(); got != "users" {
		t.Fatalf("table selection = %q, want users", got)
	}

	updated, cmd = m.Update(schemaAttributeColumnsLoadedMsg{columns: []string{"email", "id"}})
	m = updated.(Model)
	m.schemaAttributes.targetEditing = false
	m.schemaAttributes.workspace = true
	m.schemaAttributes.inputs[2].SetValue("email")
	if !m.schemaAttributes.workspace || m.schemaAttributes.inputs[2].Value() != "email" || !strings.Contains(m.View(), "Table Workspace") || !strings.Contains(m.View(), "Fields") {
		t.Fatalf("table workspace did not start with first column selected:\n%s", m.View())
	}
}

func TestSchemaAttributeWorkspaceUsesConcreteActionMenu(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 100
	m.height = 80
	m.startSchemaAttributeForm()
	m.schemaAttributes.guidedStep = schemaGuidedAdvanced
	m.step = stepSchemaAttributeForm
	m.schemaAttributes.inputs[1].SetValue("users")
	m.schemaAttributes.inputs[2].SetValue("email")
	m.schemaAttributes.columns = []string{"email", "id"}
	m.schemaAttributes.targetEditing = false
	m.schemaAttributes.workspace = true
	m.schemaAttributes.current = &driver.ColumnAttributeMetadata{Nullable: false}

	updated, _ := m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.schemaAttributes.actionMenu || !strings.Contains(m.View(), "Change email") || !strings.Contains(m.View(), "Allow NULL") {
		t.Fatalf("workspace action menu missing expected action:\n%s", m.View())
	}
	updated, _ = m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.schemaAttributes.actionMenu || m.schemaAttributes.nullable == nil || !*m.schemaAttributes.nullable {
		t.Fatalf("action did not create requested nullable change: %#v", m.schemaAttributes)
	}
}

func TestSchemaAttributeRendersCurrentValuesAfterColumnMetadataLoads(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 100
	m.height = 80
	m.startSchemaAttributeForm()
	m.schemaAttributes.guidedStep = schemaGuidedAdvanced
	m.step = stepSchemaAttributeForm
	m.schemaAttributes.inputs[1].SetValue("users")
	m.schemaAttributes.inputs[2].SetValue("id")
	m.schemaAttributes.targetEditing = false

	updated, _ := m.Update(schemaAttributeCurrentLoadedMsg{schema: "public", table: "users", column: "id", metadata: &driver.ColumnAttributeMetadata{
		Nullable:          false,
		Default:           "nextval('users_id_seq'::regclass)",
		UniqueConstraints: []string{"users_id_key"},
	}})
	m = updated.(Model)
	view := m.View()
	for _, want := range []string{"Attribute Changes", "NOT NULL", "nextval('users_id_seq'::regclass)", "users_id_key", "KEEP"} {
		if !strings.Contains(view, want) {
			t.Fatalf("current metadata view missing %q:\n%s", want, view)
		}
	}
}

func TestSchemaAttributeAcceptingColumnStartsCurrentMetadataLoad(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "local", Driver: "postgres", Database: "app"}
	m.startSchemaAttributeForm()
	m.schemaAttributes.guidedStep = schemaGuidedAdvanced
	m.schemaAttributes.inputs[1].SetValue("users")
	m.schemaAttributes.inputs[2].SetValue("id")
	m.schemaAttributes.focused = schemaAttributeFocusColumn
	m.schemaAttributes.columnSchema = "public"
	m.schemaAttributes.columnTable = "users"
	m.schemaAttributes.columns = []string{"id"}

	updated, cmd := m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if cmd == nil || !got.schemaAttributes.currentLoading || got.schemaAttributes.current != nil {
		t.Fatalf("current loading state = cmd:%v loading:%v metadata:%#v, want non-nil/true/nil", cmd != nil, got.schemaAttributes.currentLoading, got.schemaAttributes.current)
	}
}

func TestSchemaAttributeSuccessfulApplyStaysInEditorAndRefreshesCurrentState(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "local", Driver: "postgres", Database: "app"}
	m.startSchemaAttributeForm()
	m.schemaAttributes.guidedStep = schemaGuidedAdvanced
	m.step = stepSchemaAttributeApplying
	m.schemaAttributes.inputs[1].SetValue("users")
	m.schemaAttributes.inputs[2].SetValue("email")
	falseValue := false
	m.schemaAttributes.nullable = &falseValue
	m.schemaAttributes.defaultMode = schemaDefaultDrop
	m.schemaAttributes.unique = &falseValue
	m.schemaAttributes.inputs[3].SetValue("unused")
	m.schemaAttributes.inputs[4].SetValue("unused")

	updated, cmd := m.Update(schemaAttributeApplyFinishedMsg{plan: &driver.ColumnAttributePlan{Change: driver.ColumnAttributeChange{Schema: "public", Table: "users", Column: "email"}}})
	m = updated.(Model)
	if m.step != stepSchemaAttributeForm || cmd == nil || !m.schemaAttributes.currentLoading {
		t.Fatalf("step/cmd/currentLoading = %v/%v/%v, want form/non-nil/true", m.step, cmd != nil, m.schemaAttributes.currentLoading)
	}
	if m.schemaAttributes.nullable != nil || m.schemaAttributes.unique != nil || m.schemaAttributes.defaultMode != schemaDefaultUnchanged || m.schemaAttributes.inputs[3].Value() != "" || m.schemaAttributes.inputs[4].Value() != "" {
		t.Fatalf("requested changes were not reset: %#v", m.schemaAttributes)
	}
	if !strings.Contains(m.schemaAttributes.notice, "Applied successfully") {
		t.Fatalf("notice = %q, want applied confirmation", m.schemaAttributes.notice)
	}
}

func TestSchemaAttributeNextColumnPreservesTableAndResetsRequestedChanges(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "local", Driver: "postgres", Database: "app"}
	m.startSchemaAttributeForm()
	m.schemaAttributes.guidedStep = schemaGuidedAdvanced
	m.schemaAttributes.inputs[0].SetValue("public")
	m.schemaAttributes.inputs[1].SetValue("users")
	m.schemaAttributes.inputs[2].SetValue("email")
	m.schemaAttributes.columns = []string{"email", "id"}
	falseValue := false
	m.schemaAttributes.nullable = &falseValue
	m.schemaAttributes.defaultMode = schemaDefaultDrop

	updated, cmd := m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)
	if cmd == nil || m.schemaAttributes.inputs[0].Value() != "public" || m.schemaAttributes.inputs[1].Value() != "users" || m.schemaAttributes.inputs[2].Value() != "id" || !m.schemaAttributes.currentLoading {
		t.Fatalf("next column state = schema:%q table:%q column:%q cmd:%v loading:%v", m.schemaAttributes.inputs[0].Value(), m.schemaAttributes.inputs[1].Value(), m.schemaAttributes.inputs[2].Value(), cmd != nil, m.schemaAttributes.currentLoading)
	}
	if m.schemaAttributes.nullable != nil || m.schemaAttributes.defaultMode != schemaDefaultUnchanged {
		t.Fatalf("next column retained requested changes: %#v", m.schemaAttributes)
	}
}

func TestGuidedSchemaFlowChoosesIntentSearchesOneFieldAndBuildsChange(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.width = 100
	m.step = stepSchemaAttributeForm
	m.startSchemaAttributeForm()
	if !strings.Contains(m.View(), "Find fields to change") {
		t.Fatalf("guided search view missing:\n%s", m.View())
	}
	m.schemaAttributes.search.SetValue("customer email")
	updated, _ := m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.schemaAttributes.guidedStep != schemaGuidedChooseField {
		t.Fatalf("guided step = %v, want field results", m.schemaAttributes.guidedStep)
	}
	updated, _ = m.Update(schemaAttributeSearchLoadedMsg{results: []driver.CatalogColumnSearchResult{{Schema: "crm", Table: "customers", Column: "email", Type: "text"}}})
	m = updated.(Model)
	updated, _ = m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.schemaAttributes.guidedStep != schemaGuidedAdvanced || !m.schemaAttributes.workspace || !m.schemaAttributes.selectedColumns["email"] || m.schemaAttributes.inputs[0].Value() != "crm" || m.schemaAttributes.inputs[1].Value() != "customers" || m.schemaAttributes.inputs[2].Value() != "email" {
		t.Fatalf("guided selected target = %#v", m.schemaAttributes)
	}
}

func TestSchemaWorkspaceSpaceSelectsMultipleFields(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepSchemaAttributeForm
	m.startSchemaAttributeForm()
	m.schemaAttributes.guidedStep = schemaGuidedAdvanced
	m.schemaAttributes.workspace = true
	m.schemaAttributes.columns = []string{"email", "alternate_email"}
	m.schemaAttributes.columnIdx = 1
	m.schemaAttributes.selectedColumns = map[string]bool{"email": true}

	updated, _ := m.updateSchemaAttributeForm(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if !m.schemaAttributes.selectedColumns["email"] || !m.schemaAttributes.selectedColumns["alternate_email"] {
		t.Fatalf("selected fields = %#v", m.schemaAttributes.selectedColumns)
	}
	if !strings.Contains(m.View(), "Fields · 2 selected") {
		t.Fatalf("workspace selection count missing:\n%s", m.View())
	}
}
