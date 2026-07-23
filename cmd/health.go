package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/spf13/cobra"
)

var (
	healthProfile            string
	healthOutput             string
	healthLongQueryThreshold time.Duration
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Collect a read-only database health snapshot",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithTimeout(cmd, cmd.Context(), func(ctx context.Context) error {
			return executeHealth(ctx)
		})
	},
}

func executeHealth(ctx context.Context) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	profile, ok := cfg.GetProfile(healthProfile)
	if !ok {
		return fmt.Errorf("profile %q not found", healthProfile)
	}
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		return err
	}
	collector, ok := drv.(driver.HealthCollector)
	if !ok {
		return fmt.Errorf("driver %q does not support health snapshots", profile.Driver)
	}
	snapshot, err := collector.CollectHealth(ctx, profile, healthLongQueryThreshold)
	if err != nil {
		return err
	}

	switch healthOutput {
	case "table":
		printHealthTable(profile.Name, snapshot)
	case "json":
		return json.NewEncoder(os.Stdout).Encode(healthJSON{HealthSnapshot: snapshot, LatencyMS: snapshot.Latency.Milliseconds()})
	default:
		return fmt.Errorf("unsupported output %q (want table or json)", healthOutput)
	}
	return nil
}

type healthJSON struct {
	*driver.HealthSnapshot
	LatencyMS int64 `json:"latency_ms"`
}

func printHealthTable(profileName string, snapshot *driver.HealthSnapshot) {
	status := "OK"
	if snapshot.IdleInTransaction > 0 || snapshot.LongRunningQueries > 0 || snapshot.BlockedSessions > 0 || hasUnavailableHealthCapability(snapshot.Capabilities) {
		status = "WARN"
	}
	fmt.Printf("Database Health: %s [%s]\n", profileName, status)
	fmt.Printf("Server version:       %s\n", snapshot.ServerVersion)
	fmt.Printf("Connection latency:   %s\n", snapshot.Latency.Round(time.Millisecond))
	fmt.Printf("Database size:        %s\n", formatBytes(snapshot.DatabaseSize))
	fmt.Printf("Connections:          %d / %d (active %d, idle %d, idle txn %d)\n", snapshot.CurrentConnections, snapshot.MaxConnections, snapshot.ActiveConnections, snapshot.IdleConnections, snapshot.IdleInTransaction)
	fmt.Printf("Long-running queries: %d (threshold %s)\n", snapshot.LongRunningQueries, healthLongQueryThreshold)
	fmt.Printf("Blocked sessions:     %d\n", snapshot.BlockedSessions)
	for _, capability := range snapshot.Capabilities {
		if !capability.Available {
			fmt.Printf("Unavailable: %s — %s\n", capability.Name, capability.Message)
		}
	}
}

func hasUnavailableHealthCapability(capabilities []driver.HealthCapability) bool {
	for _, capability := range capabilities {
		if !capability.Available {
			return true
		}
	}
	return false
}

func init() {
	healthCmd.Flags().StringVar(&healthProfile, "profile", "", "PostgreSQL profile name")
	healthCmd.Flags().StringVar(&healthOutput, "output", "table", "Output format: table or json")
	healthCmd.Flags().DurationVar(&healthLongQueryThreshold, "long-query-threshold", 5*time.Minute, "Warn for queries running longer than this duration")
	_ = healthCmd.MarkFlagRequired("profile")
	RootCmd.AddCommand(healthCmd)
}
