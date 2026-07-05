package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"
	"dbtool/internal/driver/postgres"
	"dbtool/internal/history"
	"dbtool/internal/safety"

	"github.com/gen2brain/beeep"
	"github.com/spf13/cobra"
)

var (
	migrateFrom            string
	migrateTo              string
	migrateFormat          string
	migrateSchemaOnly      bool
	migrateDataOnly        bool
	migrateClean           bool
	migrateCreateIfMissing bool
	migrateJobs            int
	migrateKeepTemp        bool
	migrateIncludeTable    []string
	migrateExcludeTable    []string
	migrateIncludeSchema   []string
	migrateExcludeSchema   []string
	migrateDryRun          bool
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate database schema and data from one profile to another",
	RunE: func(cmd *cobra.Command, args []string) error {
		return ExecuteMigrateLogic(cmd.Context(), MigrateOptions{
			FromName:        migrateFrom,
			ToName:          migrateTo,
			Format:          driver.Format(migrateFormat),
			SchemaOnly:      migrateSchemaOnly,
			DataOnly:        migrateDataOnly,
			Clean:           migrateClean,
			CreateIfMissing: migrateCreateIfMissing,
			Jobs:            migrateJobs,
			KeepTemp:        migrateKeepTemp,
			IncludeTable:    migrateIncludeTable,
			ExcludeTable:    migrateExcludeTable,
			IncludeSchema:   migrateIncludeSchema,
			ExcludeSchema:   migrateExcludeSchema,
			DryRun:          migrateDryRun,
		})
	},
}

// MigrateOptions configures a migrate operation (source -> destination).
type MigrateOptions struct {
	FromName        string
	ToName          string
	Format          driver.Format
	SchemaOnly      bool
	DataOnly        bool
	Clean           bool
	CreateIfMissing bool
	Jobs            int
	KeepTemp        bool
	IncludeTable    []string
	ExcludeTable    []string
	IncludeSchema   []string
	ExcludeSchema   []string
	DryRun          bool
}

