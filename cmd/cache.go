package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"dbtool/internal/cache"

	"github.com/spf13/cobra"
)

var cacheNamespace string

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the local file cache",
}

var cacheListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cache namespaces with their sizes and entry counts",
	RunE: func(cmd *cobra.Command, args []string) error {
		fc, err := cache.NewFileCache()
		if err != nil {
			return fmt.Errorf("failed to initialize cache: %w", err)
		}

		cacheDir, err := fc.Dir()
		if err != nil {
			return fmt.Errorf("failed to get cache directory: %w", err)
		}

		entries, err := os.ReadDir(cacheDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Cache is empty.")
				return nil
			}
			return fmt.Errorf("failed to read cache directory: %w", err)
		}

		totalEntries := 0
		var totalSize int64

		fmt.Printf("%-20s %-10s %s\n", "NAMESPACE", "ENTRIES", "SIZE")
		fmt.Println("--------------------------------------------")

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			nsPath := filepath.Join(cacheDir, e.Name())
			nsEntries, err := os.ReadDir(nsPath)
			if err != nil {
				continue
			}

			var nsSize int64
			nsCount := 0
			for _, f := range nsEntries {
				if filepath.Ext(f.Name()) != ".json" {
					continue
				}
				fi, err := f.Info()
				if err != nil {
					continue
				}
				nsSize += fi.Size()
				nsCount++
			}

			totalEntries += nsCount
			totalSize += nsSize
			fmt.Printf("%-20s %-10d %s\n", e.Name(), nsCount, formatBytes(nsSize))
		}

		fmt.Println("--------------------------------------------")
		fmt.Printf("%-20s %-10d %s\n", "TOTAL", totalEntries, formatBytes(totalSize))
		fmt.Printf("\nCache directory: %s\n", cacheDir)
		return nil
	},
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear the cache (all namespaces, or a specific namespace with --namespace)",
	RunE: func(cmd *cobra.Command, args []string) error {
		fc, err := cache.NewFileCache()
		if err != nil {
			return fmt.Errorf("failed to initialize cache: %w", err)
		}

		if cacheNamespace != "" {
			if err := fc.ClearNamespace(cacheNamespace); err != nil {
				return fmt.Errorf("failed to clear namespace %q: %w", cacheNamespace, err)
			}
			fmt.Printf("✓ Cache namespace %q cleared.\n", cacheNamespace)
		} else {
			if err := fc.Clear(); err != nil {
				return fmt.Errorf("failed to clear cache: %w", err)
			}
			fmt.Println("✓ All cache entries cleared.")
		}
		return nil
	},
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func init() {
	cacheClearCmd.Flags().StringVar(&cacheNamespace, "namespace", "", "Specific namespace to clear (default: all)")

	cacheCmd.AddCommand(cacheListCmd)
	cacheCmd.AddCommand(cacheClearCmd)
	RootCmd.AddCommand(cacheCmd)
}
