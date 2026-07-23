package cmd

import "testing"

func TestProfileAddRegistersRestoreDrillSandboxOptIn(t *testing.T) {
	if profileAddCmd.Flags().Lookup("restore-drill-sandbox") == nil {
		t.Fatal("profile add is missing --restore-drill-sandbox")
	}
}
