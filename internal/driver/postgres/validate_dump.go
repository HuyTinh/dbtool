package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dbtool/internal/driver"
	"dbtool/internal/integrity"

	"github.com/jackc/pgx/v5"
)

func (d *PostgresDriver) ValidateDump(ctx context.Context, opts driver.ValidateOptions) (*driver.ValidationResult, error) {
	result := &driver.ValidationResult{
		SchemaCompatible: true,
	}

	// Check file size
	size, err := integrity.FileSize(opts.FilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot check dump file: %w", err)
	}
	if size == 0 {
		result.Errors = append(result.Errors, "dump file is empty (0 bytes)")
		return result, nil
	}

	// Parse TOC
	if opts.Format != driver.FormatPlain {
		entries, dumpVersion, tocErr := integrity.ParseTOC(opts.FilePath)
		if tocErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not parse TOC: %v", tocErr))
		} else {
			result.HasTOC = true
			result.DumpPGVersion = dumpVersion
			tables := entries.Tables()
			views := entries.Views()
			result.TableCount = len(tables)
			result.ViewCount = len(views)
			result.SchemaNames = entries.Schemas()

			if result.TableCount == 0 && result.ViewCount == 0 {
				result.Warnings = append(result.Warnings, "dump contains no tables or views")
			}

			// Filter pre-flight
			hasFilters := len(opts.IncludeTable) > 0 || len(opts.ExcludeTable) > 0 ||
				len(opts.IncludeSchema) > 0 || len(opts.ExcludeSchema) > 0

			if hasFilters {
				fm := integrity.MatchFilters(entries,
					opts.IncludeTable, opts.ExcludeTable,
					opts.IncludeSchema, opts.ExcludeSchema)
				result.FilterMatches = driver.FilterMatchResult{
					IncludeTableMatched:    fm.IncludeTableMatched,
					IncludeTableUnmatched:  fm.IncludeTableUnmatched,
					ExcludeTableMatched:    fm.ExcludeTableMatched,
					ExcludeTableUnmatched:  fm.ExcludeTableUnmatched,
					IncludeSchemaMatched:   fm.IncludeSchemaMatched,
					IncludeSchemaUnmatched: fm.IncludeSchemaUnmatched,
					ExcludeSchemaMatched:   fm.ExcludeSchemaMatched,
					ExcludeSchemaUnmatched: fm.ExcludeSchemaUnmatched,
				}

				for _, pat := range fm.IncludeTableUnmatched {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("--include-table %q did not match any table in the dump", pat))
				}
				for _, pat := range fm.ExcludeTableUnmatched {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("--exclude-table %q did not match any table in the dump", pat))
				}
				for _, pat := range fm.IncludeSchemaUnmatched {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("--include-schema %q did not match any schema in the dump", pat))
				}
				for _, pat := range fm.ExcludeSchemaUnmatched {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("--exclude-schema %q did not match any schema in the dump", pat))
				}
			}
		}
	}

	// Schema compatibility check via target DB connection
	targetVersion := getTargetVersion(ctx, opts.Profile.Host, opts.Profile.Port,
		opts.Profile.User, opts.Profile.Password, opts.Profile.Database)
	if targetVersion != "" {
		result.TargetPGVersion = targetVersion
		if result.DumpPGVersion != "" {
			dumpMajor := majorVersion(result.DumpPGVersion)
			targetMajor := majorVersion(targetVersion)
			if dumpMajor > targetMajor {
				result.SchemaCompatible = false
				result.Errors = append(result.Errors,
					fmt.Sprintf("dump was created with PostgreSQL %s but target is %s — restore may fail",
						result.DumpPGVersion, targetVersion))
			}
		}
	} else {
		result.Warnings = append(result.Warnings, "could not connect to target database to check version compatibility")
	}

	return result, nil
}

func getTargetVersion(ctx context.Context, host string, port int, user, password, database string) string {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?connect_timeout=3",
		user, password, host, port, database)
	connCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(connCtx, dsn)
	if err != nil {
		return ""
	}
	defer conn.Close(connCtx)

	var version string
	if err := conn.QueryRow(connCtx, "SHOW server_version").Scan(&version); err != nil {
		return ""
	}
	return version
}

func majorVersion(version string) int {
	parts := strings.SplitN(version, ".", 2)
	if len(parts) == 0 {
		return 0
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return n
}
