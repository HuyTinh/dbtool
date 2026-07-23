package driver

import (
	"context"

	"dbtool/internal/config"
)

// ColumnAttributeChange describes PostgreSQL column properties to add or remove.
// Default is a PostgreSQL SQL expression and is intentionally not interpolated or
// transformed by dbtool.
type ColumnAttributeChange struct {
	Schema      string  `json:"schema"`
	Table       string  `json:"table"`
	Column      string  `json:"column"`
	Nullable    *bool   `json:"nullable,omitempty"`
	Default     *string `json:"default,omitempty"`
	DropDefault bool    `json:"drop_default,omitempty"`
	Unique      *bool   `json:"unique,omitempty"`
	Constraint  string  `json:"constraint,omitempty"`
}

// ColumnAttributeBatchChange applies the same requested attributes to one or
// more columns in a single table. The table scope makes it possible for a
// driver to preflight, lock, and apply the entire batch atomically.
type ColumnAttributeBatchChange struct {
	Schema      string   `json:"schema"`
	Table       string   `json:"table"`
	Columns     []string `json:"columns"`
	Nullable    *bool    `json:"nullable,omitempty"`
	Default     *string  `json:"default,omitempty"`
	DropDefault bool     `json:"drop_default,omitempty"`
	Unique      *bool    `json:"unique,omitempty"`
}

// ColumnAttributePlan is a read-only preflight result. Statements are the exact
// PostgreSQL DDL that ApplyColumnAttributes will execute after it rechecks the
// preflight while holding an exclusive table lock.
type ColumnAttributePlan struct {
	Change                    ColumnAttributeChange `json:"change"`
	Statements                []string              `json:"statements"`
	NullRows                  int64                 `json:"null_rows,omitempty"`
	DuplicateGroups           int64                 `json:"duplicate_groups,omitempty"`
	DuplicateRows             int64                 `json:"duplicate_rows,omitempty"`
	ExistingUniqueConstraints []string              `json:"existing_unique_constraints,omitempty"`
	Warnings                  []string              `json:"warnings,omitempty"`
}

// ColumnAttributeBatchPlan is the aggregated, per-column evidence and exact
// DDL preview for an all-or-nothing batch operation.
type ColumnAttributeBatchPlan struct {
	Change ColumnAttributeBatchChange `json:"change"`
	Plans  []ColumnAttributePlan      `json:"plans"`
}

// ColumnAttributeMetadata is the current read-only PostgreSQL state for one
// column. Primary-key constraints are reported separately because schema editor
// v1 does not alter primary keys.
type ColumnAttributeMetadata struct {
	Nullable              bool     `json:"nullable"`
	Default               string   `json:"default,omitempty"`
	UniqueConstraints     []string `json:"unique_constraints,omitempty"`
	PrimaryKeyConstraints []string `json:"primary_key_constraints,omitempty"`
}

// ColumnAttributeInspector is an optional capability for reading the current
// attribute state before a user requests a schema change.
type ColumnAttributeInspector interface {
	GetColumnAttributeMetadata(ctx context.Context, profile config.Profile, schema, table, column string) (*ColumnAttributeMetadata, error)
}

// ColumnAttributeEditor is an optional capability for schema-changing column
// attributes. Preflight is read-only; Apply re-runs it under a table lock.
type ColumnAttributeEditor interface {
	PreflightColumnAttributes(ctx context.Context, profile config.Profile, change ColumnAttributeChange) (*ColumnAttributePlan, error)
	ApplyColumnAttributes(ctx context.Context, profile config.Profile, change ColumnAttributeChange) (*ColumnAttributePlan, error)
}

// ColumnAttributeBatchEditor is an optional all-or-nothing, same-table batch
// extension. It prevents a UI from turning a selected-field batch into a
// sequence of independently committed DDL operations.
type ColumnAttributeBatchEditor interface {
	PreflightColumnAttributeBatch(ctx context.Context, profile config.Profile, change ColumnAttributeBatchChange) (*ColumnAttributeBatchPlan, error)
	ApplyColumnAttributeBatch(ctx context.Context, profile config.Profile, change ColumnAttributeBatchChange) (*ColumnAttributeBatchPlan, error)
}
