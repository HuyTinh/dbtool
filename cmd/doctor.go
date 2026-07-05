package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check database restoration dependencies and connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		runCtx, cancel, err := ContextWithTimeout(cmd, cmd.Context())
		if err != nil {
			return err
		}
		defer cancel()

		cfg, err := config.LoadConfig()
		var configErr error
		if err != nil {
			configErr = err
		}

		var checkTasks []func(ctx context.Context) []driver.DoctorCheck

		// 1. Global: Configuration validation
		checkTasks = append(checkTasks, func(ctx context.Context) []driver.DoctorCheck {
			var results []driver.DoctorCheck
			if configErr != nil {
				results = append(results, driver.DoctorCheck{
					Name:     "Configuration File",
					Severity: driver.CheckError,
					OK:       false,
					Message:  fmt.Sprintf("Failed to load profiles.yaml: %v", configErr),
					Hint:     "Ensure the configuration file path is valid and conforms to YAML spec.",
				})
			} else {
				path, _ := config.GetConfigFilePath()
				results = append(results, driver.DoctorCheck{
					Name:     "Configuration File",
					Severity: driver.CheckOK,
					OK:       true,
					Message:  fmt.Sprintf("Profiles file is valid and loaded successfully from %s", path),
				})
			}
			return results
		})

		// 2. Global: Cache folder validation
		checkTasks = append(checkTasks, func(ctx context.Context) []driver.DoctorCheck {
			var results []driver.DoctorCheck
			dir, err := config.GetConfigDir()
			if err != nil {
				results = append(results, driver.DoctorCheck{
					Name:     "Cache Folder",
					Severity: driver.CheckError,
					OK:       false,
					Message:  fmt.Sprintf("Failed to resolve configuration folder: %v", err),
				})
				return results
			}

			cacheDir := filepath.Join(dir, "cache")
			if err := os.MkdirAll(cacheDir, 0700); err != nil {
				results = append(results, driver.DoctorCheck{
					Name:     "Cache Folder",
					Severity: driver.CheckError,
					OK:       false,
					Message:  fmt.Sprintf("Cache directory %s is not writable: %v", cacheDir, err),
				})
			} else {
				results = append(results, driver.DoctorCheck{
					Name:     "Cache Folder",
					Severity: driver.CheckOK,
					OK:       true,
					Message:  fmt.Sprintf("Cache folder is writable at: %s", cacheDir),
				})
			}
			return results
		})

		// 3. Global: History file validation
		checkTasks = append(checkTasks, func(ctx context.Context) []driver.DoctorCheck {
			var results []driver.DoctorCheck
			dir, err := config.GetConfigDir()
			if err != nil {
				return nil
			}
			historyPath := filepath.Join(dir, "history.jsonl")
			f, err := os.OpenFile(historyPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				results = append(results, driver.DoctorCheck{
					Name:     "History File Log",
					Severity: driver.CheckError,
					OK:       false,
					Message:  fmt.Sprintf("History log file %s is not writable: %v", historyPath, err),
				})
			} else {
				f.Close()
				results = append(results, driver.DoctorCheck{
					Name:     "History File Log",
					Severity: driver.CheckOK,
					OK:       true,
					Message:  fmt.Sprintf("History log file is writable at: %s", historyPath),
				})
			}
			return results
		})

		// 4. Drivers default binary existence check
		for _, d := range driver.All() {
			drv := d
			checkTasks = append(checkTasks, func(ctx context.Context) []driver.DoctorCheck {
				return drv.Doctor(nil)
			})
		}

		// 5. Individual profile connectivity check
		if configErr == nil && cfg != nil {
			for _, p := range cfg.Profiles {
				prof := p
				drv, err := driver.Get(prof.Driver)
				if err != nil {
					checkTasks = append(checkTasks, func(ctx context.Context) []driver.DoctorCheck {
						return []driver.DoctorCheck{{
							Name:     "Profile Engine: " + prof.Name,
							Severity: driver.CheckError,
							OK:       false,
							Message:  fmt.Sprintf("Required driver %q is not registered: %v", prof.Driver, err),
							Hint:     "Specify a supported driver like 'postgres' inside profiles.yaml.",
						}}
					})
					continue
				}

				checkTasks = append(checkTasks, func(ctx context.Context) []driver.DoctorCheck {
					return drv.Doctor(&prof)
				})
			}
		}

		// Execute check tasks concurrently using errgroup
		g, gctx := errgroup.WithContext(runCtx)
		resultsChan := make(chan []driver.DoctorCheck, len(checkTasks))

		for _, task := range checkTasks {
			t := task
			g.Go(func() error {
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
					resultsChan <- t(gctx)
					return nil
				}
			})
		}

		if err := g.Wait(); err != nil {
			return fmt.Errorf("doctor validation check timed out or interrupted: %w", err)
		}
		close(resultsChan)

		// Aggregate outputs
		var allChecks []driver.DoctorCheck
		for list := range resultsChan {
			for _, item := range list {
				if item.Name != "" { // Filter out skipped checks
					allChecks = append(allChecks, item)
				}
			}
		}

		hasErrors := false
		fmt.Println("\nDBTool Doctor Report:")
		fmt.Println(strings.Repeat("=", 40))

		for _, check := range allChecks {
			icon := ""
			switch check.Severity {
			case driver.CheckOK:
				icon = "\033[1;32m✓\033[0m"
			case driver.CheckWarning:
				icon = "\033[1;33m⚠\033[0m"
			case driver.CheckError:
				icon = "\033[1;31m✗\033[0m"
				hasErrors = true
			}

			fmt.Printf("%s %-30s - %s\n", icon, check.Name, check.Message)
			if !check.OK && check.Hint != "" {
				fmt.Printf("  -> \033[3mGợi ý: %s\033[0m\n", check.Hint)
			}
		}

		if hasErrors {
			os.Exit(1)
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(doctorCmd)
}
