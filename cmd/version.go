package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of dbtool",
	Long:  `All software has versions. This is dbtool's version.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("dbtool version: %s\n", Version)
		if Commit != "none" {
			fmt.Printf("commit:         %s\n", Commit)
		}
		if Date != "unknown" {
			fmt.Printf("build date:     %s\n", Date)
		}
		fmt.Printf("go version:     %s\n", runtime.Version())
		fmt.Printf("platform:       %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
}
