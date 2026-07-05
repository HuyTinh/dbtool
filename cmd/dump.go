package cmd

import (
	"fmt"
	"os"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"
	"dbtool/internal/driver/postgres"
	"dbtool/internal/history"

	"github.com/gen2brain/beeep"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var (
	dumpProfile    string
	dumpFormat     string
	dumpIncTables  []string
	dumpExcTables  []string
	dumpIncSchemas []string
	dumpExcSchemas []string
)

var dumpCmd = &cobra.Command{
	Use:   "dump [output_file]",
	Short: "Export a database schema and content into a dump file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		// 1. Validate filter patterns early
		for _, pat := range dumpIncTables {
			if err := postgres.ValidateTablePattern(pat); err != nil {
				return err
			}
		}
		for _, pat := range dumpExcTables {
			if err := postgres.ValidateTablePattern(pat); err != nil {
				return err
			}
		}
		for _, pat := range dumpIncSchemas {
			if err := postgres.ValidateTablePattern(pat); err != nil {
				return err
			}
		}
		for _, pat := range dumpExcSchemas {
			if err := postgres.ValidateTablePattern(pat); err != nil {
				return err
			}
		}

		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		profile, ok := cfg.GetProfile(dumpProfile)
		if !ok {
			return fmt.Errorf("profile %q not found", dumpProfile)
		}

		drv, err := driver.Get(profile.Driver)
		if err != nil {
			return err
		}

		format := driver.Format(dumpFormat)
		if format == "" {
			format = driver.FormatCustom
		}

		opts := driver.DumpOptions{
			Profile:       profile,
			FilePath:      filePath,
			Format:        format,
			IncludeTable:  dumpIncTables,
			ExcludeTable:  dumpExcTables,
			IncludeSchema: dumpIncSchemas,
			ExcludeSchema: dumpExcSchemas,
		}

		runCtx, cancel, err := ContextWithTimeout(cmd, cmd.Context())
		if err != nil {
			return err
		}
		defer cancel()

		progressChan, err := drv.Dump(runCtx, opts)
		if err != nil {
			return err
		}

		var bar *progressbar.ProgressBar
		if !Quiet && !Verbose {
			bar = progressbar.NewOptions(100,
				progressbar.OptionSetDescription("Dumping database"),
				progressbar.OptionSetWriter(os.Stdout),
				progressbar.OptionSetWidth(15),
				progressbar.OptionThrottle(100*time.Millisecond),
				progressbar.OptionShowCount(),
				progressbar.OptionOnCompletion(func() {
					fmt.Println()
				}),
			)
		}

		fmt.Printf("Starting dump of CSDL '%s' on %s:%d...\n", profile.Database, profile.Host, profile.Port)
		var lastProgress driver.Progress
		for p := range progressChan {
			lastProgress = p
			if !Quiet {
				if Verbose || p.Err != nil || p.Percent == 100 {
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

		// Reconstruct command string for logging
		filterFlags := ""
		for _, t := range dumpIncTables {
			filterFlags += " -t " + t
		}
		for _, t := range dumpExcTables {
			filterFlags += " -T " + t
		}
		for _, s := range dumpIncSchemas {
			filterFlags += " -n " + s
		}
		for _, s := range dumpExcSchemas {
			filterFlags += " -N " + s
		}
		cmdString := fmt.Sprintf("pg_dump -h %s -p %d -U %s -d %s -f %s -F %s%s",
			profile.Host, profile.Port, profile.User, profile.Database, filePath, format, filterFlags)

		// Record to history
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
			_ = beeep.Notify("DBTool Dump Failed", fmt.Sprintf("Profile: %s\nError: %v", profile.Name, lastProgress.Err), "")
			return lastProgress.Err
		}
		_ = beeep.Notify("DBTool Dump Success", fmt.Sprintf("Database %s dumped to %s", profile.Database, filePath), "")

		fmt.Println("✓ Database dump completed successfully.")
		return nil
	},
}

func init() {
	dumpCmd.Flags().StringVar(&dumpProfile, "profile", "", "Profile name to dump from")
	dumpCmd.Flags().StringVar(&dumpFormat, "format", "custom", "Format of output file (custom, plain, directory)")
	dumpCmd.Flags().StringSliceVar(&dumpIncTables, "include-table", nil, "Dump specific table (can be repeated)")
	dumpCmd.Flags().StringSliceVar(&dumpExcTables, "exclude-table", nil, "Exclude specific table from dump (can be repeated)")
	dumpCmd.Flags().StringSliceVar(&dumpIncSchemas, "include-schema", nil, "Dump specific schema (can be repeated)")
	dumpCmd.Flags().StringSliceVar(&dumpExcSchemas, "exclude-schema", nil, "Exclude specific schema from dump (can be repeated)")

	_ = dumpCmd.MarkFlagRequired("profile")

	RootCmd.AddCommand(dumpCmd)
}
