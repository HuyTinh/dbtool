package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/scrypt"
)

const encryptedPasswordAlgorithm = "aes-256-gcm+scrypt"

func encryptPassword(masterPassword, password string) (ciphertext, salt, nonce string, err error) {
	if masterPassword == "" {
		return "", "", "", fmt.Errorf("master password is required")
	}

	saltBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, saltBytes); err != nil {
		return "", "", "", fmt.Errorf("generate salt: %w", err)
	}
	key, err := scrypt.Key([]byte(masterPassword), saltBytes, 32768, 8, 1, 32)
	if err != nil {
		return "", "", "", fmt.Errorf("derive encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", "", err
	}
	nonceBytes := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		return "", "", "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonceBytes, []byte(password), nil)
	return base64.StdEncoding.EncodeToString(sealed), base64.StdEncoding.EncodeToString(saltBytes), base64.StdEncoding.EncodeToString(nonceBytes), nil
}

func decryptPassword(masterPassword, ciphertext, salt, nonce string) (string, error) {
	if masterPassword == "" {
		return "", fmt.Errorf("master password is required; rerun with --ask-pass")
	}
	sealed, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode encrypted password: %w", err)
	}
	saltBytes, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		return "", fmt.Errorf("decode password salt: %w", err)
	}
	nonceBytes, err := base64.StdEncoding.DecodeString(nonce)
	if err != nil {
		return "", fmt.Errorf("decode password nonce: %w", err)
	}
	key, err := scrypt.Key([]byte(masterPassword), saltBytes, 32768, 8, 1, 32)
	if err != nil {
		return "", fmt.Errorf("derive encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(nil, nonceBytes, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt password: %w", err)
	}
	return string(plaintext), nil
}
