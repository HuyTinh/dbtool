package cmd

import (
	"strings"
	"testing"

	"dbtool/internal/driver"
)

func TestBuildSessionActionPlanRendersNonExecutableCancelPreview(t *testing.T) {
	snapshot := &driver.SessionSnapshot{Sessions: []driver.SessionEntry{{
		PID:           42,
		Database:      "app",
		User:          "reader",
		State:         "active",
		WaitEventType: "Lock",
		WaitEvent:     "transactionid",
		QueryAgeMS:    1200,
		BlockingPIDs:  []int32{7},
	}}}

	plan, err := buildSessionActionPlan(snapshot, 42, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selected.PID != 42 || plan.Selected.State != "active" || len(plan.Selected.BlockingPIDs) != 1 {
		t.Fatalf("selected session = %#v", plan.Selected)
	}
	if plan.SQLPreview != "SELECT pg_cancel_backend(42);" {
		t.Fatalf("SQL preview = %q", plan.SQLPreview)
	}
	if !strings.Contains(strings.ToLower(strings.Join(plan.Risks, " ")), "plan only") {
		t.Fatalf("risks must state that this is non-executable: %#v", plan.Risks)
	}
}

func TestBuildSessionActionPlanBlocksDiscoveredSelfPID(t *testing.T) {
	snapshot := &driver.SessionSnapshot{SelfPID: 42, Sessions: []driver.SessionEntry{{PID: 42}}}

	_, err := buildSessionActionPlan(snapshot, 42, "terminate")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "self") {
		t.Fatalf("self action error = %v, want self-protection error", err)
	}
}

func TestRenderSessionActionPlanNeverClaimsExecution(t *testing.T) {
	plan := sessionActionPlan{
		Action:     "terminate",
		Selected:   driver.SessionEntry{PID: 9, State: "idle", WaitEventType: "Client", WaitEvent: "ClientRead"},
		SQLPreview: "SELECT pg_terminate_backend(9);",
		Risks:      []string{"PLAN ONLY — this command never executes SQL."},
	}

	out := renderSessionActionPlanTable("source", plan)
	for _, want := range []string{"PLAN ONLY", "PID", "idle", "ClientRead", "SELECT pg_terminate_backend(9);", "never executes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("plan table missing %q:\n%s", want, out)
		}
	}
}
