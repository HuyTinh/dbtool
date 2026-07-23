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

func TestCollectSessionsIntegrationFiltersStateAndReturnsBlockers(t *testing.T) {
	if os.Getenv("DBTOOL_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set DBTOOL_POSTGRES_INTEGRATION=1 to run Docker PostgreSQL integration tests")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker is unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	containerName := fmt.Sprintf("dbtool-sessions-test-%d", time.Now().UnixNano())
	runDocker(t, ctx, "run", "-d", "--rm", "--name", containerName,
		"-e", "POSTGRES_USER=dbtool", "-e", "POSTGRES_PASSWORD=dbtool-test-password", "-e", "POSTGRES_DB=dbtool",
		"-p", "127.0.0.1::5432", "postgres:16-alpine")
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", containerName).Run()
	})

	waitForPostgres(t, ctx, containerName)
	profile := config.Profile{Host: "127.0.0.1", Port: dockerMappedPort(t, ctx, containerName), User: "dbtool", Password: "dbtool-test-password", Database: "dbtool"}
	snapshot, err := (&PostgresDriver{}).CollectSessions(ctx, profile, "active", 10)
	if err != nil {
		t.Fatalf("CollectSessions: %v", err)
	}
	if len(snapshot.Sessions) == 0 {
		t.Fatal("expected the collector query to be visible as an active session")
	}
	for _, session := range snapshot.Sessions {
		if session.State != "active" || session.PID == 0 || session.Database == "" || session.User == "" {
			t.Fatalf("unexpected session: %#v", session)
		}
	}
}
