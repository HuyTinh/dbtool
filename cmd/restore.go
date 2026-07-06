package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"
	"dbtool/internal/driver/postgres"
	"dbtool/internal/history"
	"dbtool/internal/integrity"
	"dbtool/internal/safety"

	"github.com/gen2brain/beeep"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var (
	restoreProfile         string
	restoreFormat          string
	restoreJobs            int
	restoreClean           bool
	restoreDryRun          bool
	restoreCreateIfMissing bool
	restoreOptimize        bool
	restoreSkipChecksum    bool
	restoreVerify          bool
	restoreVerifyPost      bool
	includeTable           []string
	excludeTable           []string
	includeSchema          []string
	excludeSchema          []string
)

func ExecuteRestoreLogic(ctx context.Context, filePath string) error {
	// 1. Verify file exists and is readable early
	if err := validateDumpFile(filePath); err != nil {
		return err
	}

	// 1.5 Verify dump file integrity via checksum sidecar
	if !restoreSkipChecksum {
		stored, _ := integrity.ReadChecksumFile(filePath)
		if stored == "" {
			fmt.Println("No checksum sidecar found, skipping integrity verification.")
		} else {
			fmt.Print("Verifying dump file integrity... ")
			ok, err := integrity.VerifyChecksum(filePath)
			if err != nil {
				fmt.Println("FAILED")
				fmt.Fprintf(os.Stderr, "⚠ %v\n", err)
				if !safety.PromptConfirm("Checksum verification failed. Continue restore anyway?") {
					fmt.Println("Operation cancelled by user.")
					return nil
				}
			} else if ok {
				fmt.Printf("OK (%s)\n", stored[:12]+"...")
			}
		}
	}

	// 2. Validate filter patterns early
	for _, pat := range includeTable {
		if err := postgres.ValidateTablePattern(pat); err != nil {
			return err
		}
	}
	for _, pat := range excludeTable {
		if err := postgres.ValidateTablePattern(pat); err != nil {
			return err
		}
	}
	for _, pat := range includeSchema {
		if err := postgres.ValidateTablePattern(pat); err != nil {
			return err
		}
	}
	for _, pat := range excludeSchema {
		if err := postgres.ValidateTablePattern(pat); err != nil {
			return err
		}
	}

	// 3. Load profile configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	profile, ok := cfg.GetProfile(restoreProfile)
	if !ok {
		return fmt.Errorf("profile %q not found in configurations", restoreProfile)
	}

	// 4. Retrieve database driver from registry
	drv, err := driver.Get(profile.Driver)
	if err != nil {
		return err
	}

	// 5. Detect file format
	detectedFormat, err := drv.DetectFormat(filePath)
	if err != nil {
		return fmt.Errorf("failed to detect dump format: %w", err)
	}

	fmt.Printf("Detected dump format: %s\n", detectedFormat)

	// Set default format if not provided
	finalFormat := driver.Format(restoreFormat)
	if finalFormat == "" || finalFormat == "auto" {
		finalFormat = detectedFormat
	}

	opts := driver.RestoreOptions{
		Profile:         profile,
		FilePath:        filePath,
		Format:          finalFormat,
		Jobs:            restoreJobs,
		Clean:           restoreClean,
		IncludeTable:    includeTable,
		ExcludeTable:    excludeTable,
		IncludeSchema:   includeSchema,
		ExcludeSchema:   excludeSchema,
		DryRun:          restoreDryRun,
		CreateIfMissing: restoreCreateIfMissing,
	}

	cmdString := getDryRunCommand(opts)

	// 5.5 Handle --verify (pre-restore validation only)
	if restoreVerify {
		fmt.Println("\nVerifying dump file...")
		valResult, valErr := drv.ValidateDump(ctx, driver.ValidateOptions{
			FilePath:      filePath,
			Format:        finalFormat,
			Profile:       profile,
			IncludeTable:  includeTable,
			ExcludeTable:  excludeTable,
			IncludeSchema: includeSchema,
			ExcludeSchema: excludeSchema,
		})
		if valErr != nil {
			return fmt.Errorf("validation failed: %w", valErr)
		}
		printValidationResult(valResult)
		if len(valResult.Errors) > 0 {
			return fmt.Errorf("dump validation found %d error(s)", len(valResult.Errors))
		}
		return nil
	}

	// 6. Handle Dry Run
	if restoreDryRun {
		if restoreClean {
			fmt.Fprintln(os.Stderr, "\033[1;31m⚠️ CẢNH BÁO: Lệnh này sẽ XÓA TOÀN BỘ object cũ trong schema của DB đích trước khi restore! (--clean)")
			fmt.Fprintln(os.Stderr, "   Xem mục 14.3 để biết rủi ro khi DB đích dùng chung schema cho nhiều app.\033[0m")
		}

		fmt.Println("Sẽ chạy:")
		fmt.Printf("  %s\n\n", cmdString)

		info, err := os.Stat(filePath)
		fileSizeStr := "unknown"
		if err == nil {
			fileSizeStr = formatBytes(info.Size())
		}

		fmt.Printf("Profile:     %s (%s @ %s:%d/%s)\n", profile.Name, profile.Driver, profile.Host, profile.Port, profile.Database)
		fmt.Printf("File:        %s (%s, format: %s)\n", filePath, fileSizeStr, finalFormat)
		fmt.Printf("Jobs:        %d\n", restoreJobs)
		cleanStr := "không"
		if restoreClean {
			cleanStr = "có"
		}
		fmt.Printf("Clean:       %s\n", cleanStr)
		fmt.Println("\n(Không có gì được thực thi. Bỏ --dry-run để chạy thật.)")
		return nil
	}

	// 6.5 Ensure database exists if requested
	if restoreCreateIfMissing && !restoreDryRun {
		fmt.Printf("Checking and ensuring target database '%s' exists...\n", profile.Database)
		if err := drv.EnsureDatabaseExists(ctx, profile); err != nil {
			return fmt.Errorf("failed to ensure database existence: %w", err)
		}
	}

	// 7. Safety Validation
	hasData, err := safety.CheckDatabaseHasData(ctx, profile.Host, profile.Port, profile.User, profile.Password, profile.Database)
	if err != nil {
		// If target DB check fails, warn but proceed with safety prompt
		fmt.Printf("Warning: safety check could not inspect target database content: %v\n", err)
		hasData = true
	}

	if hasData {
		if restoreClean {
			fmt.Fprintln(os.Stderr, "\033[1;31m⚠️ CẢNH BÁO: Lệnh này sẽ XÓA TOÀN BỘ object cũ trong schema của DB đích trước khi restore!\033[0m")
		}
		prompt := fmt.Sprintf("CSDL '%s' tại %s:%d đã có sẵn dữ liệu. Bạn có chắc chắn muốn ghi đè?", profile.Database, profile.Host, profile.Port)
		if !safety.PromptConfirm(prompt) {
			fmt.Println("Operation cancelled by user.")
			return nil
		}
	}

	// 8. Attach Execution Timeout Context
	// We create a root command object internally just to parse the timeout config safely
	tempCmd := &cobra.Command{}
	runCtx, cancel, err := ContextWithTimeout(tempCmd, ctx)
	if err != nil {
		return err
	}
	defer cancel()

	// 9. Execute Restore Subprocess
	progressChan, err := drv.Restore(runCtx, opts)
	if err != nil {
		return err
	}

	var bar *progressbar.ProgressBar
	if !Quiet && !Verbose {
		bar = progressbar.NewOptions(100,
			progressbar.OptionSetDescription("Restoring database"),
			progressbar.OptionSetWriter(os.Stdout),
			progressbar.OptionSetWidth(15),
			progressbar.OptionThrottle(100*time.Millisecond),
			progressbar.OptionShowCount(),
			progressbar.OptionOnCompletion(func() {
				fmt.Println()
			}),
		)
	}

	fmt.Println("Starting restore operation...")
	var lastProgress driver.Progress
	for p := range progressChan {
		lastProgress = p
		if !Quiet {
			if Verbose || p.Err != nil || strings.Contains(p.Message, "[Database Error]") || p.Percent == 100 {
				if bar != nil {
					_ = bar.Clear()
				}
				fmt.Println(p.Message)
			} else {
				if bar != nil {
					_ = bar.Set(int(p.Percent))
				} else {
					fmt.Printf("\r%-100s", p.Message)
				}
			}
		}
	}
	if bar != nil {
		_ = bar.Finish()
	}
	fmt.Println()

	// 10. Record History Log
	historyRec := history.HistoryRecord{
		File:    filePath,
		Profile: profile.Name,
		Time:    time.Now(),
		Success: lastProgress.Err == nil,
		Command: cmdString,
	}
	if lastProgress.Err != nil {
		historyRec.Error = lastProgress.Err.Error()
	}
	_ = history.AppendHistory(historyRec)

	// Desktop Notification
	if lastProgress.Err != nil {
		_ = beeep.Notify("DBTool Restore Failed", fmt.Sprintf("Profile: %s\nError: %v", profile.Name, lastProgress.Err), "")
		return lastProgress.Err
	}
	_ = beeep.Notify("DBTool Restore Success", fmt.Sprintf("Database %s restored successfully", profile.Database), "")

	fmt.Println("✓ Database restore completed successfully.")

	// Post-restore optimization
	if restoreOptimize {
		fmt.Println("Running VACUUM ANALYZE to optimize database...")
		if err := drv.Optimize(runCtx, profile); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ Warning: optimization failed (restore was successful): %v\n", err)
		} else {
			fmt.Println("✓ Database optimization completed.")
		}
	}

	// Post-restore verification
	if restoreVerifyPost {
		fmt.Println("\nVerifying restored database...")
		verifyResult, verifyErr := drv.VerifyRestore(runCtx, driver.VerifyRestoreOptions{
			Profile:       profile,
			FilePath:      filePath,
			Format:        finalFormat,
			IncludeTable:  includeTable,
			ExcludeTable:  excludeTable,
			IncludeSchema: includeSchema,
			ExcludeSchema: excludeSchema,
		})
		if verifyErr != nil {
			fmt.Fprintf(os.Stderr, "⚠ Warning: post-restore verification failed: %v\n", verifyErr)
		} else {
			printVerifyResult(verifyResult)
			if !verifyResult.Verified {
				return fmt.Errorf("post-restore verification found issues")
			}
		}
	}

	return nil
}

