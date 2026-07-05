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
	"strings"
	"sync"
	"time"

	"dbtool/internal/cache"
	"dbtool/internal/config"
	"dbtool/internal/driver"
	"dbtool/internal/procutil"

	"github.com/jackc/pgx/v5"
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

	totalTOC := 0
	if opts.Format == driver.FormatCustom || opts.Format == driver.FormatDirectory {
		totalTOC = d.countTOC(opts.FilePath)
	}

	progressChan := make(chan driver.Progress, 100)
	go func() {
		defer close(progressChan)

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			scanStream(stdout, progressChan, false, 0, false)
		}()

		go func() {
			defer wg.Done()
			scanStream(stderr, progressChan, true, totalTOC, false)
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

	totalTables := countSourceTables(ctx, opts.Profile)

	progressChan := make(chan driver.Progress, 100)
	go func() {
		defer close(progressChan)

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			scanStream(stdout, progressChan, false, 0, true)
		}()

		go func() {
			defer wg.Done()
			scanStream(stderr, progressChan, true, totalTables, true)
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
	if opts.SchemaOnly {
		args = append(args, "-s")
	}
	if opts.DataOnly {
		args = append(args, "-a")
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

func scanStream(r io.Reader, out chan driver.Progress, isStderr bool, totalCount int, isDump bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in scanStream (isStderr=%v): %v\n%s", isStderr, r, debug.Stack())
			sendProgress(out, driver.Progress{Err: fmt.Errorf("internal scan log failure: %v", r)})
		}
	}()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	
	processed := 0
	for scanner.Scan() {
		line := scanner.Text()
		var p driver.Progress
		if isStderr {
			p = classifyStderrLine(line)
			if !isDump && totalCount > 0 && isPgRestoreVerboseLine(line) {
				processed++
				p.Percent = (float64(processed) / float64(totalCount)) * 100
				if p.Percent >= 100.0 {
					p.Percent = 99.0
				}
			}
			if isDump && totalCount > 0 && isPgDumpVerboseLine(line) {
				processed++
				p.Percent = (float64(processed) / float64(totalCount)) * 100
				if p.Percent >= 100.0 {
					p.Percent = 99.0
				}
			}
		} else {
			p = parseLine(line)
		}
		sendProgress(out, p)
	}
}

func isPgRestoreVerboseLine(line string) bool {
	line = strings.ToLower(strings.TrimSpace(line))
	if !strings.HasPrefix(line, "pg_restore:") {
		return false
	}
	if strings.Contains(line, "warning:") || strings.Contains(line, "error:") {
		return false
	}
	return strings.Contains(line, "creating") ||
		strings.Contains(line, "processing") ||
		strings.Contains(line, "setting") ||
		strings.Contains(line, "dropping") ||
		strings.Contains(line, "refreshing")
}

func isPgDumpVerboseLine(line string) bool {
	line = strings.ToLower(strings.TrimSpace(line))
	if !strings.HasPrefix(line, "pg_dump:") {
		return false
	}
	if strings.Contains(line, "warning:") || strings.Contains(line, "error:") {
		return false
	}
	return strings.Contains(line, "dumping contents of table") ||
		strings.Contains(line, "dumping database") ||
		strings.Contains(line, "saving database definition")
}

func countTOCEntries(filePath string) (int, error) {
	binary := "pg_restore"
	if _, err := exec.LookPath(binary); err != nil {
		return 0, err
	}
	cmd := exec.Command(binary, "-l", filePath)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		count++
	}
	return count, nil
}

func (d *PostgresDriver) countTOC(filePath string) int {
	if d.cache == nil {
		count, _ := countTOCEntries(filePath)
		return count
	}
	fp, err := cache.ComputeFingerprint(filePath)
	if err != nil {
		return 0
	}
	var cachedCount int
	ok, _ := d.cache.Get("toc", fp.Key(), &cachedCount)
	if ok {
		return cachedCount
	}
	count, err := countTOCEntries(filePath)
	if err != nil {
		return 0
	}
	_ = d.cache.Set("toc", fp.Key(), filePath, count, 30*24*time.Hour)
	return count
}

func countSourceTables(ctx context.Context, profile config.Profile) int {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?connect_timeout=3",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database,
	)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return 0
	}
	defer conn.Close(ctx)
	var count int
	query := `
		SELECT count(*) 
		FROM information_schema.tables 
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema') 
		  AND table_schema NOT LIKE 'pg_toast%'
	`
	err = conn.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0
	}
	return count
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
