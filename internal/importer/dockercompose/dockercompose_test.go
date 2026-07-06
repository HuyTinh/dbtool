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
	if p.Runtime == nil {
		t.Fatalf("expected docker runtime metadata")
	}
	if p.Runtime.Type != "docker" || p.Runtime.Source != "docker-compose" || p.Runtime.ServiceName != "pgdb" {
		t.Fatalf("unexpected runtime metadata: %#v", p.Runtime)
	}
	if p.Runtime.SourceFile != composePath {
		t.Fatalf("expected source file %s, got %s", composePath, p.Runtime.SourceFile)
	}
}

func TestDockerComposeImporter_Parse_PostgresVolumes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dbtool-compose-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	absBind := filepath.Join(tmpDir, "absolute-pgdata")
	content := `
services:
  pgdb:
    image: postgres:16
    container_name: app-postgres
    ports:
      - "55432:5432"
    environment:
      POSTGRES_PASSWORD: secret
    volumes:
      - ./pgdata:/var/lib/postgresql/data
      - pglogs:/var/log/postgresql:rw
      - ` + absBind + `:/mnt/absolute:ro
      - type: bind
        source: ./conf
        target: /etc/postgresql
      - type: volume
        source: pgbackup
        target: /backup
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

	runtime := profiles[0].Runtime
	if runtime == nil {
		t.Fatalf("expected runtime metadata")
	}
	if runtime.Container != "app-postgres" {
		t.Fatalf("expected container name app-postgres, got %s", runtime.Container)
	}

	want := map[string]struct {
		typ    string
		source string
	}{
		"/var/lib/postgresql/data": {typ: "bind", source: filepath.Join(tmpDir, "pgdata")},
		"/var/log/postgresql":      {typ: "volume", source: "pglogs"},
		"/mnt/absolute":            {typ: "bind", source: filepath.Clean(absBind)},
		"/etc/postgresql":          {typ: "bind", source: filepath.Join(tmpDir, "conf")},
		"/backup":                  {typ: "volume", source: "pgbackup"},
	}
	if len(runtime.Mounts) != len(want) {
		t.Fatalf("expected %d mounts, got %d: %#v", len(want), len(runtime.Mounts), runtime.Mounts)
	}
	for _, mount := range runtime.Mounts {
		expected, ok := want[mount.Target]
		if !ok {
			t.Fatalf("unexpected mount target %s in %#v", mount.Target, runtime.Mounts)
		}
		if mount.Type != expected.typ || mount.Source != expected.source {
			t.Fatalf("mount %s mismatch: got %#v, want type=%s source=%s", mount.Target, mount, expected.typ, expected.source)
		}
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
