package cmd

import "testing"

func TestMigratePreflightRegistersReadOnlyProfileFlags(t *testing.T) {
	for _, name := range []string{"source", "target", "output"} {
		if migratePreflightCmd.Flags().Lookup(name) == nil {
			t.Fatalf("migrate preflight is missing --%s", name)
		}
	}
}