var restoreCmd = &cobra.Command{
	Use:   "restore [file_or_directory]",
	Short: "Restore a database from a dump archive or file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return ExecuteRestoreLogic(cmd.Context(), args[0])
	},
}

func validateDumpFile(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("dump file not found: %s", path)
	}
	if err != nil {
		return fmt.Errorf("failed to access dump file %s: %w", path, err)
	}
	if info.IsDir() {
		return nil // valid directory dump
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot read dump file %s (check file permissions): %w", path, err)
	}
	f.Close()
	return nil
}

func defaultJobs() int {
	n := runtime.NumCPU()
	if n > 4 {
		return 4
	}
	return n
}



func getDryRunCommand(opts driver.RestoreOptions) string {
	if opts.Format == driver.FormatPlain {
		return fmt.Sprintf("psql -h %s -p %d -U %s -d %s -f %s",
			opts.Profile.Host, opts.Profile.Port, opts.Profile.User, opts.Profile.Database, opts.FilePath)
	}
	cleanFlag := ""
	if opts.Clean {
		cleanFlag = " --clean --if-exists"
	}
	jobsFlag := ""
	if opts.Format == driver.FormatDirectory && opts.Jobs > 1 {
		jobsFlag = fmt.Sprintf(" -j %d", opts.Jobs)
	}
	filterFlags := ""
	for _, t := range opts.IncludeTable {
		filterFlags += " -t " + t
	}
	for _, t := range opts.ExcludeTable {
		filterFlags += " -T " + t
	}
	for _, s := range opts.IncludeSchema {
		filterFlags += " -n " + s
	}
	for _, s := range opts.ExcludeSchema {
		filterFlags += " -N " + s
	}
	return fmt.Sprintf("pg_restore -h %s -p %d -U %s -d %s%s%s%s -v %s",
		opts.Profile.Host, opts.Profile.Port, opts.Profile.User, opts.Profile.Database, jobsFlag, cleanFlag, filterFlags, opts.FilePath)
}

