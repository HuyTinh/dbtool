package driver

import "testing"

func TestCompareSchemaSnapshotsReportsMissingTablesAndColumnDifferences(t *testing.T) {
	source := &SchemaSnapshot{Tables: []SchemaTable{{
		Schema: "app", Name: "orders",
		Columns: []SchemaColumn{{Name: "id", DataType: "bigint", Nullable: false}, {Name: "status", DataType: "text", Nullable: false, Default: "'new'::text"}},
	}}}
	target := &SchemaSnapshot{Tables: []SchemaTable{{
		Schema: "app", Name: "orders",
		Columns: []SchemaColumn{{Name: "id", DataType: "integer", Nullable: false}, {Name: "legacy", DataType: "text", Nullable: true}},
	}, {Schema: "app", Name: "legacy", Columns: nil}}}

	diff := CompareSchemaSnapshots(source, target)
	if len(diff.ColumnDifferences) != 3 {
		t.Fatalf("column differences = %#v, want missing status, changed id, and extra legacy", diff.ColumnDifferences)
	}
	if len(diff.ExtraTables) != 1 || diff.ExtraTables[0] != "app.legacy" {
		t.Fatalf("extra tables = %#v, want app.legacy", diff.ExtraTables)
	}
}
