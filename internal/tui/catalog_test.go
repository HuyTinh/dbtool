package tui

import (
	"reflect"
	"testing"

	"dbtool/internal/config"
	"dbtool/internal/driver"
)

func TestApplyCatalogTablesUsesQualifiedNamesAndClearsSchemaInclude(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepDumpConfirm
	m.result.DumpSettings.IncludeSchema = []string{"public"}
	m.catalogTables = []driver.CatalogTable{
		{Schema: "public", Name: "users"},
		{Schema: "app", Name: "orders"},
	}
	m.catalogTableSelected = map[string]bool{"app.orders": true, "public.users": true}

	m.applyCatalogTables()

	if got, want := m.result.DumpSettings.IncludeTable, []string{"app.orders", "public.users"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("IncludeTable = %#v, want %#v", got, want)
	}
	if got := m.result.DumpSettings.IncludeSchema; len(got) != 0 {
		t.Fatalf("IncludeSchema = %#v, want none when table selection is applied", got)
	}
}

func TestApplyCatalogTablesCanExcludeSelections(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = stepDumpConfirm
	m.catalogExclude = true
	m.result.DumpSettings.IncludeTable = []string{"public.users"}
	m.result.DumpSettings.ExcludeSchema = []string{"legacy"}
	m.catalogTables = []driver.CatalogTable{{Schema: "app", Name: "audit_log"}}
	m.catalogTableSelected = map[string]bool{"app.audit_log": true}

	m.applyCatalogTables()

	if got, want := m.result.DumpSettings.ExcludeTable, []string{"app.audit_log"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ExcludeTable = %#v, want %#v", got, want)
	}
	if got := m.result.DumpSettings.IncludeTable; len(got) != 0 {
		t.Fatalf("IncludeTable = %#v, want none when exclude selection is applied", got)
	}
	if got := m.result.DumpSettings.ExcludeSchema; len(got) != 0 {
		t.Fatalf("ExcludeSchema = %#v, want none when table selection is applied", got)
	}
}
