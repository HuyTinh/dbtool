package postgres

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime/debug"
	"sync"
	"time"

	"dbtool/internal/driver"
	"dbtool/internal/procutil"
)

func (d *PostgresDriver) Restore(ctx context.Context, opts driver.RestoreOptions) (<-chan driver.Progress, error) {
	binary, args := buildRestoreArgs(opts)

	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("binary %q not found in PATH: %w", binary, err)
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+opts.Profile.Password)
	procutil.SetupProcAttr(cmd)
	cmd.Cancel = func() error {
		return procutil.KillProcessGroup(cmd, os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	progressChan := make(chan driver.Progress, 100)
	go func() {
		defer close(progressChan)

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			scanStream(stdout, progressChan, false)
		}()

		go func() {
			defer wg.Done()
			scanStream(stderr, progressChan, true)
		}()

		wg.Wait()

		err := cmd.Wait()
		if err != nil {
			sendProgress(progressChan, driver.Progress{
				Done: true,
				Err:  fmt.Errorf("restore command failed: %w", err),
			})
			return
		}

		sendProgress(progressChan, driver.Progress{
			Percent: 100,
			Done:    true,
			Message: "Restore process completed successfully",
		})
	}()

	return progressChan, nil
}

func (d *PostgresDriver) Dump(ctx context.Context, opts driver.DumpOptions) (<-chan driver.Progress, error) {
	binary, args := buildDumpArgs(opts)

	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("binary %q not found in PATH: %w", binary, err)
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+opts.Profile.Password)
	procutil.SetupProcAttr(cmd)
	cmd.Cancel = func() error {
		return procutil.KillProcessGroup(cmd, os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	progressChan := make(chan driver.Progress, 100)
	go func() {
		defer close(progressChan)

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			scanStream(stdout, progressChan, false)
		}()

		go func() {
			defer wg.Done()
			scanStream(stderr, progressChan, true)
		}()

		wg.Wait()

		err := cmd.Wait()
		if err != nil {
			sendProgress(progressChan, driver.Progress{
				Done: true,
				Err:  fmt.Errorf("dump command failed: %w", err),
			})
			return
		}

		sendProgress(progressChan, driver.Progress{
			Percent: 100,
			Done:    true,
			Message: "Dump process completed successfully",
		})
	}()

	return progressChan, nil
}

func buildRestoreArgs(opts driver.RestoreOptions) (string, []string) {
	if opts.Format == driver.FormatPlain {
		args := []string{
			"-h", opts.Profile.Host,
			"-p", fmt.Sprint(opts.Profile.Port),
			"-U", opts.Profile.User,
			"-d", opts.Profile.Database,
			"-f", opts.FilePath,
		}
		return "psql", args
	}

	args := []string{
		"-h", opts.Profile.Host,
		"-p", fmt.Sprint(opts.Profile.Port),
		"-U", opts.Profile.User,
		"-d", opts.Profile.Database,
	}
	if opts.Format == driver.FormatDirectory && opts.Jobs > 1 {
		args = append(args, "-j", fmt.Sprint(opts.Jobs))
	}
	if opts.Clean {
		args = append(args, "--clean", "--if-exists")
	}
	for _, t := range opts.IncludeTable {
		args = append(args, "-t", t)
	}
	for _, t := range opts.ExcludeTable {
		args = append(args, "-T", t)
	}
	for _, s := range opts.IncludeSchema {
		args = append(args, "-n", s)
	}
	for _, s := range opts.ExcludeSchema {
		args = append(args, "-N", s)
	}
	// Verbose mode to output logs which we parse
	args = append(args, "-v")
	args = append(args, opts.FilePath)

	return "pg_restore", args
}

func buildDumpArgs(opts driver.DumpOptions) (string, []string) {
	args := []string{
		"-h", opts.Profile.Host,
		"-p", fmt.Sprint(opts.Profile.Port),
		"-U", opts.Profile.User,
		"-d", opts.Profile.Database,
		"-f", opts.FilePath,
		"-v",
	}
	switch opts.Format {
	case driver.FormatCustom:
		args = append(args, "-Fc")
	case driver.FormatDirectory:
		args = append(args, "-Fd")
	case driver.FormatPlain:
		args = append(args, "-Fp")
	}
	for _, t := range opts.IncludeTable {
		args = append(args, "-t", t)
	}
	for _, t := range opts.ExcludeTable {
		args = append(args, "-T", t)
	}
	for _, s := range opts.IncludeSchema {
		args = append(args, "-n", s)
	}
	for _, s := range opts.ExcludeSchema {
		args = append(args, "-N", s)
	}
	return "pg_dump", args
}

func scanStream(r io.Reader, out chan driver.Progress, isStderr bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in scanStream (isStderr=%v): %v\n%s", isStderr, r, debug.Stack())
			sendProgress(out, driver.Progress{Err: fmt.Errorf("internal scan log failure: %v", r)})
		}
	}()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if isStderr {
			sendProgress(out, classifyStderrLine(line))
		} else {
			sendProgress(out, parseLine(line))
		}
	}
}

func sendProgress(out chan driver.Progress, p driver.Progress) {
	select {
	case out <- p:
	default:
		select {
		case <-out: // Discard oldest
		default:
		}
		select {
		case out <- p:
		default: // Non-blocking fail-safe
		}
	}
}
