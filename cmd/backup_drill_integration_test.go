package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dbtool/internal/backup"
	"dbtool/internal/config"
	"dbtool/internal/history"
)

func TestDumpToStrictRestoreDrillIntegration(t *testing.T) {
	if os.Getenv("DBTOOL_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set DBTOOL_POSTGRES_INTEGRATION=1 to run Docker PostgreSQL integration tests")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := time.Now().UnixNano()
	source := fmt.Sprintf("dbtool-drill-source-%d", id)
	sandbox := fmt.Sprintf("dbtool-drill-sandbox-%d", id)
	for _, name := range []string{source, sandbox} {
		runDrillDocker(t, ctx, "run", "-d", "--rm", "--name", name, "-e", "POSTGRES_USER=dbtool", "-e", "POSTGRES_PASSWORD=dbtool-test-password", "-e", "POSTGRES_DB=dbtool", "-p", "127.0.0.1::5432", "postgres:17-alpine")
	}
	t.Cleanup(func() {
		for _, name := range []string{source, sandbox} {
			_ = exec.Command("docker", "rm", "-f", name).Run()
		}
	})
	waitDrillPostgres(t, ctx, source)
	waitDrillPostgres(t, ctx, sandbox)
	runDrillDocker(t, ctx, "exec", source, "psql", "-U", "dbtool", "-d", "dbtool", "-c", "CREATE TABLE public.orders (id integer primary key); INSERT INTO public.orders VALUES (1),(2),(3);")

	home := t.TempDir()
	oldAppData := os.Getenv("APPDATA")
	t.Setenv("APPDATA", home)
	t.Cleanup(func() { _ = os.Setenv("APPDATA", oldAppData) })
	cfg := &config.Config{Version: config.CurrentConfigVersion, Profiles: map[string]config.Profile{
		"source":  {Driver: "postgres", Host: "127.0.0.1", Port: drillPort(t, ctx, source), User: "dbtool", Password: "dbtool-test-password", Database: "dbtool"},
		"sandbox": {Driver: "postgres", Host: "127.0.0.1", Port: drillPort(t, ctx, sandbox), User: "dbtool", Password: "dbtool-test-password", Database: "dbtool", RestoreDrillSandbox: true},
	}}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(home, "orders.dump")
	oldDumpProfile, oldCounts := dumpProfile, dumpManifestRowCounts
	dumpProfile, dumpManifestRowCounts = "source", []string{"public.orders"}
	t.Cleanup(func() { dumpProfile, dumpManifestRowCounts = oldDumpProfile, oldCounts })
	if err := ExecuteDumpLogic(ctx, artifact); err != nil {
		t.Fatalf("dump: %v", err)
	}
	manifest, err := backup.ReadManifest(artifact)
	if err != nil || len(manifest.RowCounts) != 1 || manifest.RowCounts[0].RowCount != 3 {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
	oldProfile, oldConfirm, oldPolicy, oldOutput := backupRestoreDrillProfile, backupRestoreDrillConfirm, backupRestoreDrillPolicy, backupRestoreDrillOutput
	backupRestoreDrillProfile, backupRestoreDrillConfirm, backupRestoreDrillPolicy, backupRestoreDrillOutput = "sandbox", true, "strict", "json"
	t.Cleanup(func() {
		backupRestoreDrillProfile, backupRestoreDrillConfirm, backupRestoreDrillPolicy, backupRestoreDrillOutput = oldProfile, oldConfirm, oldPolicy, oldOutput
	})
	if err := executeBackupRestoreDrill(ctx, artifact); err != nil {
		t.Fatalf("restore drill: %v", err)
	}
	records, err := history.ReadHistory(history.QueryFilter{Profile: "sandbox", Limit: 1})
	if err != nil || len(records) != 1 || !records[0].Success || records[0].Operation != "restore-drill" {
		t.Fatalf("history=%#v err=%v", records, err)
	}
	manifest.RowCounts[0].RowCount = 4
	if err := backup.WriteManifest(artifact, *manifest); err != nil {
		t.Fatalf("write mismatched manifest: %v", err)
	}
	if err := executeBackupRestoreDrill(ctx, artifact); err == nil {
		t.Fatal("expected strict restore drill to reject row-count mismatch")
	}
	records, err = history.ReadHistory(history.QueryFilter{Profile: "sandbox", Limit: 1})
	if err != nil || len(records) != 1 || records[0].Success || records[0].Operation != "restore-drill" {
		t.Fatalf("failure history=%#v err=%v", records, err)
	}
}

func runDrillDocker(t *testing.T, ctx context.Context, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
func waitDrillPostgres(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	for {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		if exec.CommandContext(ctx, "docker", "exec", name, "pg_isready", "-U", "dbtool", "-d", "dbtool").Run() == nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}
func drillPort(t *testing.T, ctx context.Context, name string) int {
	t.Helper()
	out := strings.TrimSpace(runDrillDocker(t, ctx, "port", name, "5432/tcp"))
	_, p, err := net.SplitHostPort(out)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
