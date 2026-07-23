package postgres

import (
	"context"
	"fmt"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/jackc/pgx/v5"
)

func (d *PostgresDriver) PreflightColumnAttributes(ctx context.Context, profile config.Profile, change driver.ColumnAttributeChange) (*driver.ColumnAttributePlan, error) {
	if err := validateColumnAttributeChange(change); err != nil {
		return nil, err
	}
	conn, err := pgx.Connect(ctx, buildPostgresDSN(profile, 3))
	if err != nil {
		return nil, fmt.Errorf("connect for column attribute preflight: %w", err)
	}
	defer conn.Close(ctx)
	return preflightColumnAttributes(ctx, conn, change)
}

func (d *PostgresDriver) ApplyColumnAttributes(ctx context.Context, profile config.Profile, change driver.ColumnAttributeChange) (*driver.ColumnAttributePlan, error) {
	if err := validateColumnAttributeChange(change); err != nil {
		return nil, err
	}
	conn, err := pgx.Connect(ctx, buildPostgresDSN(profile, 3))
	if err != nil {
		return nil, fmt.Errorf("connect to alter column attributes: %w", err)
	}
	defer conn.Close(ctx)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin column attribute transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, fmt.Sprintf("LOCK TABLE %s IN ACCESS EXCLUSIVE MODE", tableIdentifier(change))); err != nil {
		return nil, fmt.Errorf("lock %s.%s: %w", change.Schema, change.Table, err)
	}
	plan, err := preflightColumnAttributes(ctx, tx, change)
	if err != nil {
		return nil, err
	}
	for _, statement := range plan.Statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return nil, fmt.Errorf("apply column attribute statement %q: %w", statement, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit column attribute change: %w", err)
	}
	return plan, nil
}

func (d *PostgresDriver) PreflightColumnAttributeBatch(ctx context.Context, profile config.Profile, batch driver.ColumnAttributeBatchChange) (*driver.ColumnAttributeBatchPlan, error) {
	conn, err := pgx.Connect(ctx, buildPostgresDSN(profile, 3))
	if err != nil {
		return nil, fmt.Errorf("connect for column attribute batch preflight: %w", err)
	}
	defer conn.Close(ctx)
	return preflightColumnAttributeBatch(ctx, conn, batch)
}