func ExecuteMigrateLogic(ctx context.Context, opts MigrateOptions) error {
	for _, pat := range opts.IncludeTable {
		if err := postgres.ValidateTablePattern(pat); err != nil {
			return err
		}
	}
	for _, pat := range opts.ExcludeTable {
		if err := postgres.ValidateTablePattern(pat); err != nil {
			return err
		}
	}
	for _, pat := range opts.IncludeSchema {
		if err := postgres.ValidateTablePattern(pat); err != nil {
			return err
		}
	}
	for _, pat := range opts.ExcludeSchema {
		if err := postgres.ValidateTablePattern(pat); err != nil {
			return err
		}
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	src, ok := cfg.GetProfile(opts.FromName)
	if !ok {
		return fmt.Errorf("source profile %q not found", opts.FromName)
	}
	dst, ok := cfg.GetProfile(opts.ToName)
	if !ok {
		return fmt.Errorf("destination profile %q not found", opts.ToName)
	}
	if opts.FromName == opts.ToName {
		return fmt.Errorf("source and destination profiles must be different")
	}
	if src.Driver != dst.Driver {
		return fmt.Errorf("cross-driver migration is not supported (source: %s, dest: %s)", src.Driver, dst.Driver)
	}

	drv, err := driver.Get(src.Driver)
	if err != nil {
		return err
	}

	format := opts.Format
	if format == "" {
		format = driver.FormatCustom
	}

	fmt.Printf("Migrating: %s (%s:%d/%s) -> %s (%s:%d/%s)\n",
		opts.FromName, src.Host, src.Port, src.Database,
		opts.ToName, dst.Host, dst.Port, dst.Database)
	fmt.Println()

	if opts.DryRun {
		fmt.Println("Migration plan (dry-run):")
		fmt.Printf("  Source: %s:%d/%s (driver: %s)\n", src.Host, src.Port, src.Database, src.Driver)
		fmt.Printf("  Target: %s:%d/%s (driver: %s)\n", dst.Host, dst.Port, dst.Database, dst.Driver)
		fmt.Printf("  Format: %s\n", format)
		if opts.SchemaOnly {
			fmt.Println("  Mode:   schema-only")
		}
		if opts.DataOnly {
			fmt.Println("  Mode:   data-only")
		}
		fmt.Printf("  Clean: %v   Create-if-missing: %v   Jobs: %d\n", opts.Clean, opts.CreateIfMissing, opts.Jobs)
		fmt.Println("\nWould run:")
		fmt.Println("  1. pg_dump (source) -> temp file")
		fmt.Println("  2. pg_restore (target) <- temp file")
		fmt.Println("\n(Nothing will be executed. Remove --dry-run to run.)")
		return nil
	}

	tempPath, err := createTempDumpPath(format)
	if err != nil {
		return err
	}
	if !opts.KeepTemp {
		defer os.RemoveAll(tempPath)
	}

	// === Phase 1: Dump from source ===
	fmt.Println("[1/2] Dumping from source...")

	dumpOpts := driver.DumpOptions{
		Profile:       src,
		FilePath:      tempPath,
		Format:        format,
		SchemaOnly:    opts.SchemaOnly,
		DataOnly:      opts.DataOnly,
		IncludeTable:  opts.IncludeTable,
		ExcludeTable:  opts.ExcludeTable,
		IncludeSchema: opts.IncludeSchema,
		ExcludeSchema: opts.ExcludeSchema,
	}

	dumpProg, err := drv.Dump(ctx, dumpOpts)
	if err != nil {
		return err
	}
	var dumpLast driver.Progress
	for p := range dumpProg {
		dumpLast = p
		if !Quiet {
			if Verbose || p.Err != nil || p.Percent == 100 {
				fmt.Println(p.Message)
			}
		}
	}

	dumpCmdStr := fmt.Sprintf("pg_dump -h %s -p %d -U %s -d %s -f %s -F %s",
		src.Host, src.Port, src.User, src.Database, tempPath, format)

	if dumpLast.Err != nil {
		_ = history.AppendHistory(history.HistoryRecord{
			File:    tempPath,
			Profile: src.Name,
			Time:    time.Now(),
			Success: false,
			Command: "migrate(dump): " + dumpCmdStr,
			Error:   dumpLast.Err.Error(),
		})
		_ = beeep.Notify("DBTool Migrate Failed", fmt.Sprintf("Dump phase: %v", dumpLast.Err), "")
		return fmt.Errorf("dump phase failed: %w", dumpLast.Err)
	}

	_ = history.AppendHistory(history.HistoryRecord{
		File:    tempPath,
		Profile: src.Name,
		Time:    time.Now(),
		Success: true,
		Command: "migrate(dump): " + dumpCmdStr,
	})

	// === Safety check on target ===
	fmt.Println()
	fmt.Println("Checking target database...")

	hasData, err := safety.CheckDatabaseHasData(ctx, dst.Host, dst.Port, dst.User, dst.Password, dst.Database)
	if err != nil {
		fmt.Printf("Warning: safety check could not inspect target database: %v\n", err)
		hasData = true
	}
	if hasData {
		if opts.Clean {
			fmt.Fprintln(os.Stderr, "\033[1;31m⚠️ WARNING: --clean will DROP all objects in the target database before restore!\033[0m")
		}
		prompt := fmt.Sprintf("Target database '%s' at %s:%d already has data. Overwrite?", dst.Database, dst.Host, dst.Port)
		if !safety.PromptConfirm(prompt) {
			fmt.Println("Migration cancelled by user.")
			return nil
		}
	}

	if opts.CreateIfMissing {
		if err := drv.EnsureDatabaseExists(ctx, dst); err != nil {
			return fmt.Errorf("failed to ensure target database exists: %w", err)
		}
	}

	// === Phase 2: Restore into target ===
	fmt.Println()
	fmt.Println("[2/2] Restoring into target...")

	detectedFormat, err := drv.DetectFormat(tempPath)
	if err != nil || detectedFormat == driver.FormatUnknown {
		detectedFormat = format
	}

	restoreOpts := driver.RestoreOptions{
		Profile:         dst,
		FilePath:        tempPath,
		Format:          detectedFormat,
		Jobs:            opts.Jobs,
		Clean:           opts.Clean,
		IncludeTable:    opts.IncludeTable,
		ExcludeTable:    opts.ExcludeTable,
		IncludeSchema:   opts.IncludeSchema,
		ExcludeSchema:   opts.ExcludeSchema,
		CreateIfMissing: opts.CreateIfMissing,
	}

	restoreProg, err := drv.Restore(ctx, restoreOpts)
	if err != nil {
		return err
	}
	var restoreLast driver.Progress
	for p := range restoreProg {
		restoreLast = p
		if !Quiet {
			if Verbose || p.Err != nil || p.Percent == 100 {
				fmt.Println(p.Message)
			}
		}
	}

	restoreCmdStr := getDryRunCommand(restoreOpts)

	if restoreLast.Err != nil {
		_ = history.AppendHistory(history.HistoryRecord{
			File:    tempPath,
			Profile: dst.Name,
			Time:    time.Now(),
			Success: false,
			Command: "migrate(restore): " + restoreCmdStr,
			Error:   restoreLast.Err.Error(),
		})
		_ = beeep.Notify("DBTool Migrate Failed", fmt.Sprintf("Restore phase: %v", restoreLast.Err), "")
		return fmt.Errorf("restore phase failed: %w", restoreLast.Err)
	}

	_ = history.AppendHistory(history.HistoryRecord{
		File:    tempPath,
		Profile: dst.Name,
		Time:    time.Now(),
		Success: true,
		Command: "migrate(restore): " + restoreCmdStr,
	})

	_ = beeep.Notify("DBTool Migrate Success",
		fmt.Sprintf("%s -> %s migrated successfully", src.Database, dst.Database), "")

	fmt.Println()
	fmt.Println("✓ Migration completed successfully.")
	if opts.KeepTemp {
		fmt.Printf("Temp dump kept at: %s\n", tempPath)
	}
	return nil
}

// createTempDumpPath allocates a temp path for the intermediate dump.
// For directory format it returns a directory; otherwise a file path.
func createTempDumpPath(format driver.Format) (string, error) {
	pattern := "dbtool-migrate-*.dump"
	if format == driver.FormatDirectory {
		pattern = "dbtool-migrate-*"
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temp dump file: %w", err)
	}
	name := f.Name()
	f.Close()
	if format == driver.FormatDirectory {
		_ = os.Remove(name)
		if err := os.MkdirAll(name, 0755); err != nil {
			return "", fmt.Errorf("failed to create temp dump directory: %w", err)
		}
	}
	return name, nil
}

func init() {
	migrateCmd.Flags().StringVar(&migrateFrom, "from", "", "Source profile name")
	migrateCmd.Flags().StringVar(&migrateTo, "to", "", "Destination profile name")
	migrateCmd.Flags().StringVar(&migrateFormat, "format", "custom", "Intermediate dump format (custom, plain, directory)")
	migrateCmd.Flags().BoolVar(&migrateSchemaOnly, "schema-only", false, "Migrate schema only (no data)")
	migrateCmd.Flags().BoolVar(&migrateDataOnly, "data-only", false, "Migrate data only (no schema)")
	migrateCmd.Flags().BoolVar(&migrateClean, "clean", false, "Drop target objects before restore")
	migrateCmd.Flags().BoolVar(&migrateCreateIfMissing, "create-if-missing", false, "Create the target database if it does not exist")
	migrateCmd.Flags().IntVarP(&migrateJobs, "jobs", "j", defaultJobs(), "Parallel restore jobs (directory format only)")
	migrateCmd.Flags().BoolVar(&migrateKeepTemp, "keep-temp", false, "Keep the intermediate dump file after migration")
	migrateCmd.Flags().StringSliceVar(&migrateIncludeTable, "include-table", nil, "Include specific table (can be repeated)")
	migrateCmd.Flags().StringSliceVar(&migrateExcludeTable, "exclude-table", nil, "Exclude specific table (can be repeated)")
	migrateCmd.Flags().StringSliceVar(&migrateIncludeSchema, "include-schema", nil, "Include specific schema (can be repeated)")
	migrateCmd.Flags().StringSliceVar(&migrateExcludeSchema, "exclude-schema", nil, "Exclude specific schema (can be repeated)")
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "Show what would run without executing")

	_ = migrateCmd.MarkFlagRequired("from")
	_ = migrateCmd.MarkFlagRequired("to")

	RootCmd.AddCommand(migrateCmd)
}
