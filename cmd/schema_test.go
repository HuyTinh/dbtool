package cmd

import "testing"

func TestSchemaDiffCommandRegistersReadOnlyFlags(t *testing.T) {
	for _, name := range []string{"source", "target", "output"} {
		if schemaDiffCmd.Flags().Lookup(name) == nil {
			t.Fatalf("schema diff is missing --%s", name)
		}
	}
}

func TestColumnAttributeChangeFromOptionsKeepsExplicitFalse(t *testing.T) {
	change, err := columnAttributeChangeFromOptions(columnAttributeOptions{
		schema:      "public",
		table:       "users",
		column:      "email",
		nullableSet: true,
		nullable:    false,
		uniqueSet:   true,
		unique:      false,
		dropDefault: true,
		constraint:  "users_email_key",
	})
	if err != nil {
		t.Fatalf("columnAttributeChangeFromOptions: %v", err)
	}
	if change.Nullable == nil || *change.Nullable || change.Unique == nil || *change.Unique {
		t.Fatalf("change = %#v, want explicit false nullable and unique", change)
	}
	if !change.DropDefault || change.Constraint != "users_email_key" {
		t.Fatalf("change = %#v, want drop default and constraint", change)
	}
}
