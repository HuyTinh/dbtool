package driver

import (
	"context"
	"sort"

	"dbtool/internal/config"
)

// SchemaColumn is read-only PostgreSQL column metadata used for migration review.
type SchemaColumn struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Nullable bool   `json:"nullable"`
	Default  string `json:"default,omitempty"`
}

type SchemaTable struct {
	Schema  string         `json:"schema"`
	Name    string         `json:"name"`
	Columns []SchemaColumn `json:"columns"`
}

// SchemaSnapshot holds catalog metadata only; it never represents table data.
type SchemaSnapshot struct {
	Tables []SchemaTable `json:"tables"`
}

type ColumnDifference struct {
	Table  string        `json:"table"`
	Column string        `json:"column"`
	Source *SchemaColumn `json:"source,omitempty"`
	Target *SchemaColumn `json:"target,omitempty"`
}

type SchemaDiff struct {
	MissingTables     []string           `json:"missing_tables"`
	ExtraTables       []string           `json:"extra_tables"`
	ColumnDifferences []ColumnDifference `json:"column_differences"`
}

// SchemaInspector is an optional read-only schema catalog capability.
type SchemaInspector interface {
	CollectSchema(ctx context.Context, profile config.Profile) (*SchemaSnapshot, error)
}

// CompareSchemaSnapshots produces a deterministic source-to-target catalog diff.
func CompareSchemaSnapshots(source, target *SchemaSnapshot) SchemaDiff {
	var diff SchemaDiff
	sourceTables := make(map[string]SchemaTable, len(source.Tables))
	targetTables := make(map[string]SchemaTable, len(target.Tables))
	for _, table := range source.Tables {
		sourceTables[schemaTableKey(table)] = table
	}
	for _, table := range target.Tables {
		targetTables[schemaTableKey(table)] = table
	}
	for key, sourceTable := range sourceTables {
		targetTable, ok := targetTables[key]
		if !ok {
			diff.MissingTables = append(diff.MissingTables, key)
			continue
		}
		diff.ColumnDifferences = append(diff.ColumnDifferences, compareTableColumns(sourceTable, targetTable)...)
	}
	for key := range targetTables {
		if _, ok := sourceTables[key]; !ok {
			diff.ExtraTables = append(diff.ExtraTables, key)
		}
	}
	sort.Strings(diff.MissingTables)
	sort.Strings(diff.ExtraTables)
	sort.Slice(diff.ColumnDifferences, func(i, j int) bool {
		if diff.ColumnDifferences[i].Table == diff.ColumnDifferences[j].Table {
			return diff.ColumnDifferences[i].Column < diff.ColumnDifferences[j].Column
		}
		return diff.ColumnDifferences[i].Table < diff.ColumnDifferences[j].Table
	})
	return diff
}

func schemaTableKey(table SchemaTable) string { return table.Schema + "." + table.Name }

func compareTableColumns(source, target SchemaTable) []ColumnDifference {
	sourceColumns := make(map[string]SchemaColumn, len(source.Columns))
	targetColumns := make(map[string]SchemaColumn, len(target.Columns))
	for _, column := range source.Columns {
		sourceColumns[column.Name] = column
	}
	for _, column := range target.Columns {
		targetColumns[column.Name] = column
	}
	var differences []ColumnDifference
	for name, sourceColumn := range sourceColumns {
		targetColumn, ok := targetColumns[name]
		if !ok || sourceColumn != targetColumn {
			sourceCopy := sourceColumn
			var targetCopy *SchemaColumn
			if ok {
				targetCopy = &targetColumn
			}
			differences = append(differences, ColumnDifference{Table: schemaTableKey(source), Column: name, Source: &sourceCopy, Target: targetCopy})
		}
	}
	for name, targetColumn := range targetColumns {
		if _, ok := sourceColumns[name]; !ok {
			targetCopy := targetColumn
			differences = append(differences, ColumnDifference{Table: schemaTableKey(source), Column: name, Target: &targetCopy})
		}
	}
	return differences
}
