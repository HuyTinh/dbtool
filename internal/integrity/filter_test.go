package integrity

import (
	"testing"
)

func makeTestTOC() TOCEntryList {
	return TOCEntryList{
		{Type: "TABLE", Schema: "public", Name: "users"},
		{Type: "TABLE", Schema: "public", Name: "orders"},
		{Type: "TABLE", Schema: "public", Name: "order_items"},
		{Type: "TABLE", Schema: "analytics", Name: "events"},
		{Type: "TABLE", Schema: "analytics", Name: "sessions"},
		{Type: "VIEW", Schema: "public", Name: "user_summary"},
	}
}

func TestMatchFilters_IncludeTableExact(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, []string{"users"}, nil, nil, nil)

	if len(result.IncludeTableMatched) != 1 || result.IncludeTableMatched[0] != "users" {
		t.Errorf("expected 'users' matched, got %v", result.IncludeTableMatched)
	}
	if len(result.IncludeTableUnmatched) != 0 {
		t.Errorf("expected no unmatched, got %v", result.IncludeTableUnmatched)
	}
}

func TestMatchFilters_IncludeTableWildcard(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, []string{"order_*"}, nil, nil, nil)

	if len(result.IncludeTableMatched) != 1 {
		t.Errorf("expected 'order_*' matched, got %v", result.IncludeTableMatched)
	}
}

func TestMatchFilters_IncludeTableUnmatched(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, []string{"nonexistent"}, nil, nil, nil)

	if len(result.IncludeTableUnmatched) != 1 || result.IncludeTableUnmatched[0] != "nonexistent" {
		t.Errorf("expected 'nonexistent' unmatched, got %v", result.IncludeTableUnmatched)
	}
}

func TestMatchFilters_ExcludeTableMatched(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, nil, []string{"events"}, nil, nil)

	if len(result.ExcludeTableMatched) != 1 || result.ExcludeTableMatched[0] != "events" {
		t.Errorf("expected 'events' matched in exclude, got %v", result.ExcludeTableMatched)
	}
}

func TestMatchFilters_IncludeSchema(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, nil, nil, []string{"analytics"}, nil)

	if len(result.IncludeSchemaMatched) != 1 || result.IncludeSchemaMatched[0] != "analytics" {
		t.Errorf("expected 'analytics' matched in include-schema, got %v", result.IncludeSchemaMatched)
	}
}

func TestMatchFilters_ExcludeSchemaUnmatched(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, nil, nil, nil, []string{"nonexistent_schema"})

	if len(result.ExcludeSchemaUnmatched) != 1 {
		t.Errorf("expected 1 unmatched exclude-schema, got %v", result.ExcludeSchemaUnmatched)
	}
}

func TestMatchFilters_SchemaQualifiedTable(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, []string{"analytics.events"}, nil, nil, nil)

	if len(result.IncludeTableMatched) != 1 {
		t.Errorf("expected 'analytics.events' matched, got matched=%v unmatched=%v",
			result.IncludeTableMatched, result.IncludeTableUnmatched)
	}
}

func TestMatchFilters_SchemaQualifiedTableWrongSchema(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, []string{"analytics.users"}, nil, nil, nil)

	if len(result.IncludeTableUnmatched) != 1 {
		t.Errorf("expected 'analytics.users' unmatched (users is in public), got matched=%v",
			result.IncludeTableMatched)
	}
}

func TestMatchFilters_NoFilters(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, nil, nil, nil, nil)

	if result.HasUnmatched() {
		t.Error("expected no unmatched patterns when no filters given")
	}
}

func TestMatchFilters_MultiplePatterns(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, []string{"users", "ghost", "orders"}, nil, nil, nil)

	if len(result.IncludeTableMatched) != 2 {
		t.Errorf("expected 2 matched, got %v", result.IncludeTableMatched)
	}
	if len(result.IncludeTableUnmatched) != 1 || result.IncludeTableUnmatched[0] != "ghost" {
		t.Errorf("expected 'ghost' unmatched, got %v", result.IncludeTableUnmatched)
	}
}

func TestMatchFilters_QuestionMarkWildcard(t *testing.T) {
	toc := makeTestTOC()
	result := MatchFilters(toc, []string{"user?"}, nil, nil, nil)

	if len(result.IncludeTableMatched) != 1 || result.IncludeTableMatched[0] != "user?" {
		t.Errorf("expected 'user?' to match 'users', got matched=%v unmatched=%v",
			result.IncludeTableMatched, result.IncludeTableUnmatched)
	}
}

func TestFilterMatchResult_HasUnmatched(t *testing.T) {
	r := FilterMatchResult{}
	if r.HasUnmatched() {
		t.Error("empty result should not have unmatched")
	}

	r.IncludeTableUnmatched = []string{"foo"}
	if !r.HasUnmatched() {
		t.Error("should have unmatched")
	}
}

func TestFilterMatchResult_UnmatchedPatterns(t *testing.T) {
	r := FilterMatchResult{
		IncludeTableUnmatched:  []string{"a"},
		ExcludeSchemaUnmatched: []string{"b"},
	}
	all := r.UnmatchedPatterns()
	if len(all) != 2 {
		t.Errorf("expected 2 unmatched patterns, got %d", len(all))
	}
}
