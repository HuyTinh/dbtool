package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestRunWithTimeoutPassesDeadlineToCommandWork(t *testing.T) {
	previousTimeout := Timeout
	Timeout = "1s"
	t.Cleanup(func() { Timeout = previousTimeout })

	called := false
	err := runWithTimeout(&cobra.Command{}, context.Background(), func(ctx context.Context) error {
		called = true
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("operation context has no deadline")
		}
		if remaining := time.Until(deadline); remaining <= 0 || remaining > time.Second {
			t.Fatalf("unexpected deadline remaining: %s", remaining)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("runWithTimeout: %v", err)
	}
	if !called {
		t.Fatal("operation was not called")
	}
}
