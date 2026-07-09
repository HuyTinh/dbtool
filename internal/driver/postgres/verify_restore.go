package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"dbtool/internal/driver"
	"dbtool/internal/integrity"

	"github.com/jackc/pgx/v5"
)

const maxVerifyTables = 50

func (d *PostgresDriver) VerifyRestore(ctx context.Context, opts driver.VerifyRestoreOptions) (*driver.VerifyResult, error) {
	result := &driver.VerifyResult{}

	// Step 1: Parse TOC to get expected tables
	var expectedTables []integrity.TOCEntry
	if opts.Format != driver.FormatPlain {
		entries, _, tocErr := integrity.ParseTOC(opts.FilePath)
		if tocErr != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("could not parse TOC for verification: %v", tocErr))
		} else {
			expectedTables = entries.Tables()

			// Apply filters to narrow expected set
			hasFilters := len(opts.IncludeTable) > 0 || len(opts.ExcludeTable) > 0 ||
				len(opts.IncludeSchema) > 0 || len(opts.ExcludeSchema) > 0

			if hasFilters {
				expectedTables = applyFiltersToTables(expectedTables, opts)
			}
		}
	}

	result.TablesExpected = len(expectedTables)

	// Step 2: Connect to target DB
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		opts.Profile.User, opts.Profile.Password,
		opts.Profile.Host, opts.Profile.Port, opts.Profile.Database)

	connCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	conn, err := pgx.Connect(connCtx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to target database for verification: %w", err)
	}
	defer conn.Close(connCtx)

	// Step 3: Table existence check
	actualTables, err := queryActualTables(connCtx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to query target tables: %w", err)
	}

	actualSet := make(map[string]bool)
	for _, t := range actualTables {
		actualSet[t.Schema+"."+t.Name] = true
	}

	if len(expectedTables) > 0 {
		for _, et := range expectedTables {
			key := et.Schema + "." + et.Name
			if actualSet[key] {
				result.TablesFound++
			} else {
				result.MissingTables = append(result.MissingTables, key)
			}
		}

		if len(result.MissingTables) > 0 {
			result.Errors = append(result.Errors,
				fmt.Sprintf("%d table(s) from dump not found in target database", len(result.MissingTables)))
		}
	} else {
		result.TablesFound = len(actualTables)
		result.Warnings = append(result.Warnings,
			"TOC not available — verified table count from target database only")
	}

	// Step 4: Row count + sample query on found tables
	tablesToSample := buildSampleList(expectedTables, actualSet, actualTables)

	for _, t := range tablesToSample {
		fullName := t.Schema + "." + t.Name
		count, err := queryRowCount(connCtx, conn, t.Schema, t.Name)
		if err != nil {
			result.SampleFailed = append(result.SampleFailed, fullName)
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("could not query %s: %v", fullName, err))
			continue
		}

		result.SampleOK = append(result.SampleOK, fullName)
		result.RowCounts = append(result.RowCounts, driver.TableRowCount{
			Schema:   t.Schema,
			Table:    t.Name,
			RowCount: count,
		})

		if count == 0 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s has 0 rows", fullName))
		}
	}

	skipped := result.TablesFound - len(tablesToSample)
	if skipped > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%d additional table(s) not sampled (limit: %d)", skipped, maxVerifyTables))
	}

	// Step 5: Build result
	result.Verified = len(result.Errors) == 0
	return result, nil
}

func queryActualTables(ctx context.Context, conn *pgx.Conn) ([]integrity.TOCEntry, error) {
	query := `
		SELECT table_schema, table_name 
		FROM information_schema.tables 
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema') 
		  AND table_schema NOT LIKE 'pg_toast%%'
		  AND table_type = 'BASE TABLE'
		ORDER BY table_schema, table_name
	`
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []integrity.TOCEntry
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			return nil, err
		}
		tables = append(tables, integrity.TOCEntry{
			Type:   "TABLE",
			Schema: schema,
			Name:   name,
		})
	}
	return tables, rows.Err()
}

