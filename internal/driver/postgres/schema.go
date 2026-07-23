package postgres

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"

	"dbtool/internal/config"

	"github.com/jackc/pgx/v5"
)

func (d *PostgresDriver) EnsureSchemas(ctx context.Context, profile config.Profile, schemas []string) error {
	if len(schemas) == 0 {
		return nil
	}

	conn, err := pgx.Connect(ctx, buildPostgresDSN(profile, 3))
	if err != nil {
		return fmt.Errorf("connect to target database: %w", err)
	}
	defer conn.Close(ctx)

	return ensureSchemas(ctx, schemas, func(ctx context.Context, statement string) error {
		_, err := conn.Exec(ctx, statement)
		return err
	})
}

func ensureSchemas(ctx context.Context, schemas []string, execute func(context.Context, string) error) error {
	seen := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		if schema == "" {
			continue
		}
		if _, ok := seen[schema]; ok {
			continue
		}
		seen[schema] = struct{}{}

		statement := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", pgx.Identifier{schema}.Sanitize())
		if err := execute(ctx, statement); err != nil {
			return fmt.Errorf("ensure schema %q: %w", schema, err)
		}
	}
	return nil
}

func buildPostgresDSN(profile config.Profile, connectTimeoutSeconds int) string {
	query := url.Values{}
	query.Set("connect_timeout", strconv.Itoa(connectTimeoutSeconds))

	return (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(profile.User, profile.Password),
		Host:     net.JoinHostPort(profile.Host, strconv.Itoa(profile.Port)),
		Path:     profile.Database,
		RawQuery: query.Encode(),
	}).String()
}
