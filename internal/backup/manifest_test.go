package backup

import (
	"os"
	"path/filepath"
	"testing"

	"dbtool/internal/integrity"
)

func TestWriteAndReadManifestPreservesArtifactInventory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.dump")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := BuildManifest(Artifact{Path: path, Checksum: ChecksumValid}, "custom", "16.2", integrity.TOCEntryList{
		{Type: "TABLE", Schema: "public", Name: "orders"},
		{Type: "TABLE", Schema: "public", Name: "users"},
	})
	if err := WriteManifest(path, manifest); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	got, err := ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.Format != "custom" || got.ArchiveVersion != "16.2" || len(got.Tables) != 2 {
		t.Fatalf("manifest = %#v", got)
	}
}

func TestValidateManifestRequiresManifestForStrictPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.dump")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateManifest(path, "abc", VerificationStrict); err == nil {
		t.Fatal("expected strict policy to reject a missing manifest")
	}
	warnings, err := ValidateManifest(path, "abc", VerificationBasic)
	if err != nil || len(warnings) != 1 {
		t.Fatalf("basic policy = warnings %#v, err %v", warnings, err)
	}
}

func TestValidateManifestRejectsArtifactChecksumMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.dump")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(path, Manifest{ArtifactChecksum: "expected", Format: "custom", Tables: []string{"public.orders"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateManifest(path, "actual", VerificationStrict); err == nil {
		t.Fatal("expected checksum mismatch to be rejected")
	}
}

func TestValidateRowCountBaselineRejectsMismatch(t *testing.T) {
	manifest := Manifest{RowCounts: []TableRowCount{{Schema: "public", Table: "orders", RowCount: 4}}}
	err := ValidateRowCountBaseline(manifest, []TableRowCount{{Schema: "public", Table: "orders", RowCount: 3}})
	if err == nil {
		t.Fatal("expected row-count mismatch to fail")
	}
}