func queryRowCount(ctx context.Context, conn *pgx.Conn, schema, table string) (int64, error) {
	sampleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := fmt.Sprintf(`SELECT count(*) FROM "%s"."%s"`, schema, table)
	var count int64
	err := conn.QueryRow(sampleCtx, query).Scan(&count)
	if err != nil {
		if sampleCtx.Err() != nil {
			return 0, fmt.Errorf("query timeout (table may be very large)")
		}
		return 0, err
	}
	return count, nil
}

func applyFiltersToTables(tables []integrity.TOCEntry, opts driver.VerifyRestoreOptions) []integrity.TOCEntry {
	allEntries := make(integrity.TOCEntryList, len(tables))
	copy(allEntries, tables)

	fm := integrity.MatchFilters(allEntries,
		opts.IncludeTable, opts.ExcludeTable,
		opts.IncludeSchema, opts.ExcludeSchema)

	matchedSet := make(map[string]bool)
	for _, p := range fm.IncludeTableMatched {
		matchedSet[p] = true
	}

	// If include filters specified, only keep tables that match
	if len(opts.IncludeTable) > 0 || len(opts.IncludeSchema) > 0 {
		var filtered []integrity.TOCEntry
		for _, t := range tables {
			re := buildTableMatcher(opts.IncludeTable, opts.IncludeSchema)
			if re(t) {
				filtered = append(filtered, t)
			}
		}
		tables = filtered
	}

	// Apply exclude filters
	if len(opts.ExcludeTable) > 0 || len(opts.ExcludeSchema) > 0 {
		var filtered []integrity.TOCEntry
		for _, t := range tables {
			re := buildTableMatcher(opts.ExcludeTable, opts.ExcludeSchema)
			if !re(t) {
				filtered = append(filtered, t)
			}
		}
		tables = filtered
	}

	return tables
}

func buildTableMatcher(tablePats, schemaPats []string) func(integrity.TOCEntry) bool {
	return func(t integrity.TOCEntry) bool {
		for _, pat := range tablePats {
			re := patternToFilterRegex(pat)
			if re != nil {
				fullName := t.Schema + "." + t.Name
				if re.MatchString(fullName) || re.MatchString(t.Name) {
					return true
				}
			}
		}
		for _, pat := range schemaPats {
			re := schemaToFilterRegex(pat)
			if re != nil && re.MatchString(t.Schema) {
				return true
			}
		}
		return false
	}
}

func buildSampleList(expected []integrity.TOCEntry, actualSet map[string]bool, actual []integrity.TOCEntry) []integrity.TOCEntry {
	var toSample []integrity.TOCEntry

	if len(expected) > 0 {
		for _, t := range expected {
			key := t.Schema + "." + t.Name
			if actualSet[key] && len(toSample) < maxVerifyTables {
				toSample = append(toSample, t)
			}
		}
	} else {
		for _, t := range actual {
			if len(toSample) >= maxVerifyTables {
				break
			}
			toSample = append(toSample, t)
		}
	}

	return toSample
}

// patternToFilterRegex reuses the same wildcard logic from integrity/filter.go
func patternToFilterRegex(pattern string) *regexp.Regexp {
	p := strings.Trim(pattern, `"`)

	var schemaPat, tablePat string
	if idx := strings.Index(p, "."); idx >= 0 {
		schemaPat = p[:idx]
		tablePat = p[idx+1:]
	} else {
		tablePat = p
	}

	toRegexp := func(s string) string {
		s = strings.Trim(s, `"`)
		var b strings.Builder
		for _, c := range s {
			switch c {
			case '*':
				b.WriteString(".*")
			case '?':
				b.WriteString(".")
			default:
				b.WriteString(regexp.QuoteMeta(string(c)))
			}
		}
		return b.String()
	}

	var full string
	if schemaPat != "" {
		full = "^" + toRegexp(schemaPat) + `\.` + toRegexp(tablePat) + "$"
	} else {
		full = "^" + toRegexp(tablePat) + "$"
	}

	re, err := regexp.Compile(full)
	if err != nil {
		return nil
	}
	return re
}

func schemaToFilterRegex(pattern string) *regexp.Regexp {
	p := strings.Trim(pattern, `"`)
	if idx := strings.Index(p, "."); idx >= 0 {
		p = p[:idx]
	}

	var b strings.Builder
	b.WriteString("^")
	for _, c := range p {
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")

	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}