func printValidationResult(r *driver.ValidationResult) {
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("Dump Validation Report")
	fmt.Println(strings.Repeat("=", 50))

	if r.HasTOC {
		fmt.Printf("  Tables:  %d\n", r.TableCount)
		fmt.Printf("  Views:   %d\n", r.ViewCount)
		if len(r.SchemaNames) > 0 {
			fmt.Printf("  Schemas: %s\n", strings.Join(r.SchemaNames, ", "))
		}
	} else {
		fmt.Println("  TOC: not available (plain text format or parse error)")
	}

	if r.DumpPGVersion != "" {
		fmt.Printf("  Dump PG version:   %s\n", r.DumpPGVersion)
	}
	if r.TargetPGVersion != "" {
		fmt.Printf("  Target PG version: %s\n", r.TargetPGVersion)
	}
	if r.DumpPGVersion != "" && r.TargetPGVersion != "" {
		if r.SchemaCompatible {
			fmt.Println("  Version compatibility: OK")
		} else {
			fmt.Println("  Version compatibility: INCOMPATIBLE")
		}
	}

	hasFilters := len(r.FilterMatches.IncludeTableMatched) > 0 ||
		len(r.FilterMatches.IncludeTableUnmatched) > 0 ||
		len(r.FilterMatches.ExcludeTableMatched) > 0 ||
		len(r.FilterMatches.ExcludeTableUnmatched) > 0 ||
		len(r.FilterMatches.IncludeSchemaMatched) > 0 ||
		len(r.FilterMatches.IncludeSchemaUnmatched) > 0 ||
		len(r.FilterMatches.ExcludeSchemaMatched) > 0 ||
		len(r.FilterMatches.ExcludeSchemaUnmatched) > 0

	if hasFilters {
		fmt.Println("\n  Filter pre-flight:")
		for _, p := range r.FilterMatches.IncludeTableMatched {
			fmt.Printf("    --include-table %s  -> matched\n", p)
		}
		for _, p := range r.FilterMatches.IncludeTableUnmatched {
			fmt.Printf("    --include-table %s  -> NO MATCH\n", p)
		}
		for _, p := range r.FilterMatches.ExcludeTableMatched {
			fmt.Printf("    --exclude-table %s  -> matched\n", p)
		}
		for _, p := range r.FilterMatches.ExcludeTableUnmatched {
			fmt.Printf("    --exclude-table %s  -> NO MATCH\n", p)
		}
		for _, p := range r.FilterMatches.IncludeSchemaMatched {
			fmt.Printf("    --include-schema %s  -> matched\n", p)
		}
		for _, p := range r.FilterMatches.IncludeSchemaUnmatched {
			fmt.Printf("    --include-schema %s  -> NO MATCH\n", p)
		}
		for _, p := range r.FilterMatches.ExcludeSchemaMatched {
			fmt.Printf("    --exclude-schema %s  -> matched\n", p)
		}
		for _, p := range r.FilterMatches.ExcludeSchemaUnmatched {
			fmt.Printf("    --exclude-schema %s  -> NO MATCH\n", p)
		}
	}

	if len(r.Warnings) > 0 {
		fmt.Println("\n  Warnings:")
		for _, w := range r.Warnings {
			fmt.Printf("    - %s\n", w)
		}
	}
	if len(r.Errors) > 0 {
		fmt.Println("\n  Errors:")
		for _, e := range r.Errors {
			fmt.Printf("    - %s\n", e)
		}
	}

	fmt.Println(strings.Repeat("=", 50))
	if len(r.Errors) == 0 && len(r.Warnings) == 0 {
		fmt.Println("Dump is valid for restore.")
	} else if len(r.Errors) == 0 {
		fmt.Println("Dump is valid with warnings.")
	}
}

