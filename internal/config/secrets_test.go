package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

type fakeSecretStore struct {
	values map[string]string
}

func (s *fakeSecretStore) Get(service, user string) (string, error) {
	return s.values[service+"/"+user], nil
}

func (s *fakeSecretStore) Set(service, user, secret string) error {
	s.values[service+"/"+user] = secret
	return nil
}

type failingSecretStore struct {
	err error
}

func (s failingSecretStore) Get(string, string) (string, error) {
	return "", s.err
}

func (s failingSecretStore) Set(string, string, string) error {
	return s.err
}

type selectiveFailingSecretStore struct {
	values  map[string]string
	failFor string
}

func (s *selectiveFailingSecretStore) Get(service, user string) (string, error) {
	return s.values[service+"/"+user], nil
}

func (s *selectiveFailingSecretStore) Set(service, user, secret string) error {
	key := service + "/" + user
	if key == s.failFor {
		return errors.New("keyring unavailable")
	}
	s.values[key] = secret
	return nil
}

func TestMigrateProfileSecretsStoresPasswordOutsideConfig(t *testing.T) {
	store := &fakeSecretStore{values: make(map[string]string)}
	original := profileSecretStore
	profileSecretStore = store
	t.Cleanup(func() { profileSecretStore = original })

	cfg := &Config{Profiles: map[string]Profile{
		"production": {Password: "secret"},
	}}

	migrated, err := MigrateProfileSecrets(cfg)
	if err != nil {
		t.Fatalf("MigrateProfileSecrets: %v", err)
	}
	if migrated != 1 {
		t.Fatalf("migrated = %d, want 1", migrated)
	}

	profile := cfg.Profiles["production"]
	if profile.Password != "" {
		t.Fatalf("password remained in config: %q", profile.Password)
	}
	if profile.PasswordRef != "keyring:dbtool/profile/production" {
		t.Fatalf("password_ref = %q", profile.PasswordRef)
	}
	if got := store.values["dbtool/profile/production"]; got != "secret" {
		t.Fatalf("stored secret = %q, want secret", got)
	}
}

func TestResolveProfileSecretsLoadsKeyringPassword(t *testing.T) {
	store := &fakeSecretStore{values: map[string]string{
		"dbtool/profile/production": "secret",
	}}
	original := profileSecretStore
	profileSecretStore = store
	t.Cleanup(func() { profileSecretStore = original })

	cfg := &Config{Profiles: map[string]Profile{
		"production": {PasswordRef: "keyring:dbtool/profile/production"},
	}}

	if err := ResolveProfileSecrets(cfg); err != nil {
		t.Fatalf("ResolveProfileSecrets: %v", err)
	}
	if got := cfg.Profiles["production"].Password; got != "secret" {
		t.Fatalf("resolved password = %q, want secret", got)
	}
}

func TestSaveConfigDoesNotWriteResolvedKeyringPassword(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("APPDATA", configDir)

	cfg := &Config{Profiles: map[string]Profile{
		"production": {
			Password:    "secret",
			PasswordRef: "keyring:dbtool/profile/production",
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
	if !strings.Contains(string(data), "password_ref: keyring:dbtool/profile/production") {
		t.Fatalf("serialized config missing password_ref:\n%s", data)
	}
}

func TestSaveProfileStoresPasswordInKeyring(t *testing.T) {
	store := &fakeSecretStore{values: make(map[string]string)}
	original := profileSecretStore
	profileSecretStore = store
	t.Cleanup(func() { profileSecretStore = original })
	t.Setenv("APPDATA", t.TempDir())

	cfg := &Config{}
	if err := cfg.SaveProfile("production", Profile{Password: "secret"}); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	profile := cfg.Profiles["production"]
	if profile.PasswordRef != "keyring:dbtool/profile/production" {
		t.Fatalf("password_ref = %q", profile.PasswordRef)
	}
	if got := store.values["dbtool/profile/production"]; got != "secret" {
		t.Fatalf("stored secret = %q", got)
	}
}

func TestStoreProfilePasswordFallsBackToEncryptedStorageWhenKeyringFails(t *testing.T) {
	originalStore := profileSecretStore
	originalMasterPassword := masterPassword
	profileSecretStore = failingSecretStore{err: errors.New("keyring unavailable")}
	SetMasterPassword("master-password")
	t.Cleanup(func() {
		profileSecretStore = originalStore
		SetMasterPassword(originalMasterPassword)
	})

	profile := Profile{Password: "secret"}
	if err := StoreProfilePassword("production", &profile); err != nil {
		t.Fatalf("StoreProfilePassword: %v", err)
	}
	if profile.Password != "" {
		t.Fatalf("plaintext password remained in profile: %q", profile.Password)
	}
	if profile.PasswordRef != "" {
		t.Fatalf("password_ref = %q, want encrypted fallback", profile.PasswordRef)
	}
	if profile.PasswordAlgorithm != encryptedPasswordAlgorithm {
		t.Fatalf("password_algorithm = %q, want %q", profile.PasswordAlgorithm, encryptedPasswordAlgorithm)
	}
	if profile.PasswordEncrypted == "" || profile.PasswordSalt == "" || profile.PasswordNonce == "" {
		t.Fatalf("encrypted password fields must all be populated: %#v", profile)
	}
	password, err := decryptPassword(masterPassword, profile.PasswordEncrypted, profile.PasswordSalt, profile.PasswordNonce)
	if err != nil {
		t.Fatalf("decryptPassword: %v", err)
	}
	if password != "secret" {
		t.Fatalf("decrypted password = %q, want secret", password)
	}
}

func TestMigrateProfileSecretsLeavesConfigUnchangedWhenStorageFails(t *testing.T) {
	store := &selectiveFailingSecretStore{
		values:  make(map[string]string),
		failFor: "dbtool/profile/second",
	}
	originalStore := profileSecretStore
	profileSecretStore = store
	t.Cleanup(func() { profileSecretStore = originalStore })

	cfg := &Config{Profiles: map[string]Profile{
		"first":  {Password: "first-secret"},
		"second": {Password: "second-secret"},
	}}

	migrated, err := MigrateProfileSecrets(cfg)
	if err == nil {
		t.Fatal("expected migration to fail when the second secret cannot be stored")
	}
	if migrated != 1 {
		t.Fatalf("migrated = %d, want 1", migrated)
	}
	if got := cfg.Profiles["first"]; got.Password != "first-secret" || got.PasswordRef != "" {
		t.Fatalf("first profile changed after incomplete migration: %#v", got)
	}
	if got := cfg.Profiles["second"]; got.Password != "second-secret" || got.PasswordRef != "" {
		t.Fatalf("second profile changed after incomplete migration: %#v", got)
	}
}
