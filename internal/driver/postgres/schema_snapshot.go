package postgres

import (
	"context"
	"fmt"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/jackc/pgx/v5"
)

// CollectSchema reads user table and column metadata without modifying either database.
func (d *PostgresDriver) CollectSchema(ctx context.Context, profile config.Profile) (*driver.SchemaSnapshot, error) {
	conn, err := pgx.Connect(ctx, buildPostgresDSN(profile, 3))
	if err != nil {
		return nil, fmt.Errorf("connect for schema snapshot: %w", err)
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `
		SELECT c.table_schema, c.table_name, c.column_name, c.data_type,
		       c.is_nullable = 'YES', COALESCE(c.column_default, '')
		FROM information_schema.columns c
		JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE t.table_type = 'BASE TABLE'
		  AND c.table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND c.table_schema NOT LIKE 'pg_toast%'
		ORDER BY c.table_schema, c.table_name, c.ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("list schema columns: %w", err)
	}
	defer rows.Close()
	snapshot := &driver.SchemaSnapshot{}
	byTable := map[string]int{}
	for rows.Next() {
		var schema, table string
		var column driver.SchemaColumn
		if err := rows.Scan(&schema, &table, &column.Name, &column.DataType, &column.Nullable, &column.Default); err != nil {
			return nil, fmt.Errorf("scan schema column: %w", err)
		}
		key := schema + "." + table
		idx, ok := byTable[key]
		if !ok {
			idx = len(snapshot.Tables)
			byTable[key] = idx
			snapshot.Tables = append(snapshot.Tables, driver.SchemaTable{Schema: schema, Name: table})
		}
		snapshot.Tables[idx].Columns = append(snapshot.Tables[idx].Columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema columns: %w", err)
	}
	return snapshot, nil
}
