package cmd

import "testing"

func TestBackupRestorePreflightRegistersReadOnlyFlags(t *testing.T) {
	for _, name := range []string{"profile", "output"} {
		if backupRestorePreflightCmd.Flags().Lookup(name) == nil {
			t.Fatalf("restore-preflight is missing --%s", name)
		}
	}
}
