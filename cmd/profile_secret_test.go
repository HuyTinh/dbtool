package cmd

import (
	"os"
	"testing"

	"dbtool/internal/config"
)

func TestProfileMigrateSecretsDryRunDoesNotWriteConfig(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	path, err := config.GetConfigFilePath()
	if err != nil {
		t.Fatalf("GetConfigFilePath: %v", err)
	}
	original := []byte("profiles:\n  production:\n    driver: postgres\n    password: secret\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	previousDryRun := profileMigrateSecretsDryRun
	profileMigrateSecretsDryRun = true
	t.Cleanup(func() { profileMigrateSecretsDryRun = previousDryRun })

	if err := profileMigrateSecretsCmd.RunE(profileMigrateSecretsCmd, nil); err != nil {
		t.Fatalf("migrate-secrets --dry-run: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != string(original) {
		t.Fatalf("dry-run changed config:\n%s", data)
	}
}