func printVerifyResult(r *driver.VerifyResult) {
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("Post-restore Verification Report")
	fmt.Println(strings.Repeat("=", 50))

	if r.TablesExpected > 0 {
		fmt.Printf("  Tables expected: %d\n", r.TablesExpected)
	}
	fmt.Printf("  Tables found:    %d\n", r.TablesFound)

	if len(r.MissingTables) > 0 {
		fmt.Println("\n  Missing tables:")
		for _, t := range r.MissingTables {
			fmt.Printf("    - %s\n", t)
		}
	}

	if len(r.RowCounts) > 0 {
		fmt.Printf("\n  Row counts (%d tables sampled):\n", len(r.RowCounts))
		for _, rc := range r.RowCounts {
			fmt.Printf("    %s.%s: %d rows\n", rc.Schema, rc.Table, rc.RowCount)
		}
	}

	if len(r.SampleFailed) > 0 {
		fmt.Println("\n  Sample query failed:")
		for _, t := range r.SampleFailed {
			fmt.Printf("    - %s\n", t)
		}
	}

	if len(r.Warnings) > 0 {
		fmt.Println("\n  Warnings:")
		for _, w := range r.Warnings {
			fmt.Printf("    - %s\n", w)
		}
	}
	if len(r.Errors) > 0 {
		fmt.Println("\n  Errors:")
		for _, e := range r.Errors {
			fmt.Printf("    - %s\n", e)
		}
	}

	fmt.Println(strings.Repeat("=", 50))
	if r.Verified {
		fmt.Println("✓ Post-restore verification passed.")
	} else {
		fmt.Println("✗ Post-restore verification found issues.")
	}
}

