package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dbtool/internal/config"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	Quiet   bool
	Verbose bool
	Timeout string
	AskPass bool
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
		if AskPass {
			if err := promptMasterPassword(); err != nil {
				return err
			}
		}
		return nil
	},
}

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Inspect read-only PostgreSQL session metadata",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithTimeout(cmd, cmd.Context(), executeSessions)
	},
}

var sessionsPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Render a non-executable PostgreSQL session action plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithTimeout(cmd, cmd.Context(), executeSessionsPlan)
	},
}

func init() {
	RootCmd.Version = Version
	RootCmd.PersistentFlags().BoolVarP(&Quiet, "quiet", "q", false, "Suppress progress outputs and only display errors")
	RootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "Print detailed logs of subprocess execution")
	RootCmd.PersistentFlags().StringVar(&Timeout, "timeout", "", "Operation execution timeout (e.g., 2h, 45m, 15s)")
	RootCmd.PersistentFlags().BoolVar(&AskPass, "ask-pass", false, "Prompt securely for the master password used by encrypted profile secrets")
	sessionsCmd.Flags().StringVar(&sessionsProfile, "profile", "", "PostgreSQL profile name")
	sessionsCmd.Flags().StringVar(&sessionsState, "state", "", "Limit results to one PostgreSQL session state")
	sessionsCmd.Flags().IntVar(&sessionsLimit, "limit", 20, "Maximum sessions to show")
	sessionsCmd.Flags().StringVar(&sessionsOutput, "output", "table", "Output format: table or json")
	_ = sessionsCmd.MarkFlagRequired("profile")
	sessionsPlanCmd.Flags().StringVar(&sessionsPlanProfile, "profile", "", "PostgreSQL profile name")
	sessionsPlanCmd.Flags().Int32Var(&sessionsPlanPID, "pid", 0, "PostgreSQL backend PID to plan for")
	sessionsPlanCmd.Flags().StringVar(&sessionsPlanAction, "action", "", "Action preview: cancel or terminate")
	sessionsPlanCmd.Flags().StringVar(&sessionsPlanOutput, "output", "table", "Output format: table or json")
	_ = sessionsPlanCmd.MarkFlagRequired("profile")
	_ = sessionsPlanCmd.MarkFlagRequired("pid")
	_ = sessionsPlanCmd.MarkFlagRequired("action")
	sessionsCmd.AddCommand(sessionsPlanCmd)
	RootCmd.AddCommand(sessionsCmd)
}

func promptMasterPassword() error {
	fmt.Fprint(os.Stderr, "Master password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return fmt.Errorf("read master password for --ask-pass (an interactive terminal is required): %w", err)
	}
	if len(password) == 0 {
		return errors.New("master password for --ask-pass cannot be empty")
	}
	config.SetMasterPassword(string(password))
	return nil
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

func runWithTimeout(cmd *cobra.Command, rootCtx context.Context, run func(context.Context) error) error {
	runCtx, cancel, err := ContextWithTimeout(cmd, rootCtx)
	if err != nil {
		return err
	}
	defer cancel()
	return run(runCtx)
}
