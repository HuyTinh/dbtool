package cmd

import "testing"

func TestHealthCommandRegistersOutputAndThresholdFlags(t *testing.T) {
	if healthCmd.Flags().Lookup("profile") == nil {
		t.Fatal("health command is missing --profile")
	}
	if healthCmd.Flags().Lookup("output") == nil {
		t.Fatal("health command is missing --output")
	}
	if healthCmd.Flags().Lookup("long-query-threshold") == nil {
		t.Fatal("health command is missing --long-query-threshold")
	}
}
