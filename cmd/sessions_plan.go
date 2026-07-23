package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/driver"
)

var (
	sessionsPlanProfile string
	sessionsPlanPID     int32
	sessionsPlanAction  string
	sessionsPlanOutput  string
)

type sessionActionPlan struct {
	Action     string              `json:"action"`
	Selected   driver.SessionEntry `json:"selected"`
	SQLPreview string              `json:"sql_preview"`
	Risks      []string            `json:"risks"`
}

func executeSessionsPlan(ctx context.Context) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	profile, ok := cfg.GetProfile(sessionsPlanProfile)
	if !ok {
		return fmt.Errorf("profile %q not found", sessionsPlanProfile)
	}
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		return err
	}
	collector, ok := drv.(driver.SessionCollector)
	if !ok {
		return fmt.Errorf("driver %q does not support session snapshots", profile.Driver)
	}

	// This is the only database interaction in this command: a read-only snapshot.
	snapshot, err := collector.CollectSessions(ctx, profile, "", 100)
	if err != nil {
		return err
	}
	plan, err := buildSessionActionPlan(snapshot, sessionsPlanPID, sessionsPlanAction)
	if err != nil {
		return err
	}

	switch sessionsPlanOutput {
	case "table":
		fmt.Print(renderSessionActionPlanTable(profile.Name, plan))
	case "json":
		return json.NewEncoder(os.Stdout).Encode(plan)
	default:
		return fmt.Errorf("unsupported output %q (want table or json)", sessionsPlanOutput)
	}
	return nil
}

func buildSessionActionPlan(snapshot *driver.SessionSnapshot, pid int32, action string) (sessionActionPlan, error) {
	if snapshot == nil {
		return sessionActionPlan{}, fmt.Errorf("session snapshot is unavailable")
	}
	if snapshot.SelfPID != 0 && snapshot.SelfPID == pid {
		return sessionActionPlan{}, fmt.Errorf("refusing plan for self PID %d", pid)
	}

	var selected driver.SessionEntry
	found := false
	for _, session := range snapshot.Sessions {
		if session.PID == pid {
			selected = session
			found = true
			break
		}
	}
	if !found {
		return sessionActionPlan{}, fmt.Errorf("PID %d was not found in the read-only session snapshot", pid)
	}

	action = strings.ToLower(strings.TrimSpace(action))
	var sqlPreview string
	switch action {
	case "cancel":
		sqlPreview = fmt.Sprintf("SELECT pg_cancel_backend(%d);", pid)
	case "terminate":
		sqlPreview = fmt.Sprintf("SELECT pg_terminate_backend(%d);", pid)
	default:
		return sessionActionPlan{}, fmt.Errorf("unsupported action %q (want cancel or terminate)", action)
	}
	return sessionActionPlan{
		Action:     action,
		Selected:   selected,
		SQLPreview: sqlPreview,
		Risks: []string{
			"PLAN ONLY — this command never executes SQL.",
			"The previewed action can disrupt in-flight work and may roll back work in the selected session.",
			"Review state, waits, blockers, and application ownership before taking any action outside dbtool.",
		},
	}, nil
}

func renderSessionActionPlanTable(profileName string, plan sessionActionPlan) string {
	blockers := "-"
	if len(plan.Selected.BlockingPIDs) > 0 {
		pids := make([]string, len(plan.Selected.BlockingPIDs))
		for i, pid := range plan.Selected.BlockingPIDs {
			pids[i] = fmt.Sprintf("%d", pid)
		}
		blockers = strings.Join(pids, ",")
	}
	var out strings.Builder
	fmt.Fprintf(&out, "SESSION ACTION PLAN — PLAN ONLY (non-executable)\n")
	fmt.Fprintf(&out, "Profile: %s\n", profileName)
	fmt.Fprintf(&out, "Action preview: %s\n\n", plan.Action)
	fmt.Fprintf(&out, "Selected PID: %d\nState: %s\nWait: %s %s\nBlockers: %s\nQuery age: %dms\n\n", plan.Selected.PID, plan.Selected.State, plan.Selected.WaitEventType, plan.Selected.WaitEvent, blockers, plan.Selected.QueryAgeMS)
	fmt.Fprintf(&out, "SQL preview (not executed):\n%s\n\n", plan.SQLPreview)
	out.WriteString("Risks:\n")
	for _, risk := range plan.Risks {
		fmt.Fprintf(&out, "- %s\n", risk)
	}
	return out.String()
}
