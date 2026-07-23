package postgres

import (
	"reflect"
	"testing"

	"dbtool/internal/driver"
)

func boolPtr(value bool) *bool       { return &value }
func stringPtr(value string) *string { return &value }

func TestBuildColumnAttributeStatementsBuildsSafePostgresDDL(t *testing.T) {
	change := driver.ColumnAttributeChange{
		Schema:   "app",
		Table:    "user accounts",
		Column:   "email",
		Nullable: boolPtr(false),
		Default:  stringPtr("lower(current_user)"),
		Unique:   boolPtr(true),
	}

	statements, err := buildColumnAttributeStatements(change, "uq_app_user_accounts_email")
	if err != nil {
		t.Fatalf("buildColumnAttributeStatements: %v", err)
	}
	want := []string{
		`ALTER TABLE "app"."user accounts" ALTER COLUMN "email" SET NOT NULL`,
		`ALTER TABLE "app"."user accounts" ALTER COLUMN "email" SET DEFAULT lower(current_user)`,
		`ALTER TABLE "app"."user accounts" ADD CONSTRAINT "uq_app_user_accounts_email" UNIQUE ("email")`,
	}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("statements = %#v, want %#v", statements, want)
	}
}

func TestBuildColumnAttributeStatementsDropsRequestedProperties(t *testing.T) {
	change := driver.ColumnAttributeChange{
		Schema:      "public",
		Table:       "users",
		Column:      "email",
		Nullable:    boolPtr(true),
		DropDefault: true,
		Unique:      boolPtr(false),
		Constraint:  "users_email_key",
	}

	statements, err := buildColumnAttributeStatements(change, "users_email_key")
	if err != nil {
		t.Fatalf("buildColumnAttributeStatements: %v", err)
	}
	want := []string{
		`ALTER TABLE "public"."users" ALTER COLUMN "email" DROP NOT NULL`,
		`ALTER TABLE "public"."users" ALTER COLUMN "email" DROP DEFAULT`,
		`ALTER TABLE "public"."users" DROP CONSTRAINT "users_email_key"`,
	}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("statements = %#v, want %#v", statements, want)
	}
}

func TestBuildColumnAttributeStatementsRejectsConflictingDefaultRequests(t *testing.T) {
	_, err := buildColumnAttributeStatements(driver.ColumnAttributeChange{
		Schema:      "public",
		Table:       "users",
		Column:      "email",
		Default:     stringPtr("'unknown'"),
		DropDefault: true,
	}, "")
	if err == nil {
		t.Fatal("expected conflicting default request to fail")
	}
}

func TestExpandColumnAttributeBatchCreatesOneSafeChangePerSelectedColumn(t *testing.T) {
	changes, err := expandColumnAttributeBatch(driver.ColumnAttributeBatchChange{
		Schema:   "crm",
		Table:    "customers",
		Columns:  []string{"email", "alternate_email"},
		Nullable: boolPtr(false),
		Unique:   boolPtr(true),
	})
	if err != nil {
		t.Fatalf("expandColumnAttributeBatch: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v, want two selected fields", changes)
	}
	for _, change := range changes {
		if change.Schema != "crm" || change.Table != "customers" || change.Nullable == nil || *change.Nullable || change.Unique == nil || !*change.Unique {
			t.Fatalf("unexpected batch change: %#v", change)
		}
	}
	if changes[0].Column != "email" || changes[1].Column != "alternate_email" {
		t.Fatalf("columns = %#v, want input order preserved", changes)
	}
}
