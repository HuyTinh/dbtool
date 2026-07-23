package postgres

import (
	"context"
	"fmt"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/jackc/pgx/v5"
)

// CollectHealth gathers a read-only PostgreSQL health snapshot. Individual
// optional metrics degrade to unavailable when the connected role lacks access.
func (d *PostgresDriver) CollectHealth(ctx context.Context, profile config.Profile, longQueryThreshold time.Duration) (*driver.HealthSnapshot, error) {
	started := time.Now()
	conn, err := pgx.Connect(ctx, buildPostgresDSN(profile, 3))
	if err != nil {
		return nil, fmt.Errorf("connect for health snapshot: %w", err)
	}
	defer conn.Close(ctx)

	snapshot := &driver.HealthSnapshot{Latency: time.Since(started)}
	if err := conn.QueryRow(ctx, "SHOW server_version").Scan(&snapshot.ServerVersion); err != nil {
		return nil, fmt.Errorf("read PostgreSQL version: %w", err)
	}
	if err := conn.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&snapshot.DatabaseSize); err != nil {
		snapshot.Capabilities = append(snapshot.Capabilities, unavailableCapability("database_size", err))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, availableCapability("database_size"))
	}
	if err := conn.QueryRow(ctx, `
		SELECT
			count(*)::int,
			count(*) FILTER (WHERE state = 'active' AND pid <> pg_backend_pid())::int,
			count(*) FILTER (WHERE state = 'idle')::int,
			count(*) FILTER (WHERE state = 'idle in transaction')::int,
			current_setting('max_connections')::int
		FROM pg_stat_activity`).Scan(
		&snapshot.CurrentConnections,
		&snapshot.ActiveConnections,
		&snapshot.IdleConnections,
		&snapshot.IdleInTransaction,
		&snapshot.MaxConnections,
	); err != nil {
		snapshot.Capabilities = append(snapshot.Capabilities, unavailableCapability("sessions", err))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, availableCapability("sessions"))
	}
	if longQueryThreshold > 0 {
		if err := conn.QueryRow(ctx, `
			SELECT count(*)::int
			FROM pg_stat_activity
			WHERE state <> 'idle'
			  AND pid <> pg_backend_pid()
			  AND query_start < now() - $1::interval`, longQueryThreshold.String()).Scan(&snapshot.LongRunningQueries); err != nil {
			snapshot.Capabilities = append(snapshot.Capabilities, unavailableCapability("long_running_queries", err))
		} else {
			snapshot.Capabilities = append(snapshot.Capabilities, availableCapability("long_running_queries"))
		}
	}
	if err := conn.QueryRow(ctx, `
		SELECT count(*)::int
		FROM pg_stat_activity
		WHERE cardinality(pg_blocking_pids(pid)) > 0`).Scan(&snapshot.BlockedSessions); err != nil {
		snapshot.Capabilities = append(snapshot.Capabilities, unavailableCapability("blocked_sessions", err))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, availableCapability("blocked_sessions"))
	}
	return snapshot, nil
}

func availableCapability(name string) driver.HealthCapability {
	return driver.HealthCapability{Name: name, Available: true}
}

func unavailableCapability(name string, err error) driver.HealthCapability {
	return driver.HealthCapability{Name: name, Available: false, Message: err.Error()}
}
