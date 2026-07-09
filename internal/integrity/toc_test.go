package integrity

import (
	"testing"
)

func TestTOCEntryList_Tables(t *testing.T) {
	entries := TOCEntryList{
		{Type: "TABLE", Schema: "public", Name: "users"},
		{Type: "TABLE", Schema: "public", Name: "orders"},
		{Type: "VIEW", Schema: "public", Name: "user_view"},
		{Type: "INDEX", Schema: "public", Name: "idx_users"},
		{Type: "MATERIALIZED VIEW", Schema: "analytics", Name: "daily_stats"},
	}

	tables := entries.Tables()
	if len(tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(tables))
	}
	if tables[0].Name != "users" || tables[1].Name != "orders" {
		t.Errorf("unexpected table names: %v", tables)
	}
}

func TestTOCEntryList_Views(t *testing.T) {
	entries := TOCEntryList{
		{Type: "TABLE", Schema: "public", Name: "users"},
		{Type: "VIEW", Schema: "public", Name: "user_view"},
		{Type: "MATERIALIZED VIEW", Schema: "analytics", Name: "daily_stats"},
		{Type: "INDEX", Schema: "public", Name: "idx_users"},
	}

	views := entries.Views()
	if len(views) != 2 {
		t.Fatalf("expected 2 views, got %d", len(views))
	}
	if views[0].Name != "user_view" {
		t.Errorf("expected user_view, got %s", views[0].Name)
	}
	if views[1].Name != "daily_stats" {
		t.Errorf("expected daily_stats, got %s", views[1].Name)
	}
}

func TestTOCEntryList_Schemas(t *testing.T) {
	entries := TOCEntryList{
		{Type: "TABLE", Schema: "public", Name: "users"},
		{Type: "TABLE", Schema: "public", Name: "orders"},
		{Type: "TABLE", Schema: "analytics", Name: "events"},
		{Type: "SCHEMA", Schema: "public", Name: "public"},
	}

	schemas := entries.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}

	found := make(map[string]bool)
	for _, s := range schemas {
		found[s] = true
	}
	if !found["public"] || !found["analytics"] {
		t.Errorf("expected public and analytics, got %v", schemas)
	}
}

func TestTOCEntryList_Empty(t *testing.T) {
	var entries TOCEntryList

	if len(entries.Tables()) != 0 {
		t.Error("expected 0 tables for empty list")
	}
	if len(entries.Views()) != 0 {
		t.Error("expected 0 views for empty list")
	}
	if len(entries.Schemas()) != 0 {
		t.Error("expected 0 schemas for empty list")
	}
}

func TestExtractDumpVersion(t *testing.T) {
	tests := []struct {
		name     string
		rawTOC   string
		expected string
	}{
		{
			name: "standard pg_dump header",
			rawTOC: `;
; Archive created with pg_dump version 15.4
; Dumped from database version 15.4
;
3125; 16400 16405 TABLE public orders postgres`,
			expected: "15.4",
		},
		{
			name: "version with patch",
			rawTOC: `;
; Dumped from database version 16.1.2
;
3125; 16400 16405 TABLE public orders postgres`,
			expected: "16.1.2",
		},
		{
			name:     "no version info",
			rawTOC:   "; just a comment\n3125; 16400 16405 TABLE public orders postgres",
			expected: "",
		},
		{
			name:     "empty input",
			rawTOC:   "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDumpVersion(tt.rawTOC)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
