package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ComputeFileChecksum(filePath string) (string, error) {
	target := filePath
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("cannot stat %s: %w", filePath, err)
	}
	if info.IsDir() {
		target = filepath.Join(filePath, "toc.dat")
		if _, err := os.Stat(target); err != nil {
			return "", fmt.Errorf("directory dump missing toc.dat: %w", err)
		}
	}

	f, err := os.Open(target)
	if err != nil {
		return "", fmt.Errorf("cannot open %s: %w", target, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to hash %s: %w", target, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func WriteChecksumFile(filePath string, checksum string) error {
	sidecar := sidecarPath(filePath)
	data := []byte(checksum)
	tmp := sidecar + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("failed to write checksum sidecar: %w", err)
	}
	if err := os.Rename(tmp, sidecar); err != nil {
		return fmt.Errorf("failed to rename checksum sidecar: %w", err)
	}
	return nil
}

func ReadChecksumFile(filePath string) (string, error) {
	sidecar := sidecarPath(filePath)
	data, err := os.ReadFile(sidecar)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read checksum sidecar: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// VerifyChecksum checks the dump file against its sidecar checksum.
// Returns (true, nil) if no sidecar exists (nothing to verify).
// Returns (true, nil) if checksum matches.
// Returns (false, err) if checksum does not match.
func VerifyChecksum(filePath string) (bool, error) {
	expected, err := ReadChecksumFile(filePath)
	if err != nil {
		return false, err
	}
	if expected == "" {
		return true, nil
	}

	actual, err := ComputeFileChecksum(filePath)
	if err != nil {
		return false, fmt.Errorf("cannot compute checksum for verification: %w", err)
	}

	if actual != expected {
		return false, fmt.Errorf(
			"checksum mismatch: expected %s, got %s",
			expected[:12]+"...", actual[:12]+"...",
		)
	}
	return true, nil
}

func FileSize(filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, fmt.Errorf("cannot stat %s: %w", filePath, err)
	}
	if !info.IsDir() {
		return info.Size(), nil
	}

	var total int64
	err = filepath.Walk(filePath, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("failed to calculate directory size: %w", err)
	}
	return total, nil
}

func sidecarPath(filePath string) string {
	info, err := os.Stat(filePath)
	if err == nil && info.IsDir() {
		return filepath.Join(filePath, "checksum.sha256")
	}
	return filePath + ".sha256"
}
