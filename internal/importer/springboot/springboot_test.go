package springboot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseYAML_Basic(t *testing.T) {
	yml := `
spring:
  datasource:
    url: jdbc:postgresql://localhost:5432/mydb
    username: admin
    password: secret
`
	dir := t.TempDir()
	f := filepath.Join(dir, "application.yml")
	if err := os.WriteFile(f, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	imp := &SpringBootImporter{}
	profiles, err := imp.Parse(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	p := profiles[0]
	if p.Driver != "postgres" {
		t.Errorf("expected driver postgres, got %s", p.Driver)
	}
	if p.Host != "localhost" {
		t.Errorf("expected host localhost, got %s", p.Host)
	}
	if p.Port != 5432 {
		t.Errorf("expected port 5432, got %d", p.Port)
	}
	if p.Database != "mydb" {
		t.Errorf("expected database mydb, got %s", p.Database)
	}
	if p.Username != "admin" {
		t.Errorf("expected username admin, got %s", p.Username)
	}
	if p.Password != "secret" {
		t.Errorf("expected password secret, got %s", p.Password)
	}
}

func TestParseProperties_Basic(t *testing.T) {
	props := `
spring.datasource.url=jdbc:postgresql://db.example.com:5433/prod
spring.datasource.username=prod_user
spring.datasource.password=prod_pass
`
	dir := t.TempDir()
	f := filepath.Join(dir, "application.properties")
	if err := os.WriteFile(f, []byte(props), 0644); err != nil {
		t.Fatal(err)
	}

	imp := &SpringBootImporter{}
	profiles, err := imp.Parse(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	p := profiles[0]
	if p.Host != "db.example.com" {
		t.Errorf("expected host db.example.com, got %s", p.Host)
	}
	if p.Port != 5433 {
		t.Errorf("expected port 5433, got %d", p.Port)
	}
	if p.Database != "prod" {
		t.Errorf("expected database prod, got %s", p.Database)
	}
}

func TestParseYAML_PlaceholderResolved(t *testing.T) {
	t.Setenv("DB_PASS", "envpassword")
	yml := `
spring:
  datasource:
    url: jdbc:postgresql://localhost:5432/mydb
    username: user
    password: ${DB_PASS}
`
	dir := t.TempDir()
	f := filepath.Join(dir, "application.yml")
	if err := os.WriteFile(f, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	imp := &SpringBootImporter{}
	profiles, err := imp.Parse(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) == 0 {
		t.Fatal("expected profiles, got none")
	}
	p := profiles[0]
	if p.Password != "envpassword" {
		t.Errorf("expected resolved password 'envpassword', got %q", p.Password)
	}
	if p.PasswordIsRef {
		t.Errorf("expected PasswordIsRef=false when env var is set")
	}
}

func TestParseYAML_PlaceholderUnresolved(t *testing.T) {
	os.Unsetenv("DB_SECRET")
	yml := `
spring:
  datasource:
    url: jdbc:postgresql://localhost:5432/mydb
    username: user
    password: ${DB_SECRET}
`
	dir := t.TempDir()
	f := filepath.Join(dir, "application.yml")
	if err := os.WriteFile(f, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	imp := &SpringBootImporter{}
	profiles, err := imp.Parse(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) == 0 {
		t.Fatal("expected profiles, got none")
	}
	if !profiles[0].PasswordIsRef {
		t.Errorf("expected PasswordIsRef=true when env var is not set")
	}
}

func TestParseYAML_DefaultPlaceholder(t *testing.T) {
	yml := `
spring:
  datasource:
    url: jdbc:postgresql://localhost:5432/mydb
    username: ${DB_USER:admin}
    password: ${DB_PASS:default123}
`
	dir := t.TempDir()
	f := filepath.Join(dir, "application.yml")
	if err := os.WriteFile(f, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("DB_USER")
	os.Unsetenv("DB_PASS")

	imp := &SpringBootImporter{}
	profiles, err := imp.Parse(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) == 0 {
		t.Fatal("expected profiles, got none")
	}
	p := profiles[0]
	if p.Username != "admin" {
		t.Errorf("expected username 'admin' from default, got %q", p.Username)
	}
	if p.Password != "default123" {
		t.Errorf("expected password 'default123' from default, got %q", p.Password)
	}
}

func TestDetect_FindsConfigFiles(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"application.yml",
		"application-dev.properties",
		"application-prod.yaml",
		"not_application.yml",    // should not be found
		"application_config.xml", // should not be found
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}

	imp := &SpringBootImporter{}
	found, err := imp.Detect(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundNames := make(map[string]bool)
	for _, f := range found {
		foundNames[filepath.Base(f)] = true
	}

	expected := []string{"application.yml", "application-dev.properties", "application-prod.yaml"}
	for _, name := range expected {
		if !foundNames[name] {
			t.Errorf("expected to find %s", name)
		}
	}
	if foundNames["not_application.yml"] {
		t.Error("should NOT find not_application.yml")
	}
	if foundNames["application_config.xml"] {
		t.Error("should NOT find application_config.xml")
	}
}

func TestParseJDBCURL(t *testing.T) {
	tests := []struct {
		url      string
		wantDrv  string
		wantHost string
		wantPort int
		wantDB   string
		wantErr  bool
	}{
		{"jdbc:postgresql://localhost:5432/mydb", "postgres", "localhost", 5432, "mydb", false},
		{"jdbc:postgres://db.host.com/appdb", "postgres", "db.host.com", 0, "appdb", false},
		{"jdbc:mysql://127.0.0.1:3306/shop", "mysql", "127.0.0.1", 3306, "shop", false},
		{"not_jdbc_url", "", "", 0, "", true},
	}

	for _, tt := range tests {
		drv, host, port, db, err := parseJDBCURL(tt.url)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseJDBCURL(%q) error=%v, wantErr=%v", tt.url, err, tt.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if drv != tt.wantDrv {
			t.Errorf("parseJDBCURL(%q) driver=%q, want %q", tt.url, drv, tt.wantDrv)
		}
		if host != tt.wantHost {
			t.Errorf("parseJDBCURL(%q) host=%q, want %q", tt.url, host, tt.wantHost)
		}
		if port != tt.wantPort {
			t.Errorf("parseJDBCURL(%q) port=%d, want %d", tt.url, port, tt.wantPort)
		}
		if db != tt.wantDB {
			t.Errorf("parseJDBCURL(%q) database=%q, want %q", tt.url, db, tt.wantDB)
		}
	}
}
