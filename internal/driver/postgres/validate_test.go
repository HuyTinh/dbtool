package postgres

import (
	"testing"
)

func TestValidateTablePattern(t *testing.T) {
	valid := []string{
		"users",
		"public.users",
		"public.*",
		"audit_log_*",
		`"MyTable"`,
		`public."MyTable"`,
	}
	invalid := []string{
		"users; drop table users;", // sql injection attempt
		"users table",              // spaces
		"public..users",            // multiple consecutive dots
		".users",                   // leading dot
		"users.",                   // trailing dot
		"users$",                   // special chars
	}

	for _, p := range valid {
		if err := ValidateTablePattern(p); err != nil {
			t.Errorf("Expected pattern %q to be valid, but got error: %v", p, err)
		}
	}

	for _, p := range invalid {
		if err := ValidateTablePattern(p); err == nil {
			t.Errorf("Expected pattern %q to be invalid, but got no error", p)
		}
	}
}
