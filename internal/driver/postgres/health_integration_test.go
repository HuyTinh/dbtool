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

func TestCollectHealthIntegration(t *testing.T) {
	if os.Getenv("DBTOOL_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set DBTOOL_POSTGRES_INTEGRATION=1 to run Docker PostgreSQL integration tests")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker is unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	containerName := fmt.Sprintf("dbtool-health-test-%d", time.Now().UnixNano())
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
	snapshot, err := (&PostgresDriver{}).CollectHealth(ctx, profile, time.Minute)
	if err != nil {
		t.Fatalf("CollectHealth: %v", err)
	}
	if snapshot.ServerVersion == "" {
		t.Fatal("server version is empty")
	}
	if snapshot.DatabaseSize <= 0 || snapshot.MaxConnections <= 0 {
		t.Fatalf("snapshot = %#v, want database size and max connections", snapshot)
	}
	if snapshot.Latency <= 0 {
		t.Fatalf("latency = %s, want positive duration", snapshot.Latency)
	}
}
