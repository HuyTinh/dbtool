package cmd

import "testing"

func TestSecretFallbackFlagsAreRegistered(t *testing.T) {
	if RootCmd.PersistentFlags().Lookup("ask-pass") == nil {
		t.Fatal("root command is missing --ask-pass")
	}
	if profileMigrateSecretsCmd.Flags().Lookup("dry-run") == nil {
		t.Fatal("profile migrate-secrets is missing --dry-run")
	}
}
