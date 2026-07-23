package postgres

import (
	"context"
	"fmt"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/jackc/pgx/v5"
)

// CollectSessions gathers a redacted, read-only view of PostgreSQL backends.
// It deliberately selects neither query text nor connection strings.
func (d *PostgresDriver) CollectSessions(ctx context.Context, profile config.Profile, state string, limit int) (*driver.SessionSnapshot, error) {
	if limit < 1 {
		return nil, fmt.Errorf("session explorer limit must be at least 1")
	}
	conn, err := pgx.Connect(ctx, buildPostgresDSN(profile, 3))
	if err != nil {
		return nil, fmt.Errorf("connect for session snapshot: %w", err)
	}
	defer conn.Close(ctx)

	snapshot := &driver.SessionSnapshot{}
	if err := conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&snapshot.SelfPID); err != nil {
		return nil, fmt.Errorf("identify session snapshot connection: %w", err)
	}

	rows, err := conn.Query(ctx, `
		SELECT pid,
		       COALESCE(datname, ''),
		       COALESCE(usename, ''),
		       COALESCE(state, ''),
		       COALESCE(wait_event_type, ''),
		       COALESCE(wait_event, ''),
		       COALESCE((EXTRACT(EPOCH FROM now() - query_start) * 1000)::bigint, 0),
		       pg_blocking_pids(pid)
		FROM pg_stat_activity
		WHERE ($1 = '' OR state = $1)
		ORDER BY query_start NULLS LAST, pid
		LIMIT $2`, state, limit)
	if err != nil {
		return nil, fmt.Errorf("list PostgreSQL sessions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entry driver.SessionEntry
		if err := rows.Scan(&entry.PID, &entry.Database, &entry.User, &entry.State, &entry.WaitEventType, &entry.WaitEvent, &entry.QueryAgeMS, &entry.BlockingPIDs); err != nil {
			return nil, fmt.Errorf("scan PostgreSQL session: %w", err)
		}
		snapshot.Sessions = append(snapshot.Sessions, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PostgreSQL sessions: %w", err)
	}
	return snapshot, nil
}
