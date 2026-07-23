// Package cmd contains dbtool's Cobra command handlers.
package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"dbtool/internal/backup"
	"dbtool/internal/config"
	"dbtool/internal/driver"
	"dbtool/internal/driver/postgres"
	"dbtool/internal/history"
	"dbtool/internal/integrity"

	"github.com/gen2brain/beeep"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var (
	dumpProfile           string
	dumpFormat            string
	dumpIncTables         []string
	dumpExcTables         []string
	dumpIncSchemas        []string
	dumpExcSchemas        []string
	dumpManifestRowCounts []string
)

var dumpCmd = &cobra.Command{
	Use:   "dump [output_file]",
	Short: "Export a database schema and content into a dump file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithTimeout(cmd, cmd.Context(), func(ctx context.Context) error {
			return ExecuteDumpLogic(ctx, args[0])
		})
	},
}

// ExecuteDumpLogic runs the dump operation independently of Cobra flag parsing.
func ExecuteDumpLogic(ctx context.Context, filePath string) error {
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

	progressChan, err := drv.Dump(ctx, opts)
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

	if lastProgress.Err == nil {
		if checksum, err := integrity.ComputeFileChecksum(filePath); err == nil {
			historyRec.Checksum = checksum
			_ = integrity.WriteChecksumFile(filePath, checksum)
			entries, archiveVersion, _ := integrity.ParseTOC(filePath)
			manifest := backup.BuildManifest(backup.Artifact{Path: filePath, Checksum: backup.ChecksumValid}, string(format), archiveVersion, entries)
			if len(dumpManifestRowCounts) > 0 {
				if len(dumpManifestRowCounts) > 50 {
					return fmt.Errorf("manifest row-count is limited to 50 tables")
				}
				collector, ok := drv.(driver.RowCountCollector)
				if !ok {
					return fmt.Errorf("driver %q does not support manifest row-count collection", profile.Driver)
				}
				for _, name := range dumpManifestRowCounts {
					parts := strings.Split(name, ".")
					if len(parts) != 2 {
						return fmt.Errorf("manifest row-count requires schema.table, got %q", name)
					}
					counts, err := collector.CollectRowCounts(ctx, profile, []driver.CatalogTable{{Schema: parts[0], Name: parts[1]}})
					if err != nil {
						return fmt.Errorf("collect manifest row count for %s: %w", name, err)
					}
					for _, count := range counts {
						manifest.RowCounts = append(manifest.RowCounts, backup.TableRowCount{Schema: count.Schema, Table: count.Table, RowCount: count.RowCount})
					}
				}
			}
			_ = backup.WriteManifest(filePath, manifest)
		}
		if size, err := integrity.FileSize(filePath); err == nil {
			historyRec.FileSize = size
		}
	}

	_ = history.AppendHistory(historyRec)

	if lastProgress.Err != nil {
		_ = beeep.Notify("DBTool Dump Failed", fmt.Sprintf("Profile: %s\nError: %v", profile.Name, lastProgress.Err), "")
		return lastProgress.Err
	}
	_ = beeep.Notify("DBTool Dump Success", fmt.Sprintf("Database %s dumped to %s", profile.Database, filePath), "")
	recordFlow(dumpFlow(profile.Name, filePath, string(format), dumpIncTables, dumpExcTables, dumpIncSchemas, dumpExcSchemas))

	fmt.Println("✓ Database dump completed successfully.")
	if historyRec.Checksum != "" {
		fmt.Printf("  Checksum: %s\n", historyRec.Checksum)
	}
	if historyRec.FileSize > 0 {
		fmt.Printf("  Size: %s\n", formatBytes(historyRec.FileSize))
	}
	return nil
}

func init() {
	dumpCmd.Flags().StringVar(&dumpProfile, "profile", "", "Profile name to dump from")
	dumpCmd.Flags().StringVar(&dumpFormat, "format", "custom", "Format of output file (custom, plain, directory)")
	dumpCmd.Flags().StringSliceVar(&dumpIncTables, "include-table", nil, "Dump specific table (can be repeated)")
	dumpCmd.Flags().StringSliceVar(&dumpExcTables, "exclude-table", nil, "Exclude specific table from dump (can be repeated)")
	dumpCmd.Flags().StringSliceVar(&dumpIncSchemas, "include-schema", nil, "Dump specific schema (can be repeated)")
	dumpCmd.Flags().StringSliceVar(&dumpExcSchemas, "exclude-schema", nil, "Exclude specific schema from dump (can be repeated)")
	dumpCmd.Flags().StringSliceVar(&dumpManifestRowCounts, "manifest-row-count", nil, "Record exact row count for a dumped schema.table (can be repeated; max 50)")

	_ = dumpCmd.MarkFlagRequired("profile")

	RootCmd.AddCommand(dumpCmd)
}
