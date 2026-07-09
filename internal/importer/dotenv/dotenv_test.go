package dotenv

import (
	"os"
	"path/filepath"
	"testing"

	"dbtool/internal/importer"
)

func TestDotenvImporter_Detect(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dbtool-dotenv-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create some files
	filesToCreate := []string{
		".env",
		".env.local",
		".env.development",
		".env.example", // should be ignored
		"app.env",       // should be ignored
	}

	for _, f := range filesToCreate {
		filePath := filepath.Join(tmpDir, f)
		if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
			t.Fatalf("failed to create temp file %s: %v", f, err)
		}
	}

	imp := &DotenvImporter{}
	detected, err := imp.Detect(tmpDir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	expectedCount := 3
	if len(detected) != expectedCount {
		t.Errorf("expected %d files, got %d: %v", expectedCount, len(detected), detected)
	}
}

func TestDotenvImporter_Parse_SeparateKeys(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dbtool-dotenv-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	content := `
# DB settings
DB_HOST=127.0.0.1
DB_PORT=5433
DB_DATABASE=testdb
DB_USERNAME=testuser
DB_PASSWORD=testpass
`
	envPath := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	imp := &DotenvImporter{}
	profiles, err := imp.Parse(envPath)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	p := profiles[0]
	if p.Host != "127.0.0.1" {
		t.Errorf("expected Host 127.0.0.1, got %s", p.Host)
	}
	if p.Port != 5433 {
		t.Errorf("expected Port 5433, got %d", p.Port)
	}
	if p.Database != "testdb" {
		t.Errorf("expected Database testdb, got %s", p.Database)
	}
	if p.Username != "testuser" {
		t.Errorf("expected Username testuser, got %s", p.Username)
	}
	if p.Password != "testpass" {
		t.Errorf("expected Password testpass, got %s", p.Password)
	}
}

func TestDotenvImporter_Parse_DATABASE_URL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dbtool-dotenv-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	content := `
DATABASE_URL="postgresql://user:pass@localhost:5432/dbname"
`
	envPath := filepath.Join(tmpDir, ".env.local")
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	imp := &DotenvImporter{}
	profiles, err := imp.Parse(envPath)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	p := profiles[0]
	if p.Driver != "postgres" {
		t.Errorf("expected Driver postgres, got %s", p.Driver)
	}
	if p.Host != "localhost" {
		t.Errorf("expected Host localhost, got %s", p.Host)
	}
	if p.Port != 5432 {
		t.Errorf("expected Port 5432, got %d", p.Port)
	}
	if p.Database != "dbname" {
		t.Errorf("expected Database dbname, got %s", p.Database)
	}
	if p.Username != "user" {
		t.Errorf("expected Username user, got %s", p.Username)
	}
	if p.Password != "pass" {
		t.Errorf("expected Password pass, got %s", p.Password)
	}
}

func TestDotenvImporter_Parse_ResolvingReferences(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dbtool-dotenv-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	content := `
DB_PORT=5432
DB_HOST=localhost
DB_DATABASE=mydb
DATABASE_URL=postgresql://user:pass@$DB_HOST:${DB_PORT}/mydb
`
	envPath := filepath.Join(tmpDir, ".env.development")
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	imp := &DotenvImporter{}
	profiles, err := imp.Parse(envPath)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles (1 from URL, 1 from separate keys), got %d", len(profiles))
	}

	// Profile from URL
	var urlProfile *importer.ImportedProfile
	for _, p := range profiles {
		if p.Source == ".env.development -> DATABASE_URL" {
			urlProfile = &p
			break
		}
	}

	if urlProfile == nil {
		t.Fatal("could not find profile from DATABASE_URL")
	}

	if urlProfile.Host != "localhost" {
		t.Errorf("expected Host localhost, got %s", urlProfile.Host)
	}
	if urlProfile.Port != 5432 {
		t.Errorf("expected Port 5432, got %d", urlProfile.Port)
	}
	if urlProfile.Database != "mydb" {
		t.Errorf("expected Database mydb, got %s", urlProfile.Database)
	}
}