func init() {
	restoreCmd.Flags().StringVar(&restoreProfile, "profile", "", "Profile name to restore into")
	restoreCmd.Flags().StringVar(&restoreFormat, "format", "auto", "Format of dump file (auto, custom, plain, directory)")
	restoreCmd.Flags().IntVarP(&restoreJobs, "jobs", "j", defaultJobs(), "Number of parallel restore jobs")
	restoreCmd.Flags().BoolVar(&restoreClean, "clean", false, "Clean (drop) database objects before recreating")
	restoreCmd.Flags().BoolVar(&restoreDryRun, "dry-run", false, "Show details and the native command that would run")
	restoreCmd.Flags().BoolVar(&restoreCreateIfMissing, "create-if-missing", false, "Create the target database if it does not exist")
	restoreCmd.Flags().BoolVar(&restoreOptimize, "optimize", false, "Run VACUUM ANALYZE after restore to optimize database")
	restoreCmd.Flags().BoolVar(&restoreSkipChecksum, "skip-checksum", false, "Skip checksum verification before restore")
	restoreCmd.Flags().BoolVar(&restoreVerify, "verify", false, "Validate dump file (TOC, schema compatibility, filters) without restoring")
	restoreCmd.Flags().BoolVar(&restoreVerifyPost, "verify-post", false, "Verify restored database after completion (table existence, row counts, sample queries)")
	restoreCmd.Flags().StringSliceVar(&includeTable, "include-table", nil, "Restore specific table (can be repeated)")
	restoreCmd.Flags().StringSliceVar(&excludeTable, "exclude-table", nil, "Exclude specific table (can be repeated)")
	restoreCmd.Flags().StringSliceVar(&includeSchema, "include-schema", nil, "Restore specific schema (can be repeated)")
	restoreCmd.Flags().StringSliceVar(&excludeSchema, "exclude-schema", nil, "Exclude specific schema (can be repeated)")

	_ = restoreCmd.MarkFlagRequired("profile")

	RootCmd.AddCommand(restoreCmd)
}
