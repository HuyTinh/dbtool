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
	sessionsProfile string
	sessionsState   string
	sessionsLimit   int
	sessionsOutput  string
)

func executeSessions(ctx context.Context) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	profile, ok := cfg.GetProfile(sessionsProfile)
	if !ok {
		return fmt.Errorf("profile %q not found", sessionsProfile)
	}
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		return err
	}
	collector, ok := drv.(driver.SessionCollector)
	if !ok {
		return fmt.Errorf("driver %q does not support session snapshots", profile.Driver)
	}
	snapshot, err := collector.CollectSessions(ctx, profile, sessionsState, sessionsLimit)
	if err != nil {
		return err
	}
	switch sessionsOutput {
	case "table":
		printSessionsTable(profile.Name, sessionsState, snapshot)
	case "json":
		return json.NewEncoder(os.Stdout).Encode(snapshot)
	default:
		return fmt.Errorf("unsupported output %q (want table or json)", sessionsOutput)
	}
	return nil
}

func printSessionsTable(profileName, state string, snapshot *driver.SessionSnapshot) {
	filter := "all states"
	if state != "" {
		filter = state
	}
	fmt.Printf("PostgreSQL Sessions: %s (%s)\n", profileName, filter)
	fmt.Printf("%-8s %-18s %-18s %-22s %-14s %-14s %10s %s\n", "PID", "DATABASE", "USER", "STATE", "WAIT TYPE", "WAIT EVENT", "AGE", "BLOCKERS")
	for _, session := range snapshot.Sessions {
		blockers := "-"
		if len(session.BlockingPIDs) > 0 {
			parts := make([]string, len(session.BlockingPIDs))
			for i, pid := range session.BlockingPIDs {
				parts[i] = fmt.Sprintf("%d", pid)
			}
			blockers = strings.Join(parts, ",")
		}
		fmt.Printf("%-8d %-18s %-18s %-22s %-14s %-14s %9dms %s\n", session.PID, session.Database, session.User, session.State, session.WaitEventType, session.WaitEvent, session.QueryAgeMS, blockers)
	}
}
