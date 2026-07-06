package cmd

import (
	"testing"

	"dbtool/internal/runtime"
)

func TestSelectEffectiveHBAFilePrefersExplicitFlag(t *testing.T) {
	got := selectEffectiveHBAFile("/override/pg_hba.conf", "/server/pg_hba.conf", runtime.DetectionResult{HBAHostPath: "/host/pg_hba.conf", HBAEditable: true})
	if got != "/override/pg_hba.conf" {
		t.Fatalf("expected explicit override, got %s", got)
	}
}

func TestSelectEffectiveHBAFileUsesDetectedEditableHostPath(t *testing.T) {
	got := selectEffectiveHBAFile("", "/var/lib/postgresql/data/pg_hba.conf", runtime.DetectionResult{HBAHostPath: "/host/pg_hba.conf", HBAEditable: true})
	if got != "/host/pg_hba.conf" {
		t.Fatalf("expected detected host path, got %s", got)
	}
}

func TestSelectEffectiveHBAFileFallsBackToPostgresHBAFile(t *testing.T) {
	got := selectEffectiveHBAFile("", "/var/lib/postgresql/data/pg_hba.conf", runtime.DetectionResult{HBAMatched: true, HBAEditable: false})
	if got != "/var/lib/postgresql/data/pg_hba.conf" {
		t.Fatalf("expected postgres hba_file fallback, got %s", got)
	}
}
