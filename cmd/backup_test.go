package cmd

import "testing"

func TestBackupCommandRegistersReadOnlySubcommands(t *testing.T) {
	for _, name := range []string{"verify", "list", "retention-preview", "audit"} {
		cmd, _, err := backupCmd.Find([]string{name})
		if err != nil {
			t.Fatalf("find %q: %v", name, err)
		}
		if cmd == backupCmd {
			t.Fatalf("subcommand %q is not registered", name)
		}
	}
}

func TestDumpRegistersManifestRowCountFlag(t *testing.T) {
	if dumpCmd.Flags().Lookup("manifest-row-count") == nil {
		t.Fatal("dump is missing --manifest-row-count")
	}
}
