package cmd

import "testing"

func TestSessionsCommandRegistersReadOnlyExplorerFlags(t *testing.T) {
	for _, name := range []string{"profile", "state", "limit", "output"} {
		if sessionsCmd.Flags().Lookup(name) == nil {
			t.Fatalf("sessions command is missing --%s", name)
		}
	}
}
