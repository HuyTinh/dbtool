package postgres

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"dbtool/internal/backup"
	"dbtool/internal/config"
	"dbtool/internal/driver"
)

func TestCollectRowCountsIntegrationDetectsRestoredDataMismatch(t *testing.T) {
	if os.Getenv("DBTOOL_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set DBTOOL_POSTGRES_INTEGRATION=1 to run Docker PostgreSQL integration tests")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker is unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	containerName := fmt.Sprintf("dbtool-row-count-test-%d", time.Now().UnixNano())
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
	runDocker(t, ctx, "exec", containerName, "psql", "-U", "dbtool", "-d", "dbtool", "-c", "CREATE TABLE public.orders (id integer primary key); INSERT INTO public.orders VALUES (1), (2), (3);")

	postgres := &PostgresDriver{}
	table := []driver.CatalogTable{{Schema: "public", Name: "orders"}}
	baseline, err := postgres.CollectRowCounts(ctx, profile, table)
	if err != nil {
		t.Fatalf("CollectRowCounts baseline: %v", err)
	}
	if len(baseline) != 1 || baseline[0].RowCount != 3 {
		t.Fatalf("baseline = %#v, want public.orders=3", baseline)
	}

	runDocker(t, ctx, "exec", containerName, "psql", "-U", "dbtool", "-d", "dbtool", "-c", "DELETE FROM public.orders WHERE id = 3;")
	actual, err := postgres.CollectRowCounts(ctx, profile, table)
	if err != nil {
		t.Fatalf("CollectRowCounts actual: %v", err)
	}
	err = backup.ValidateRowCountBaseline(backup.Manifest{RowCounts: []backup.TableRowCount{{Schema: "public", Table: "orders", RowCount: baseline[0].RowCount}}}, []backup.TableRowCount{{Schema: "public", Table: "orders", RowCount: actual[0].RowCount}})
	if err == nil {
		t.Fatal("expected altered restored data to fail baseline validation")
	}
}
