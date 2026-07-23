package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zalando/go-keyring"
)

const keyringService = "dbtool"

type secretStore interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
}

type osKeyringStore struct{}

func (osKeyringStore) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (osKeyringStore) Set(service, user, secret string) error {
	return keyring.Set(service, user, secret)
}

var profileSecretStore secretStore = osKeyringStore{}
var masterPassword string

func SetMasterPassword(password string) {
	masterPassword = password
}

func StoreProfilePassword(name string, profile *Profile) error {
	if profile == nil || profile.Password == "" || profile.PasswordRef != "" || profile.PasswordEncrypted != "" {
		return nil
	}

	account := "profile/" + name
	if err := profileSecretStore.Set(keyringService, account, profile.Password); err == nil {
		profile.Password = ""
		profile.PasswordRef = "keyring:" + keyringService + "/" + account
		profile.PasswordEncrypted = ""
		profile.PasswordSalt = ""
		profile.PasswordNonce = ""
		profile.PasswordAlgorithm = ""
		return nil
	} else if masterPassword == "" {
		return fmt.Errorf("store password for profile %q in OS keyring: %w; rerun with --ask-pass to use encrypted fallback", name, err)
	}

	ciphertext, salt, nonce, err := encryptPassword(masterPassword, profile.Password)
	if err != nil {
		return fmt.Errorf("encrypt password for profile %q: %w", name, err)
	}
	profile.Password = ""
	profile.PasswordRef = ""
	profile.PasswordEncrypted = ciphertext
	profile.PasswordSalt = salt
	profile.PasswordNonce = nonce
	profile.PasswordAlgorithm = encryptedPasswordAlgorithm
	return nil
}

func MigrateProfileSecrets(cfg *Config) (int, error) {
	if cfg == nil {
		return 0, fmt.Errorf("config is nil")
	}

	migrated := 0
	migratedProfiles := make(map[string]Profile)
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		profile := cfg.Profiles[name]
		if profile.Password == "" || profile.PasswordRef != "" || profile.PasswordEncrypted != "" {
			continue
		}
		if err := StoreProfilePassword(name, &profile); err != nil {
			return migrated, err
		}
		migratedProfiles[name] = profile
		migrated++
	}
	for name, profile := range migratedProfiles {
		cfg.Profiles[name] = profile
	}
	return migrated, nil
}

func PendingProfileSecretMigrations(cfg *Config) int {
	if cfg == nil {
		return 0
	}

	pending := 0
	for _, profile := range cfg.Profiles {
		if profile.Password != "" && profile.PasswordRef == "" && profile.PasswordEncrypted == "" {
			pending++
		}
	}
	return pending
}

func ResolveProfileSecrets(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	for name, profile := range cfg.Profiles {
		if profile.Password != "" {
			continue
		}

		switch {
		case profile.PasswordRef != "":
			service, account, ok := strings.Cut(strings.TrimPrefix(profile.PasswordRef, "keyring:"), "/")
			if !ok || service == "" || account == "" || !strings.HasPrefix(profile.PasswordRef, "keyring:") {
				return fmt.Errorf("profile %q has unsupported password reference %q", name, profile.PasswordRef)
			}
			password, err := profileSecretStore.Get(service, account)
			if err != nil {
				return fmt.Errorf("load password for profile %q from OS keyring: %w", name, err)
			}
			profile.Password = password
		case profile.PasswordEncrypted != "":
			if profile.PasswordAlgorithm != encryptedPasswordAlgorithm {
				return fmt.Errorf("profile %q has unsupported password algorithm %q", name, profile.PasswordAlgorithm)
			}
			password, err := decryptPassword(masterPassword, profile.PasswordEncrypted, profile.PasswordSalt, profile.PasswordNonce)
			if err != nil {
				return fmt.Errorf("decrypt password for profile %q: %w", name, err)
			}
			profile.Password = password
		default:
			continue
		}
		cfg.Profiles[name] = profile
	}
	return nil
}
