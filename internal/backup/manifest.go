package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"dbtool/internal/integrity"
)

type VerificationPolicy string

const (
	VerificationBasic  VerificationPolicy = "basic"
	VerificationStrict VerificationPolicy = "strict"
)

// Manifest is an artifact-bound, non-secret inventory used to make restore
// verification reproducible. It deliberately excludes DSNs, usernames, query
// text, and row data.
type Manifest struct {
	ArtifactChecksum string          `json:"artifact_checksum"`
	Format           string          `json:"format"`
	ArchiveVersion   string          `json:"archive_version,omitempty"`
	Schemas          []string        `json:"schemas,omitempty"`
	Tables           []string        `json:"tables,omitempty"`
	RowCounts        []TableRowCount `json:"row_counts,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

type TableRowCount struct {
	Schema   string `json:"schema"`
	Table    string `json:"table"`
	RowCount int64  `json:"row_count"`
}

func BuildManifest(artifact Artifact, format, archiveVersion string, entries integrity.TOCEntryList) Manifest {
	manifest := Manifest{Format: format, ArchiveVersion: archiveVersion, CreatedAt: time.Now().UTC()}
	if checksum, err := integrity.ReadChecksumFile(artifact.Path); err == nil {
		manifest.ArtifactChecksum = checksum
	}
	manifest.Schemas = entries.Schemas()
	for _, table := range entries.Tables() {
		manifest.Tables = append(manifest.Tables, table.Schema+"."+table.Name)
	}
	return manifest
}

func WriteManifest(artifactPath string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal backup manifest: %w", err)
	}
	path := manifestPath(artifactPath)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("write backup manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace backup manifest: %w", err)
	}
	return nil
}

func ReadManifest(artifactPath string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPath(artifactPath))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse backup manifest: %w", err)
	}
	return &manifest, nil
}

// ValidateManifest enforces the requested verification policy before a restore
// drill can execute. Basic keeps legacy artifacts usable with a clear warning;
// strict requires a checksum-bound table inventory.
func ValidateManifest(artifactPath, artifactChecksum string, policy VerificationPolicy) ([]string, error) {
	if policy != VerificationBasic && policy != VerificationStrict {
		return nil, fmt.Errorf("unsupported verification policy %q (want basic or strict)", policy)
	}
	manifest, err := ReadManifest(artifactPath)
	if os.IsNotExist(err) {
		if policy == VerificationStrict {
			return nil, fmt.Errorf("strict verification requires %s", manifestPath(artifactPath))
		}
		return []string{"backup manifest is missing; only basic restore verification will run"}, nil
	}
	if err != nil {
		return nil, err
	}
	if manifest.ArtifactChecksum == "" || manifest.ArtifactChecksum != artifactChecksum {
		return nil, fmt.Errorf("backup manifest checksum does not match the artifact")
	}
	if policy == VerificationStrict && len(manifest.Tables) == 0 {
		return nil, fmt.Errorf("strict verification requires a manifest table inventory")
	}
	if len(manifest.Tables) == 0 {
		return []string{"backup manifest has no table inventory; table recovery cannot be compared"}, nil
	}
	return nil, nil
}

func ValidateRowCountBaseline(manifest Manifest, actual []TableRowCount) error {
	actualByTable := make(map[string]int64, len(actual))
	for _, count := range actual {
		actualByTable[count.Schema+"."+count.Table] = count.RowCount
	}
	for _, expected := range manifest.RowCounts {
		name := expected.Schema + "." + expected.Table
		got, ok := actualByTable[name]
		if !ok {
			return fmt.Errorf("row-count verification did not return %s", name)
		}
		if got != expected.RowCount {
			return fmt.Errorf("row-count mismatch for %s: expected %d, got %d", name, expected.RowCount, got)
		}
	}
	return nil
}

func manifestPath(artifactPath string) string {
	info, err := os.Stat(artifactPath)
	if err == nil && info.IsDir() {
		return filepath.Join(artifactPath, "dbtool-manifest.json")
	}
	return artifactPath + ".dbtool-manifest.json"
}
