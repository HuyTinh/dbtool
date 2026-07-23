package postgres

import (
	"context"
	"fmt"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/jackc/pgx/v5"
)

// CollectSize gathers read-only PostgreSQL catalog storage metadata. Estimated
// row counts come from catalog statistics and are not exact counts.
func (d *PostgresDriver) CollectSize(ctx context.Context, profile config.Profile, schema string, limit int) (*driver.SizeSnapshot, error) {
	if limit < 1 {
		return nil, fmt.Errorf("size explorer limit must be at least 1")
	}
	conn, err := pgx.Connect(ctx, buildPostgresDSN(profile, 3))
	if err != nil {
		return nil, fmt.Errorf("connect for size snapshot: %w", err)
	}
	defer conn.Close(ctx)

	snapshot := &driver.SizeSnapshot{}
	if err := conn.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&snapshot.DatabaseSize); err != nil {
		return nil, fmt.Errorf("read database size: %w", err)
	}

	relations, err := conn.Query(ctx, `
		SELECT n.nspname,
		       c.relname,
		       c.reltuples::bigint,
		       pg_relation_size(c.oid),
		       pg_indexes_size(c.oid),
		       COALESCE(pg_total_relation_size(c.reltoastrelid), 0),
		       pg_total_relation_size(c.oid)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'm', 'p')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND n.nspname NOT LIKE 'pg_toast%'
		  AND ($1 = '' OR n.nspname = $1)
		ORDER BY pg_total_relation_size(c.oid) DESC, n.nspname, c.relname
		LIMIT $2`, schema, limit)
	if err != nil {
		return nil, fmt.Errorf("list relation sizes: %w", err)
	}
	defer relations.Close()
	for relations.Next() {
		var relation driver.SizeRelation
		if err := relations.Scan(&relation.Schema, &relation.Name, &relation.EstimatedRows, &relation.TableBytes, &relation.IndexBytes, &relation.ToastBytes, &relation.TotalBytes); err != nil {
			return nil, fmt.Errorf("scan relation size: %w", err)
		}
		snapshot.Relations = append(snapshot.Relations, relation)
	}
	if err := relations.Err(); err != nil {
		return nil, fmt.Errorf("iterate relation sizes: %w", err)
	}

	indexes, err := conn.Query(ctx, `
		SELECT n.nspname,
		       i.relname,
		       t.relname,
		       pg_relation_size(i.oid)
		FROM pg_class i
		JOIN pg_index x ON x.indexrelid = i.oid
		JOIN pg_class t ON t.oid = x.indrelid
		JOIN pg_namespace n ON n.oid = i.relnamespace
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND n.nspname NOT LIKE 'pg_toast%'
		  AND ($1 = '' OR n.nspname = $1)
		ORDER BY pg_relation_size(i.oid) DESC, n.nspname, i.relname
		LIMIT $2`, schema, limit)
	if err != nil {
		return nil, fmt.Errorf("list index sizes: %w", err)
	}
	defer indexes.Close()
	for indexes.Next() {
		var index driver.SizeIndex
		if err := indexes.Scan(&index.Schema, &index.Name, &index.TableName, &index.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan index size: %w", err)
		}
		snapshot.Indexes = append(snapshot.Indexes, index)
	}
	if err := indexes.Err(); err != nil {
		return nil, fmt.Errorf("iterate index sizes: %w", err)
	}
	return snapshot, nil
}
