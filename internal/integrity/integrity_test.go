package integrity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeFileChecksum(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.dump")
	if err := os.WriteFile(f, []byte("hello world"), 0600); err != nil {
		t.Fatal(err)
	}

	sum1, err := ComputeFileChecksum(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum1) != 64 {
		t.Fatalf("expected 64-char hex, got %d", len(sum1))
	}

	sum2, err := ComputeFileChecksum(f)
	if err != nil {
		t.Fatal(err)
	}
	if sum1 != sum2 {
		t.Fatal("same file should produce same checksum")
	}

	if err := os.WriteFile(f, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	sum3, err := ComputeFileChecksum(f)
	if err != nil {
		t.Fatal(err)
	}
	if sum1 == sum3 {
		t.Fatal("modified file should produce different checksum")
	}
}

func TestComputeFileChecksum_Directory(t *testing.T) {
	dir := t.TempDir()
	dumpDir := filepath.Join(dir, "mydump")
	if err := os.MkdirAll(dumpDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dumpDir, "toc.dat"), []byte("toc content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dumpDir, "1234.dat"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}

	sum, err := ComputeFileChecksum(dumpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum) != 64 {
		t.Fatalf("expected 64-char hex, got %d", len(sum))
	}
}

func TestComputeFileChecksum_DirectoryMissingTOC(t *testing.T) {
	dir := t.TempDir()
	dumpDir := filepath.Join(dir, "mydump")
	if err := os.MkdirAll(dumpDir, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := ComputeFileChecksum(dumpDir)
	if err == nil {
		t.Fatal("expected error for directory without toc.dat")
	}
}

func TestWriteAndReadChecksumFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.dump")
	if err := os.WriteFile(f, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := WriteChecksumFile(f, "abc123"); err != nil {
		t.Fatal(err)
	}

	got, err := ReadChecksumFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc123" {
		t.Fatalf("expected abc123, got %s", got)
	}

	sidecar := f + ".sha256"
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("sidecar file should exist: %v", err)
	}
}

func TestWriteAndReadChecksumFile_Directory(t *testing.T) {
	dir := t.TempDir()
	dumpDir := filepath.Join(dir, "mydump")
	if err := os.MkdirAll(dumpDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := WriteChecksumFile(dumpDir, "def456"); err != nil {
		t.Fatal(err)
	}

	got, err := ReadChecksumFile(dumpDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "def456" {
		t.Fatalf("expected def456, got %s", got)
	}

	sidecar := filepath.Join(dumpDir, "checksum.sha256")
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("sidecar file should exist inside directory: %v", err)
	}
}

func TestReadChecksumFile_Missing(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "nofile.dump")

	got, err := ReadChecksumFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty string for missing sidecar, got %q", got)
	}
}

func TestVerifyChecksum_Match(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.dump")
	content := []byte("integrity test data")
	if err := os.WriteFile(f, content, 0600); err != nil {
		t.Fatal(err)
	}

	sum, _ := ComputeFileChecksum(f)
	if err := WriteChecksumFile(f, sum); err != nil {
		t.Fatal(err)
	}

	ok, err := VerifyChecksum(f)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected checksum to match")
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.dump")
	if err := os.WriteFile(f, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}

	sum, _ := ComputeFileChecksum(f)
	if err := WriteChecksumFile(f, sum); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(f, []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}

	ok, err := VerifyChecksum(f)
	if ok {
		t.Fatal("expected checksum mismatch")
	}
	if err == nil {
		t.Fatal("expected error on mismatch")
	}
}

func TestVerifyChecksum_NoSidecar(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.dump")
	if err := os.WriteFile(f, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}

	ok, err := VerifyChecksum(f)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true when no sidecar exists")
	}
}

func TestFileSize_File(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.dump")
	data := []byte("12345")
	if err := os.WriteFile(f, data, 0600); err != nil {
		t.Fatal(err)
	}

	size, err := FileSize(f)
	if err != nil {
		t.Fatal(err)
	}
	if size != 5 {
		t.Fatalf("expected size 5, got %d", size)
	}
}

func TestFileSize_Directory(t *testing.T) {
	dir := t.TempDir()
	dumpDir := filepath.Join(dir, "mydump")
	if err := os.MkdirAll(dumpDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dumpDir, "a.dat"), []byte("123"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dumpDir, "b.dat"), []byte("45678"), 0600); err != nil {
		t.Fatal(err)
	}

	size, err := FileSize(dumpDir)
	if err != nil {
		t.Fatal(err)
	}
	if size != 8 {
		t.Fatalf("expected total size 8, got %d", size)
	}
}
