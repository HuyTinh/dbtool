package postgres

import (
	"context"
	"fmt"
	"time"

	"dbtool/internal/config"

	"github.com/jackc/pgx/v5"
)

func (d *PostgresDriver) Optimize(ctx context.Context, profile config.Profile) error {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database,
	)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("failed to connect for optimization: %w", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, "VACUUM ANALYZE")
	if err != nil {
		return fmt.Errorf("VACUUM ANALYZE failed: %w", err)
	}

	return nil
}
