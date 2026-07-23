package postgres

import (
	"context"
	"fmt"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/jackc/pgx/v5"
)

func (d *PostgresDriver) GetColumnAttributeMetadata(ctx context.Context, profile config.Profile, schema, table, column string) (*driver.ColumnAttributeMetadata, error) {
	conn, err := pgx.Connect(ctx, buildPostgresDSN(profile, 3))
	if err != nil {
		return nil, fmt.Errorf("connect for current column attributes: %w", err)
	}
	defer conn.Close(ctx)
	return getColumnAttributeMetadata(ctx, conn, schema, table, column)
}

func getColumnAttributeMetadata(ctx context.Context, q columnAttributeQuerier, schema, table, column string) (*driver.ColumnAttributeMetadata, error) {
	metadata := &driver.ColumnAttributeMetadata{}
	if err := q.QueryRow(ctx, `
		SELECT is_nullable = 'YES', COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name = $3`, schema, table, column).Scan(&metadata.Nullable, &metadata.Default); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("column %s.%s.%s does not exist", schema, table, column)
		}
		return nil, fmt.Errorf("read current attributes for %s.%s.%s: %w", schema, table, column, err)
	}

	rows, err := q.Query(ctx, `
		SELECT con.contype, con.conname
		FROM pg_constraint con
		JOIN pg_class rel ON rel.oid = con.conrelid
		JOIN pg_namespace ns ON ns.oid = rel.relnamespace
		JOIN pg_attribute att ON att.attrelid = rel.oid AND att.attname = $3
		WHERE con.contype IN ('u', 'p')
		  AND ns.nspname = $1 AND rel.relname = $2
		  AND array_length(con.conkey, 1) = 1 AND con.conkey[1] = att.attnum
		ORDER BY con.contype, con.conname`, schema, table, column)
	if err != nil {
		return nil, fmt.Errorf("read current constraints for %s.%s.%s: %w", schema, table, column, err)
	}
	defer rows.Close()
	for rows.Next() {
		var constraintType, name string
		if err := rows.Scan(&constraintType, &name); err != nil {
			return nil, fmt.Errorf("read current constraint for %s.%s.%s: %w", schema, table, column, err)
		}
		switch constraintType {
		case "u":
			metadata.UniqueConstraints = append(metadata.UniqueConstraints, name)
		case "p":
			metadata.PrimaryKeyConstraints = append(metadata.PrimaryKeyConstraints, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current constraints for %s.%s.%s: %w", schema, table, column, err)
	}
	return metadata, nil
}
