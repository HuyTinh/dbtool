package cmd

import "testing"

func TestSizeCommandRegistersReadOnlyExplorerFlags(t *testing.T) {
	for _, name := range []string{"profile", "schema", "limit", "output"} {
		if sizeCmd.Flags().Lookup(name) == nil {
			t.Fatalf("size command is missing --%s", name)
		}
	}
}
