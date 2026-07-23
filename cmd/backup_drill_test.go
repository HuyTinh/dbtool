package cmd

import "testing"

func TestBackupRestoreDrillRegistersGuardedFlags(t *testing.T) {
	for _, name := range []string{"profile", "confirm", "output", "verification-policy"} {
		if backupRestoreDrillCmd.Flags().Lookup(name) == nil {
			t.Fatalf("restore-drill is missing --%s", name)
		}
	}
	cmd, _, err := backupCmd.Find([]string{"restore-drill"})
	if err != nil {
		t.Fatalf("find restore-drill: %v", err)
	}
	if cmd == backupCmd {
		t.Fatal("restore-drill is not registered under backup")
	}
}
