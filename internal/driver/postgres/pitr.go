package postgres

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"dbtool/internal/config"

	"github.com/jackc/pgx/v5"
)

// SetupArchive configures PostgreSQL for WAL archiving
func (d *PostgresDriver) SetupArchive(ctx context.Context, profile config.Profile, archiveDir string) error {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database,
	)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("cannot connect to PostgreSQL: %w", err)
	}
	defer conn.Close(ctx)

	// Check wal_level
	var walLevel string
	err = conn.QueryRow(ctx, "SHOW wal_level").Scan(&walLevel)
	if err != nil {
		return fmt.Errorf("cannot check wal_level: %w", err)
	}

	if walLevel != "replica" && walLevel != "logical" {
		return fmt.Errorf("wal_level is '%s', must be 'replica' or 'logical' (requires PostgreSQL restart)", walLevel)
	}

	// Check archive_mode
	var archiveMode string
	err = conn.QueryRow(ctx, "SHOW archive_mode").Scan(&archiveMode)
	if err != nil {
		return fmt.Errorf("cannot check archive_mode: %w", err)
	}

	if archiveMode == "off" {
		return fmt.Errorf("archive_mode is OFF (requires PostgreSQL restart to enable)")
	}

	// Set archive_command
	archiveCmd := fmt.Sprintf("cp %%p %s/%%f", archiveDir)

	// Escape dấu nháy đơn để an toàn trong SQL literal
	escapedArchiveCmd := strings.ReplaceAll(archiveCmd, "'", "''")

	query := fmt.Sprintf(
		"ALTER SYSTEM SET archive_command = '%s'",
		escapedArchiveCmd,
	)
	_, err = conn.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("cannot set archive_command: %w", err)
	}

	// Reload configuration
	_, err = conn.Exec(ctx, "SELECT pg_reload_conf()")
	if err != nil {
		return fmt.Errorf("cannot reload configuration: %w", err)
	}

	return nil
}

// GetCurrentTimeline returns the current timeline ID
func (d *PostgresDriver) GetCurrentTimeline(ctx context.Context, profile config.Profile) (int, error) {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database,
	)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return 0, fmt.Errorf("cannot connect to PostgreSQL: %w", err)
	}
	defer conn.Close(ctx)

	var timeline int
	err = conn.QueryRow(ctx, "SELECT timeline_id FROM pg_control_checkpoint()").Scan(&timeline)
	if err != nil {
		return 0, fmt.Errorf("cannot get timeline: %w", err)
	}

	return timeline, nil
}

// GetArchiveMode checks if archive_mode is enabled
func (d *PostgresDriver) GetArchiveMode(ctx context.Context, profile config.Profile) (bool, error) {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database,
	)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return false, fmt.Errorf("cannot connect to PostgreSQL: %w", err)
	}
	defer conn.Close(ctx)

	var archiveMode string
	err = conn.QueryRow(ctx, "SHOW archive_mode").Scan(&archiveMode)
	if err != nil {
		return false, fmt.Errorf("cannot check archive_mode: %w", err)
	}

	return archiveMode == "on", nil
}

// StopPostgreSQL stops the PostgreSQL server
func (d *PostgresDriver) StopPostgreSQL(ctx context.Context, profile config.Profile, dataDir string) error {
	cmd := exec.CommandContext(ctx, "pg_ctl", "-D", dataDir, "stop", "-m", "fast")
	cmd.Env = append(os.Environ(), "PGPASSWORD="+profile.Password)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_ctl stop failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// StartPostgreSQL starts the PostgreSQL server
func (d *PostgresDriver) StartPostgreSQL(ctx context.Context, profile config.Profile, dataDir string) error {
	cmd := exec.CommandContext(ctx, "pg_ctl", "-D", dataDir, "start", "-w")
	cmd.Env = append(os.Environ(), "PGPASSWORD="+profile.Password)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_ctl start failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// ConfigureRecovery writes recovery.signal and postgresql.auto.conf for PITR
func (d *PostgresDriver) ConfigureRecovery(dataDir, archiveDir, targetTime string, targetTimeline int) error {
	// Write recovery.signal
	signalFile := dataDir + "/recovery.signal"
	if err := os.WriteFile(signalFile, []byte(""), 0644); err != nil {
		return fmt.Errorf("cannot create recovery.signal: %w", err)
	}

	// Build recovery configuration
	var recoveryConf strings.Builder
	recoveryConf.WriteString("# PITR Recovery Configuration\n")
	recoveryConf.WriteString(fmt.Sprintf("restore_command = 'cp %s/%%f %%p'\n", archiveDir))

	if targetTime != "" {
		recoveryConf.WriteString(fmt.Sprintf("recovery_target_time = '%s'\n", targetTime))
	}

	if targetTimeline > 0 {
		recoveryConf.WriteString(fmt.Sprintf("recovery_target_timeline = '%d'\n", targetTimeline))
	} else {
		recoveryConf.WriteString("recovery_target_timeline = 'latest'\n")
	}

	recoveryConf.WriteString("recovery_target_action = 'promote'\n")

	// Write postgresql.auto.conf
	autoConfPath := dataDir + "/postgresql.auto.conf"
	autoConf, err := os.OpenFile(autoConfPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("cannot open postgresql.auto.conf: %w", err)
	}
	defer autoConf.Close()

	if _, err := autoConf.WriteString("\n" + recoveryConf.String()); err != nil {
		return fmt.Errorf("cannot write recovery config: %w", err)
	}

	return nil
}

// WaitRecovery monitors PostgreSQL recovery progress
func (d *PostgresDriver) WaitRecovery(ctx context.Context, profile config.Profile, timeout time.Duration) error {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?connect_timeout=5",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database,
	)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		func() {
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()

			conn, err := pgx.Connect(ctx, dsn)
			if err != nil {
				// PostgreSQL might still be starting
				time.Sleep(2 * time.Second)
				return
			}
			defer conn.Close(ctx)

			var inRecovery bool
			err = conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery)
			if err != nil {
				time.Sleep(2 * time.Second)
				return
			}

			if !inRecovery {
				// Recovery completed
				return
			}

			// Still in recovery mode
			time.Sleep(2 * time.Second)
		}()
	}

	return fmt.Errorf("recovery timeout after %v", timeout)
}
