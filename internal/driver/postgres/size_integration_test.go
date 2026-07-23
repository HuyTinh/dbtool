package postgres

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"dbtool/internal/config"
)

func TestCollectSizeIntegrationListsRelationsAndIndexes(t *testing.T) {
	if os.Getenv("DBTOOL_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set DBTOOL_POSTGRES_INTEGRATION=1 to run Docker PostgreSQL integration tests")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker is unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	containerName := fmt.Sprintf("dbtool-size-test-%d", time.Now().UnixNano())
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
	profile := config.Profile{Host: "127.0.0.1", Port: dockerMappedPort(t, ctx, containerName), User: "dbtool", Password: "dbtool-test-password", Database: "dbtool"}
	runDocker(t, ctx, "exec", containerName, "psql", "-U", "dbtool", "-d", "dbtool", "-c", "CREATE SCHEMA app; CREATE TABLE app.orders (id bigint primary key, payload text); INSERT INTO app.orders SELECT g, repeat('x', 200) FROM generate_series(1, 2000) g;")

	snapshot, err := (&PostgresDriver{}).CollectSize(ctx, profile, "app", 10)
	if err != nil {
		t.Fatalf("CollectSize: %v", err)
	}
	if snapshot.DatabaseSize <= 0 || len(snapshot.Relations) != 1 || snapshot.Relations[0].Schema != "app" || snapshot.Relations[0].Name != "orders" {
		t.Fatalf("relations = %#v, want app.orders and database size", snapshot)
	}
	if snapshot.Relations[0].TotalBytes <= 0 || len(snapshot.Indexes) == 0 || snapshot.Indexes[0].SizeBytes <= 0 {
		t.Fatalf("snapshot = %#v, want relation and index sizes", snapshot)
	}
}
