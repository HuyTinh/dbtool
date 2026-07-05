package postgres

import (
	"fmt"
	"regexp"
)

var tablePatternRegex = regexp.MustCompile(`^"?[a-zA-Z_*?][a-zA-Z0-9_*?]*"?(\."?[a-zA-Z_*?][a-zA-Z0-9_*?]*"?)?$`)

// ValidateTablePattern checks if the user-input table/schema pattern is valid
func ValidateTablePattern(pattern string) error {
	if !tablePatternRegex.MatchString(pattern) {
		return fmt.Errorf(
			"pattern %q is invalid - correct format is 'table' or 'schema.table' "+
				"(wildcards like * are supported, e.g. 'public.user_*'); if table name has "+
				"uppercase or special characters, wrap them in double quotes", pattern)
	}
	return nil
}
