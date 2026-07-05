package dockercompose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDockerComposeImporter_Detect(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dbtool-compose-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filesToCreate := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
		"other.yml", // should be ignored
	}

	for _, f := range filesToCreate {
		filePath := filepath.Join(tmpDir, f)
		if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
			t.Fatalf("failed to create temp file %s: %v", f, err)
		}
	}

	imp := &DockerComposeImporter{}
	detected, err := imp.Detect(tmpDir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	expectedCount := 4
	if len(detected) != expectedCount {
		t.Errorf("expected %d files, got %d: %v", expectedCount, len(detected), detected)
	}
}

func TestDockerComposeImporter_Parse_Postgres(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dbtool-compose-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	content := `
version: '3.8'
services:
  pgdb:
    image: postgres:15-alpine
    ports:
      - "5439:5432"
    environment:
      POSTGRES_DB: main_db
      POSTGRES_USER: admin
      POSTGRES_PASSWORD: secret_password
`
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	imp := &DockerComposeImporter{}
	profiles, err := imp.Parse(composePath)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	p := profiles[0]
	if p.SuggestedName != "compose-pgdb" {
		t.Errorf("expected SuggestedName compose-pgdb, got %s", p.SuggestedName)
	}
	if p.Driver != "postgres" {
		t.Errorf("expected Driver postgres, got %s", p.Driver)
	}
	if p.Host != "localhost" {
		t.Errorf("expected Host localhost, got %s", p.Host)
	}
	if p.Port != 5439 {
		t.Errorf("expected Port 5439, got %d", p.Port)
	}
	if p.Database != "main_db" {
		t.Errorf("expected Database main_db, got %s", p.Database)
	}
	if p.Username != "admin" {
		t.Errorf("expected Username admin, got %s", p.Username)
	}
	if p.Password != "secret_password" {
		t.Errorf("expected Password secret_password, got %s", p.Password)
	}
}

func TestDockerComposeImporter_Parse_MySQL_ListEnv(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dbtool-compose-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	content := `
services:
  mysql-db:
    image: mysql:8.0
    ports:
      - 3307:3306
    environment:
      - MYSQL_DATABASE=shop
      - MYSQL_USER=buyer
      - MYSQL_PASSWORD=buy_pass
`
	composePath := filepath.Join(tmpDir, "compose.yml")
	if err := os.WriteFile(composePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	imp := &DockerComposeImporter{}
	profiles, err := imp.Parse(composePath)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	p := profiles[0]
	if p.SuggestedName != "compose-mysql-db" {
		t.Errorf("expected SuggestedName compose-mysql-db, got %s", p.SuggestedName)
	}
	if p.Driver != "mysql" {
		t.Errorf("expected Driver mysql, got %s", p.Driver)
	}
	if p.Port != 3307 {
		t.Errorf("expected Port 3307, got %d", p.Port)
	}
	if p.Database != "shop" {
		t.Errorf("expected Database shop, got %s", p.Database)
	}
	if p.Username != "buyer" {
		t.Errorf("expected Username buyer, got %s", p.Username)
	}
	if p.Password != "buy_pass" {
		t.Errorf("expected Password buy_pass, got %s", p.Password)
	}
}
