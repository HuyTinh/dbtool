package postgres

import (
	"testing"
)

func TestParseLine(t *testing.T) {
	line := "pg_restore: processing data for table \"public.users\""
	res := parseLine(line)
	if res.Message != line {
		t.Errorf("Expected message %q, got %q", line, res.Message)
	}
}

func TestClassifyStderrLine(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "pg_restore: error: role \"postgres\" already exists",
			expected: "[Ignored Warning] pg_restore: error: role \"postgres\" already exists",
		},
		{
			input:    "pg_restore: error: relation \"users\" already exists, skipping",
			expected: "[Ignored Warning] pg_restore: error: relation \"users\" already exists, skipping",
		},
		{
			input:    "pg_restore: error: some other severe database error happened",
			expected: "[Database Error] pg_restore: error: some other severe database error happened",
		},
		{
			input:    "pg_restore: standard informational message",
			expected: "pg_restore: standard informational message",
		},
	}

	for _, tc := range tests {
		res := classifyStderrLine(tc.input)
		if res.Message != tc.expected {
			t.Errorf("For input %q, expected %q, got %q", tc.input, tc.expected, res.Message)
		}
	}
}
