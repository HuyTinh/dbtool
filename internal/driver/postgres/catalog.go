package postgres

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/jackc/pgx/v5"
)

func (d *PostgresDriver) ListSchemas(ctx context.Context, profile config.Profile) ([]string, error) {
	conn, err := connectCatalog(ctx, profile)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema')
		  AND schema_name NOT LIKE 'pg_toast%'
		ORDER BY schema_name`)
	if err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			return nil, fmt.Errorf("read schema: %w", err)
		}
		schemas = append(schemas, schema)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}
	return schemas, nil
}

func (d *PostgresDriver) ListTables(ctx context.Context, profile config.Profile, schemas []string) ([]driver.CatalogTable, error) {
	conn, err := connectCatalog(ctx, profile)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	query := `
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_type IN ('BASE TABLE', 'FOREIGN')
		  AND table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND table_schema NOT LIKE 'pg_toast%'`
	args := []any(nil)
	if len(schemas) > 0 {
		query += " AND table_schema = ANY($1::text[])"
		args = append(args, schemas)
	}
	query += " ORDER BY table_schema, table_name"

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var tables []driver.CatalogTable
	for rows.Next() {
		var table driver.CatalogTable
		if err := rows.Scan(&table.Schema, &table.Name); err != nil {
			return nil, fmt.Errorf("read table: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	return tables, nil
}

func (d *PostgresDriver) ListColumns(ctx context.Context, profile config.Profile, schema, table string) ([]string, error) {
	conn, err := connectCatalog(ctx, profile)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, fmt.Errorf("read column: %w", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}
	return columns, nil
}

func (d *PostgresDriver) SearchColumns(ctx context.Context, profile config.Profile, query string, limit int) ([]driver.CatalogColumnSearchResult, error) {
	conn, err := connectCatalog(ctx, profile)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	pattern := "%" + strings.ReplaceAll(strings.TrimSpace(query), " ", "%") + "%"
	rows, err := conn.Query(ctx, `
		SELECT c.table_schema, c.table_name, c.column_name, c.data_type
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE t.table_type IN ('BASE TABLE', 'FOREIGN')
		  AND c.table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND c.table_schema NOT LIKE 'pg_toast%'
		  AND concat_ws(' ', c.table_schema, c.table_name, c.column_name) ILIKE $1
		ORDER BY c.table_schema, c.table_name, c.ordinal_position
		LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search columns: %w", err)
	}
	defer rows.Close()

	var results []driver.CatalogColumnSearchResult
	for rows.Next() {
		var result driver.CatalogColumnSearchResult
		if err := rows.Scan(&result.Schema, &result.Table, &result.Column, &result.Type); err != nil {
			return nil, fmt.Errorf("read column search result: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search columns: %w", err)
	}
	return results, nil
}

func (d *PostgresDriver) CollectRowCounts(ctx context.Context, profile config.Profile, tables []driver.CatalogTable) ([]driver.TableRowCount, error) {
	conn, err := connectCatalog(ctx, profile)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	counts := make([]driver.TableRowCount, 0, len(tables))
	for _, table := range tables {
		count, err := queryRowCount(ctx, conn, table.Schema, table.Name)
		if err != nil {
			return nil, fmt.Errorf("count %s.%s: %w", table.Schema, table.Name, err)
		}
		counts = append(counts, driver.TableRowCount{Schema: table.Schema, Table: table.Name, RowCount: count})
	}
	return counts, nil
}

func connectCatalog(ctx context.Context, profile config.Profile) (*pgx.Conn, error) {
	dsn := (&url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(profile.User, profile.Password),
		Host:   net.JoinHostPort(profile.Host, strconv.Itoa(profile.Port)),
		Path:   "/" + profile.Database,
	}).String()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL catalog: %w", err)
	}
	return conn, nil
}
