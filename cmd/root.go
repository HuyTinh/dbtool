package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var (
	Quiet   bool
	Verbose bool
	Timeout string
	Version = "v0.0.3"
	Commit  = "none"
	Date    = "unknown"
)

var RootCmd = &cobra.Command{
	Use:   "dbtool",
	Short: "dbtool is a CLI wrapper around pg_restore, pg_dump, and psql",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if Quiet && Verbose {
			return errors.New("cannot specify both --quiet and --verbose flags")
		}
		return nil
	},
}

func init() {
	RootCmd.Version = Version
	RootCmd.PersistentFlags().BoolVarP(&Quiet, "quiet", "q", false, "Suppress progress outputs and only display errors")
	RootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "Print detailed logs of subprocess execution")
	RootCmd.PersistentFlags().StringVar(&Timeout, "timeout", "", "Operation execution timeout (e.g., 2h, 45m, 15s)")
}

func Execute() error {
	// Root context captures SIGINT and SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return RootCmd.ExecuteContext(ctx)
}

func ContextWithTimeout(cmd *cobra.Command, rootCtx context.Context) (context.Context, context.CancelFunc, error) {
	if Timeout == "" {
		return rootCtx, func() {}, nil
	}
	d, err := time.ParseDuration(Timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid duration format for --timeout: %w", err)
	}
	ctx, cancel := context.WithTimeout(rootCtx, d)
	return ctx, cancel, nil
}
