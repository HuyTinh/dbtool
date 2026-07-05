package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"
	"dbtool/internal/driver/postgres"
	"dbtool/internal/history"
	"dbtool/internal/safety"

	"github.com/spf13/cobra"
)

var (
	restoreProfile   string
	restoreFormat    string
	restoreJobs      int
	restoreClean     bool
	restoreDryRun    bool
	includeTable     []string
	excludeTable     []string
	includeSchema    []string
	excludeSchema    []string
)

var restoreCmd = &cobra.Command{
	Use:   "restore [file_or_directory]",
	Short: "Restore a database from a dump archive or file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		// 1. Verify file exists and is readable early
		if err := validateDumpFile(filePath); err != nil {
			return err
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
			Profile:       profile,
			FilePath:      filePath,
			Format:        finalFormat,
			Jobs:          restoreJobs,
			Clean:         restoreClean,
			IncludeTable:  includeTable,
			ExcludeTable:  excludeTable,
			IncludeSchema: includeSchema,
			ExcludeSchema: excludeSchema,
			DryRun:        restoreDryRun,
		}

		cmdString := getDryRunCommand(opts)

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

		// 7. Safety Validation
		hasData, err := safety.CheckDatabaseHasData(cmd.Context(), profile.Host, profile.Port, profile.User, profile.Password, profile.Database)
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
		runCtx, cancel, err := ContextWithTimeout(cmd, cmd.Context())
		if err != nil {
			return err
		}
		defer cancel()

		// 9. Execute Restore Subprocess
		progressChan, err := drv.Restore(runCtx, opts)
		if err != nil {
			return err
		}

		fmt.Println("Starting restore operation...")
		var lastProgress driver.Progress
		for p := range progressChan {
			lastProgress = p
			if !Quiet {
				if Verbose || p.Err != nil || strings.Contains(p.Message, "[Database Error]") || p.Percent == 100 {
					fmt.Println(p.Message)
				} else {
					// Output only clean and concise progress lines
					fmt.Printf("\r%-100s", p.Message)
				}
			}
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

		if lastProgress.Err != nil {
			return lastProgress.Err
		}

		fmt.Println("✓ Database restore completed successfully.")
		return nil
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

func formatBytes(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
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

func init() {
	restoreCmd.Flags().StringVar(&restoreProfile, "profile", "", "Profile name to restore into")
	restoreCmd.Flags().StringVar(&restoreFormat, "format", "auto", "Format of dump file (auto, custom, plain, directory)")
	restoreCmd.Flags().IntVarP(&restoreJobs, "jobs", "j", defaultJobs(), "Number of parallel restore jobs")
	restoreCmd.Flags().BoolVar(&restoreClean, "clean", false, "Clean (drop) database objects before recreating")
	restoreCmd.Flags().BoolVar(&restoreDryRun, "dry-run", false, "Show details and the native command that would run")
	restoreCmd.Flags().StringSliceVar(&includeTable, "include-table", nil, "Restore specific table (can be repeated)")
	restoreCmd.Flags().StringSliceVar(&excludeTable, "exclude-table", nil, "Exclude specific table (can be repeated)")
	restoreCmd.Flags().StringSliceVar(&includeSchema, "include-schema", nil, "Restore specific schema (can be repeated)")
	restoreCmd.Flags().StringSliceVar(&excludeSchema, "exclude-schema", nil, "Exclude specific schema (can be repeated)")

	_ = restoreCmd.MarkFlagRequired("profile")

	RootCmd.AddCommand(restoreCmd)
}
