package postgres

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"
)

func TestEnsureSchemasIntegration(t *testing.T) {
	if os.Getenv("DBTOOL_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set DBTOOL_POSTGRES_INTEGRATION=1 to run Docker PostgreSQL integration tests")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker is unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	containerName := fmt.Sprintf("dbtool-schema-test-%d", time.Now().UnixNano())
	runDocker(t, ctx, "run", "-d", "--rm", "--name", containerName,
		"-e", "POSTGRES_USER=dbtool",
		"-e", "POSTGRES_PASSWORD=dbtool-test-password",
		"-e", "POSTGRES_DB=dbtool",
		"-p", "127.0.0.1::5432",
		"postgres:16-alpine",
	)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", containerName).Run()
	})

	waitForPostgres(t, ctx, containerName)
	port := dockerMappedPort(t, ctx, containerName)
	profile := config.Profile{
		Host:     "127.0.0.1",
		Port:     port,
		User:     "dbtool",
		Password: "dbtool-test-password",
		Database: "dbtool",
	}

	driver := &PostgresDriver{}
	if err := driver.EnsureSchemas(ctx, profile, []string{"tenant", "tenant"}); err != nil {
		t.Fatalf("EnsureSchemas: %v", err)
	}
	if err := driver.EnsureSchemas(ctx, profile, []string{"tenant"}); err != nil {
		t.Fatalf("EnsureSchemas idempotence: %v", err)
	}

	output := runDocker(t, ctx, "exec", containerName, "psql", "-U", "dbtool", "-d", "dbtool", "-tAc",
		"SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'tenant')",
	)
	if strings.TrimSpace(output) != "t" {
		t.Fatalf("tenant schema exists = %q, want t", output)
	}
}

func TestCatalogListerIntegration(t *testing.T) {
	if os.Getenv("DBTOOL_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set DBTOOL_POSTGRES_INTEGRATION=1 to run Docker PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	containerName := fmt.Sprintf("dbtool-catalog-test-%d", time.Now().UnixNano())
	runDocker(t, ctx, "run", "-d", "--rm", "--name", containerName,
		"-e", "POSTGRES_USER=dbtool",
		"-e", "POSTGRES_PASSWORD=dbtool-test-password",
		"-e", "POSTGRES_DB=dbtool",
		"-p", "127.0.0.1::5432",
		"postgres:16-alpine",
	)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", containerName).Run()
	})

	waitForPostgres(t, ctx, containerName)
	port := dockerMappedPort(t, ctx, containerName)
	profile := config.Profile{Host: "127.0.0.1", Port: port, User: "dbtool", Password: "dbtool-test-password", Database: "dbtool"}
	runDocker(t, ctx, "exec", containerName, "psql", "-U", "dbtool", "-d", "dbtool", "-c", "CREATE SCHEMA app; CREATE TABLE app.orders (id integer); CREATE TABLE public.users (id integer);")

	catalog := &PostgresDriver{}
	schemas, err := catalog.ListSchemas(ctx, profile)
	if err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	if !containsString(schemas, "app") || !containsString(schemas, "public") || containsString(schemas, "pg_catalog") {
		t.Fatalf("schemas = %#v, want app/public without system schemas", schemas)
	}

	tables, err := catalog.ListTables(ctx, profile, []string{"app"})
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(tables) != 1 || tables[0].Schema != "app" || tables[0].Name != "orders" {
		t.Fatalf("tables = %#v, want app.orders only", tables)
	}
}

