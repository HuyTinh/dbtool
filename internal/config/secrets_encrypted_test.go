package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSaveConfigDoesNotWriteResolvedEncryptedPassword(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("APPDATA", configDir)

	cfg := &Config{Profiles: map[string]Profile{
		"production": {
			Password:          "secret",
			PasswordEncrypted: "ciphertext",
			PasswordSalt:      "salt",
			PasswordNonce:     "nonce",
			PasswordAlgorithm: encryptedPasswordAlgorithm,
		},
	}}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	path, err := GetConfigFilePath()
	if err != nil {
		t.Fatalf("GetConfigFilePath: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), "password: secret") {
		t.Fatalf("serialized plaintext password:\n%s", data)
	}
	if !strings.Contains(string(data), "password_encrypted: ciphertext") {
		t.Fatalf("serialized config missing encrypted payload:\n%s", data)
	}
}

func TestResolveProfileSecretsDecryptsEncryptedPassword(t *testing.T) {
	originalMasterPassword := masterPassword
	SetMasterPassword("master-password")
	t.Cleanup(func() { SetMasterPassword(originalMasterPassword) })

	ciphertext, salt, nonce, err := encryptPassword(masterPassword, "secret")
	if err != nil {
		t.Fatalf("encryptPassword: %v", err)
	}
	cfg := &Config{Profiles: map[string]Profile{
		"production": {
			PasswordEncrypted: ciphertext,
			PasswordSalt:      salt,
			PasswordNonce:     nonce,
			PasswordAlgorithm: encryptedPasswordAlgorithm,
		},
	}}

	if err := ResolveProfileSecrets(cfg); err != nil {
		t.Fatalf("ResolveProfileSecrets: %v", err)
	}
	if got := cfg.Profiles["production"].Password; got != "secret" {
		t.Fatalf("resolved password = %q, want secret", got)
	}
}

func TestStoreProfilePasswordFailsClosedWithoutMasterPassword(t *testing.T) {
	originalStore := profileSecretStore
	originalMasterPassword := masterPassword
	profileSecretStore = failingSecretStore{err: errors.New("keyring unavailable")}
	SetMasterPassword("")
	t.Cleanup(func() {
		profileSecretStore = originalStore
		SetMasterPassword(originalMasterPassword)
	})

	profile := Profile{Password: "secret"}
	if err := StoreProfilePassword("production", &profile); err == nil {
		t.Fatal("expected keyring failure without encrypted fallback master password")
	}
	if profile.Password != "secret" || profile.PasswordRef != "" || profile.PasswordEncrypted != "" {
		t.Fatalf("profile changed after failed storage: %#v", profile)
	}
}

func TestMigrateProfileSecretsUsesEncryptedFallbackAndCountsPendingProfiles(t *testing.T) {
	originalStore := profileSecretStore
	originalMasterPassword := masterPassword
	profileSecretStore = failingSecretStore{err: errors.New("keyring unavailable")}
	SetMasterPassword("master-password")
	t.Cleanup(func() {
		profileSecretStore = originalStore
		SetMasterPassword(originalMasterPassword)
	})

	cfg := &Config{Profiles: map[string]Profile{
		"legacy":    {Password: "secret"},
		"keyring":   {PasswordRef: "keyring:dbtool/profile/keyring"},
		"encrypted": {PasswordEncrypted: "ciphertext"},
	}}
	if got := PendingProfileSecretMigrations(cfg); got != 1 {
		t.Fatalf("PendingProfileSecretMigrations = %d, want 1", got)
	}

	migrated, err := MigrateProfileSecrets(cfg)
	if err != nil {
		t.Fatalf("MigrateProfileSecrets: %v", err)
	}
	if migrated != 1 {
		t.Fatalf("migrated = %d, want 1", migrated)
	}
	profile := cfg.Profiles["legacy"]
	if profile.Password != "" || profile.PasswordEncrypted == "" || profile.PasswordAlgorithm != encryptedPasswordAlgorithm {
		t.Fatalf("legacy profile was not migrated to encrypted fallback: %#v", profile)
	}
}
