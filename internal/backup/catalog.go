// Package backup provides read-only dump artifact cataloging and retention planning.
package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dbtool/internal/integrity"
)

type ChecksumStatus string

const (
	ChecksumMissing  ChecksumStatus = "missing"
	ChecksumValid    ChecksumStatus = "valid"
	ChecksumMismatch ChecksumStatus = "mismatch"
	ChecksumError    ChecksumStatus = "error"
)

// Artifact is a dump artifact discovered from local storage. It never contains
// connection credentials or database content.
type Artifact struct {
	Path       string
	Size       int64
	ModifiedAt time.Time
	Checksum   ChecksumStatus
}

// RetentionPlan identifies files eligible for removal. It never deletes files.
type RetentionPlan struct {
	Keep         int
	Candidates   []Artifact
	ReclaimBytes int64
}

// ScanCatalog discovers supported dump artifacts in one directory, newest first.
func ScanCatalog(dir string) ([]Artifact, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read backup catalog directory: %w", err)
	}

	artifacts := make([]Artifact, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if !isDumpArtifact(entry, path) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("read backup artifact metadata %q: %w", path, err)
		}
		size, err := integrity.FileSize(path)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, Artifact{
			Path:       path,
			Size:       size,
			ModifiedAt: info.ModTime(),
			Checksum:   checksumStatus(path),
		})
	}
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].ModifiedAt.After(artifacts[j].ModifiedAt)
	})
	return artifacts, nil
}

// VerifyArtifact returns the checksum state for a single dump artifact.
func VerifyArtifact(path string) (Artifact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("stat backup artifact: %w", err)
	}
	size, err := integrity.FileSize(path)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{Path: path, Size: size, ModifiedAt: info.ModTime(), Checksum: checksumStatus(path)}, nil
}

// PlanRetention keeps the newest keep artifacts and marks only the older ones
// as candidates. Its output is always read-only.
func PlanRetention(artifacts []Artifact, keep int) RetentionPlan {
	if keep < 0 {
		keep = 0
	}
	sorted := append([]Artifact(nil), artifacts...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ModifiedAt.After(sorted[j].ModifiedAt)
	})
	plan := RetentionPlan{Keep: keep}
	if len(sorted) <= keep {
		return plan
	}
	plan.Candidates = append(plan.Candidates, sorted[keep:]...)
	for _, artifact := range plan.Candidates {
		plan.ReclaimBytes += artifact.Size
	}
	return plan
}

func isDumpArtifact(entry os.DirEntry, path string) bool {
	if entry.IsDir() {
		_, err := os.Stat(filepath.Join(path, "toc.dat"))
		return err == nil
	}
	switch strings.ToLower(filepath.Ext(entry.Name())) {
	case ".dump", ".backup", ".tar", ".sql":
		return true
	default:
		return false
	}
}

func checksumStatus(path string) ChecksumStatus {
	expected, err := integrity.ReadChecksumFile(path)
	if err != nil {
		return ChecksumError
	}
	if expected == "" {
		return ChecksumMissing
	}
	valid, err := integrity.VerifyChecksum(path)
	if err != nil || !valid {
		return ChecksumMismatch
	}
	return ChecksumValid
}