func TestColumnAttributeEditorIntegration(t *testing.T) {
	if os.Getenv("DBTOOL_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set DBTOOL_POSTGRES_INTEGRATION=1 to run Docker PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	containerName := fmt.Sprintf("dbtool-column-attributes-test-%d", time.Now().UnixNano())
	runDocker(t, ctx, "run", "-d", "--rm", "--name", containerName,
		"-e", "POSTGRES_USER=dbtool",
		"-e", "POSTGRES_PASSWORD=dbtool-test-password",
		"-e", "POSTGRES_DB=dbtool",
		"-p", "127.0.0.1::5432",
		"postgres:16-alpine",
	)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", containerName).Run()
		output, err := exec.CommandContext(cleanupCtx, "docker", "ps", "-a", "--filter", "name="+containerName, "--format", "{{.Names}}").Output()
		if err != nil {
			t.Errorf("verify Docker cleanup: %v", err)
			return
		}
		if strings.TrimSpace(string(output)) != "" {
			t.Errorf("Docker test container still exists after cleanup: %s", strings.TrimSpace(string(output)))
		}
	})

	waitForPostgres(t, ctx, containerName)
	port := dockerMappedPort(t, ctx, containerName)
	profile := config.Profile{Host: "127.0.0.1", Port: port, User: "dbtool", Password: "dbtool-test-password", Database: "dbtool"}
	runDocker(t, ctx, "exec", containerName, "psql", "-U", "dbtool", "-d", "dbtool", "-v", "ON_ERROR_STOP=1", "-c", "CREATE SCHEMA app; CREATE TABLE app.contacts (email text); INSERT INTO app.contacts (email) VALUES ('a@example.test'), ('b@example.test');")

	notNull := false
	unique := true
	defaultExpression := "'unknown'::text"
	change := driver.ColumnAttributeChange{Schema: "app", Table: "contacts", Column: "email", Nullable: &notNull, Default: &defaultExpression, Unique: &unique}
	editor := &PostgresDriver{}
	before, err := editor.GetColumnAttributeMetadata(ctx, profile, "app", "contacts", "email")
	if err != nil {
		t.Fatalf("GetColumnAttributeMetadata before apply: %v", err)
	}
	if !before.Nullable || before.Default != "" || len(before.UniqueConstraints) != 0 {
		t.Fatalf("current metadata before apply = %#v, want nullable column without default or unique", before)
	}
	plan, err := editor.PreflightColumnAttributes(ctx, profile, change)
	if err != nil {
		t.Fatalf("PreflightColumnAttributes: %v", err)
	}
	if len(plan.Statements) != 3 || plan.NullRows != 0 || plan.DuplicateGroups != 0 {
		t.Fatalf("preflight plan = %#v, want safe three-statement plan", plan)
	}
	if _, err := editor.ApplyColumnAttributes(ctx, profile, change); err != nil {
		t.Fatalf("ApplyColumnAttributes: %v", err)
	}
	after, err := editor.GetColumnAttributeMetadata(ctx, profile, "app", "contacts", "email")
	if err != nil {
		t.Fatalf("GetColumnAttributeMetadata after apply: %v", err)
	}
	if after.Nullable || after.Default != defaultExpression || len(after.UniqueConstraints) != 1 {
		t.Fatalf("current metadata after apply = %#v, want NOT NULL/default/unique", after)
	}

	output := runDocker(t, ctx, "exec", containerName, "psql", "-U", "dbtool", "-d", "dbtool", "-tAc", `
		SELECT (SELECT is_nullable = 'NO' FROM information_schema.columns WHERE table_schema = 'app' AND table_name = 'contacts' AND column_name = 'email')
		       AND (SELECT column_default = '''unknown''::text' FROM information_schema.columns WHERE table_schema = 'app' AND table_name = 'contacts' AND column_name = 'email')
		       AND EXISTS (
			SELECT 1 FROM pg_constraint con
			JOIN pg_class rel ON rel.oid = con.conrelid
			JOIN pg_namespace ns ON ns.oid = rel.relnamespace
			WHERE ns.nspname = 'app' AND rel.relname = 'contacts' AND con.contype = 'u'
		)`)
	if strings.TrimSpace(output) != "t" {
		t.Fatalf("column attributes applied = %q, want t", output)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func waitForPostgres(t *testing.T, ctx context.Context, containerName string) {
	t.Helper()
	for {
		if ctx.Err() != nil {
			t.Fatalf("PostgreSQL did not become ready: %v", ctx.Err())
		}
		if err := exec.CommandContext(ctx, "docker", "exec", containerName, "pg_isready", "-U", "dbtool", "-d", "dbtool").Run(); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func dockerMappedPort(t *testing.T, ctx context.Context, containerName string) int {
	t.Helper()
	output := strings.TrimSpace(runDocker(t, ctx, "port", containerName, "5432/tcp"))
	_, portText, err := net.SplitHostPort(output)
	if err != nil {
		t.Fatalf("parse Docker mapped port %q: %v", output, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse Docker mapped port %q: %v", output, err)
	}
	return port
}

func runDocker(t *testing.T, ctx context.Context, args ...string) string {
	t.Helper()
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