func (d *PostgresDriver) ApplyColumnAttributeBatch(ctx context.Context, profile config.Profile, batch driver.ColumnAttributeBatchChange) (*driver.ColumnAttributeBatchPlan, error) {
	changes, err := expandColumnAttributeBatch(batch)
	if err != nil {
		return nil, err
	}
	conn, err := pgx.Connect(ctx, buildPostgresDSN(profile, 3))
	if err != nil {
		return nil, fmt.Errorf("connect to alter column attribute batch: %w", err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin column attribute batch transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, fmt.Sprintf("LOCK TABLE %s IN ACCESS EXCLUSIVE MODE", tableIdentifier(changes[0]))); err != nil {
		return nil, fmt.Errorf("lock %s.%s: %w", batch.Schema, batch.Table, err)
	}
	plan, err := preflightColumnAttributeBatch(ctx, tx, batch)
	if err != nil {
		return nil, err
	}
	for _, columnPlan := range plan.Plans {
		for _, statement := range columnPlan.Statements {
			if _, err := tx.Exec(ctx, statement); err != nil {
				return nil, fmt.Errorf("apply batch statement %q: %w", statement, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit column attribute batch: %w", err)
	}
	return plan, nil
}

func expandColumnAttributeBatch(batch driver.ColumnAttributeBatchChange) ([]driver.ColumnAttributeChange, error) {
	if strings.TrimSpace(batch.Schema) == "" || strings.TrimSpace(batch.Table) == "" {
		return nil, fmt.Errorf("schema and table are required")
	}
	if len(batch.Columns) == 0 {
		return nil, fmt.Errorf("select at least one column")
	}
	seen := make(map[string]struct{}, len(batch.Columns))
	changes := make([]driver.ColumnAttributeChange, 0, len(batch.Columns))
	for _, column := range batch.Columns {
		column = strings.TrimSpace(column)
		if column == "" {
			return nil, fmt.Errorf("selected column cannot be empty")
		}
		if _, exists := seen[column]; exists {
			return nil, fmt.Errorf("column %q was selected more than once", column)
		}
		seen[column] = struct{}{}
		change := driver.ColumnAttributeChange{
			Schema:      batch.Schema,
			Table:       batch.Table,
			Column:      column,
			Nullable:    batch.Nullable,
			Default:     batch.Default,
			DropDefault: batch.DropDefault,
			Unique:      batch.Unique,
		}
		if err := validateColumnAttributeChange(change); err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func preflightColumnAttributeBatch(ctx context.Context, q columnAttributeQuerier, batch driver.ColumnAttributeBatchChange) (*driver.ColumnAttributeBatchPlan, error) {
	changes, err := expandColumnAttributeBatch(batch)
	if err != nil {
		return nil, err
	}
	plan := &driver.ColumnAttributeBatchPlan{Change: batch, Plans: make([]driver.ColumnAttributePlan, 0, len(changes))}
	for _, change := range changes {
		columnPlan, err := preflightColumnAttributes(ctx, q, change)
		if err != nil {
			return nil, fmt.Errorf("preflight %s.%s.%s: %w", change.Schema, change.Table, change.Column, err)
		}
		plan.Plans = append(plan.Plans, *columnPlan)
	}
	return plan, nil
}

type columnAttributeQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func preflightColumnAttributes(ctx context.Context, q columnAttributeQuerier, change driver.ColumnAttributeChange) (*driver.ColumnAttributePlan, error) {
	var exists bool
	if err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_attribute a
			JOIN pg_class c ON c.oid = a.attrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2 AND a.attname = $3
			  AND a.attnum > 0 AND NOT a.attisdropped
		)`, change.Schema, change.Table, change.Column).Scan(&exists); err != nil {
		return nil, fmt.Errorf("find column %s.%s.%s: %w", change.Schema, change.Table, change.Column, err)
	}
	if !exists {
		return nil, fmt.Errorf("column %s.%s.%s does not exist", change.Schema, change.Table, change.Column)
	}

	constraints, err := singleColumnUniqueConstraints(ctx, q, change)
	if err != nil {
		return nil, err
	}
	plan := &driver.ColumnAttributePlan{Change: change, ExistingUniqueConstraints: constraints}
	if change.Nullable != nil && !*change.Nullable {
		if err := q.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NULL", tableIdentifier(change), columnIdentifier(change))).Scan(&plan.NullRows); err != nil {
			return nil, fmt.Errorf("count NULL values in %s.%s.%s: %w", change.Schema, change.Table, change.Column, err)
		}
		if plan.NullRows > 0 {
			return nil, fmt.Errorf("cannot set NOT NULL: %d row(s) currently contain NULL", plan.NullRows)
		}
	}

	constraintName := change.Constraint
	if change.Unique != nil {
		if *change.Unique {
			if len(constraints) > 0 {
				return nil, fmt.Errorf("column %s.%s.%s already has UNIQUE constraint %q", change.Schema, change.Table, change.Column, constraints[0])
			}
			if err := q.QueryRow(ctx, fmt.Sprintf(`
				SELECT COUNT(*), COALESCE(SUM(group_count), 0)
				FROM (
					SELECT COUNT(*) AS group_count
					FROM %s
					WHERE %s IS NOT NULL
					GROUP BY %s
					HAVING COUNT(*) > 1
				) duplicate_values`, tableIdentifier(change), columnIdentifier(change), columnIdentifier(change))).Scan(&plan.DuplicateGroups, &plan.DuplicateRows); err != nil {
				return nil, fmt.Errorf("check duplicate values in %s.%s.%s: %w", change.Schema, change.Table, change.Column, err)
			}
			if plan.DuplicateGroups > 0 {
				return nil, fmt.Errorf("cannot add UNIQUE: %d duplicate group(s) across %d non-NULL row(s)", plan.DuplicateGroups, plan.DuplicateRows)
			}
			if constraintName == "" {
				constraintName = defaultUniqueConstraintName(change)
			}
		} else {
			if constraintName == "" {
				if len(constraints) == 0 {
					return nil, fmt.Errorf("column %s.%s.%s has no single-column UNIQUE constraint to remove", change.Schema, change.Table, change.Column)
				}
				if len(constraints) > 1 {
					return nil, fmt.Errorf("column %s.%s.%s has multiple UNIQUE constraints; specify --constraint", change.Schema, change.Table, change.Column)
				}
				constraintName = constraints[0]
			} else if !containsConstraint(constraints, constraintName) {
				return nil, fmt.Errorf("UNIQUE constraint %q does not belong solely to %s.%s.%s", constraintName, change.Schema, change.Table, change.Column)
			}
		}
	}

	statements, err := buildColumnAttributeStatements(change, constraintName)
	if err != nil {
		return nil, err
	}
	plan.Statements = statements
	if change.Default != nil {
		plan.Warnings = append(plan.Warnings, "--default is a PostgreSQL SQL expression; review the preview before applying it")
	}
	return plan, nil
}

func singleColumnUniqueConstraints(ctx context.Context, q columnAttributeQuerier, change driver.ColumnAttributeChange) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT con.conname
		FROM pg_constraint con
		JOIN pg_class rel ON rel.oid = con.conrelid
		JOIN pg_namespace ns ON ns.oid = rel.relnamespace
		JOIN pg_attribute att ON att.attrelid = rel.oid AND att.attname = $3
		WHERE con.contype = 'u' AND ns.nspname = $1 AND rel.relname = $2
		  AND array_length(con.conkey, 1) = 1 AND con.conkey[1] = att.attnum
		ORDER BY con.conname`, change.Schema, change.Table, change.Column)
	if err != nil {
		return nil, fmt.Errorf("list UNIQUE constraints for %s.%s.%s: %w", change.Schema, change.Table, change.Column, err)
	}
	defer rows.Close()
	var constraints []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read UNIQUE constraint: %w", err)
		}
		constraints = append(constraints, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list UNIQUE constraints: %w", err)
	}
	return constraints, nil
}

func buildColumnAttributeStatements(change driver.ColumnAttributeChange, constraintName string) ([]string, error) {
	if err := validateColumnAttributeChange(change); err != nil {
		return nil, err
	}
	var statements []string
	if change.Nullable != nil {
		action := "SET NOT NULL"
		if *change.Nullable {
			action = "DROP NOT NULL"
		}
		statements = append(statements, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s", tableIdentifier(change), columnIdentifier(change), action))
	}
	if change.Default != nil {
		statements = append(statements, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", tableIdentifier(change), columnIdentifier(change), *change.Default))
	}
	if change.DropDefault {
		statements = append(statements, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", tableIdentifier(change), columnIdentifier(change)))
	}
	if change.Unique != nil {
		if constraintName == "" {
			return nil, fmt.Errorf("UNIQUE change requires a constraint name")
		}
		if *change.Unique {
			statements = append(statements, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)", tableIdentifier(change), pgx.Identifier{constraintName}.Sanitize(), columnIdentifier(change)))
		} else {
			statements = append(statements, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", tableIdentifier(change), pgx.Identifier{constraintName}.Sanitize()))
		}
	}
	return statements, nil
}

func validateColumnAttributeChange(change driver.ColumnAttributeChange) error {
	if strings.TrimSpace(change.Schema) == "" || strings.TrimSpace(change.Table) == "" || strings.TrimSpace(change.Column) == "" {
		return fmt.Errorf("schema, table, and column are required")
	}
	if change.Default != nil && change.DropDefault {
		return fmt.Errorf("--default and --drop-default cannot be used together")
	}
	if change.Default != nil && strings.TrimSpace(*change.Default) == "" {
		return fmt.Errorf("--default must be a non-empty PostgreSQL expression")
	}
	if change.Nullable == nil && change.Default == nil && !change.DropDefault && change.Unique == nil {
		return fmt.Errorf("specify at least one of --nullable, --default, --drop-default, or --unique")
	}
	return nil
}

func tableIdentifier(change driver.ColumnAttributeChange) string {
	return pgx.Identifier{change.Schema, change.Table}.Sanitize()
}

func columnIdentifier(change driver.ColumnAttributeChange) string {
	return pgx.Identifier{change.Column}.Sanitize()
}

func defaultUniqueConstraintName(change driver.ColumnAttributeChange) string {
	name := "uq_" + change.Schema + "_" + change.Table + "_" + change.Column
	if len(name) > 63 {
		return name[:63]
	}
	return name
}

func containsConstraint(constraints []string, want string) bool {
	for _, constraint := range constraints {
		if constraint == want {
			return true
		}
	}
	return false
}
