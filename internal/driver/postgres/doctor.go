package postgres

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	"github.com/jackc/pgx/v5"
)

const (
	connectRetryMax     = 2
	connectRetryBackoff = 500 * time.Millisecond
)

var placeholderPattern = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}|%[A-Za-z_][A-Za-z0-9_]*%`)

func isPlaceholderProfile(profile config.Profile) bool {
	return placeholderPattern.MatchString(profile.Host) ||
		placeholderPattern.MatchString(profile.User) ||
		placeholderPattern.MatchString(profile.Database) ||
		placeholderPattern.MatchString(profile.Password)
}

func (d *PostgresDriver) TestConnection(ctx context.Context, profile config.Profile) error {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?connect_timeout=3",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database,
	)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	return conn.Ping(ctx)
}

func (d *PostgresDriver) Doctor(profile *config.Profile) []driver.DoctorCheck {
	checks := []driver.DoctorCheck{
		checkBinaryExists("pg_restore"),
		checkBinaryExists("pg_dump"),
		checkBinaryExists("psql"),
	}

	if profile != nil {
		// Check for placeholder profile values first
		if isPlaceholderProfile(*profile) {
			checks = append(checks, driver.DoctorCheck{
				Name:     "Profile placeholders: " + profile.Name,
				Severity: driver.CheckWarning,
				OK:       false,
				Message:  fmt.Sprintf("Profile %q contains unresolved placeholders (e.g. ${VAR})", profile.Name),
				Hint:     "Set the corresponding environment variables, or update the profile with real values.",
			})
			return checks
		}

		// Perform connection check (with retries)
		connCheck := d.checkConnectionWithRetry(context.Background(), *profile)
		checks = append(checks, connCheck)

		// Compare versions client vs server
		if connCheck.OK {
			versionCheck := d.checkVersionVsServer(*profile)
			if versionCheck.Name != "" {
				checks = append(checks, versionCheck)
			}
		}
	}

	return checks
}

func checkBinaryExists(binaryName string) driver.DoctorCheck {
	path, err := exec.LookPath(binaryName)
	if err != nil {
		return driver.DoctorCheck{
			Name:     binaryName + " presence",
			Severity: driver.CheckError,
			OK:       false,
			Message:  fmt.Sprintf("Binary %q not found in PATH", binaryName),
			Hint:     "Ensure PostgreSQL client utilities are installed and added to the environment PATH.",
		}
	}
	return driver.DoctorCheck{
		Name:     binaryName + " presence",
		Severity: driver.CheckOK,
		OK:       true,
		Message:  fmt.Sprintf("Found %q at %s", binaryName, path),
	}
}

func (d *PostgresDriver) checkConnectionWithRetry(ctx context.Context, profile config.Profile) driver.DoctorCheck {
	var lastErr error
	for attempt := 0; attempt <= connectRetryMax; attempt++ {
		if attempt > 0 {
			log.Printf("checkConnection: Retrying attempt %d/%d for profile %q (last error: %v)",
				attempt, connectRetryMax, profile.Name, lastErr)
			time.Sleep(connectRetryBackoff)
		}

		attemptCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := d.TestConnection(attemptCtx, profile)
		cancel()

		if err == nil {
			return driver.DoctorCheck{
				Name:     "Database Connection: " + profile.Name,
				Severity: driver.CheckOK,
				OK:       true,
				Message:  fmt.Sprintf("Successfully connected to database %s on %s:%d", profile.Database, profile.Host, profile.Port),
			}
		}
		lastErr = err
	}

	return driver.DoctorCheck{
		Name:     "Database Connection: " + profile.Name,
		Severity: driver.CheckError,
		OK:       false,
		Message:  fmt.Sprintf("Failed to connect after %d attempts: %v", connectRetryMax+1, lastErr),
		Hint:     "Verify host details, credentials, and ensure the server/network is reachable.",
	}
}

func (d *PostgresDriver) checkVersionVsServer(profile config.Profile) driver.DoctorCheck {
	serverVer, err := d.fetchServerVersion(profile)
	if err != nil {
		return driver.DoctorCheck{} // Skip if unable to query
	}

	clientVer, err := localBinaryVersion("pg_restore")
	if err != nil {
		return driver.DoctorCheck{} // Skip if client version can't be parsed
	}

	if clientVer < serverVer {
		return driver.DoctorCheck{
			Name:     "pg_restore version check",
			Severity: driver.CheckWarning,
			OK:       false,
			Message:  fmt.Sprintf("Local pg_restore version (%d) is older than PostgreSQL server version (%d)", clientVer, serverVer),
			Hint:     "Generally backwards compatible, but newer dump structures might fail. Upgrading local client utilities is recommended.",
		}
	}

	return driver.DoctorCheck{
		Name:     "pg_restore version check",
		Severity: driver.CheckOK,
		OK:       true,
		Message:  fmt.Sprintf("Local client and server versions are compatible (%d >= %d)", clientVer, serverVer),
	}
}

func (d *PostgresDriver) fetchServerVersion(profile config.Profile) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?connect_timeout=3",
		profile.User, profile.Password, profile.Host, profile.Port, profile.Database)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return 0, err
	}
	defer conn.Close(ctx)

	var verStr string
	err = conn.QueryRow(ctx, "SHOW server_version;").Scan(&verStr)
	if err != nil {
		return 0, err
	}

	return parseMajorVersion(verStr)
}

func localBinaryVersion(binaryName string) (int, error) {
	cmd := exec.Command(binaryName, "--version")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	return parseMajorVersion(string(out))
}

func parseMajorVersion(verStr string) (int, error) {
	// Look for a number pattern like "16.2" or "17"
	re := regexp.MustCompile(`\b(\d+)(?:\.(\d+))?\b`)
	matches := re.FindStringSubmatch(verStr)
	if len(matches) < 2 {
		return 0, fmt.Errorf("could not find version number in string: %q", verStr)
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}
	return major, nil
}

func (d *PostgresDriver) EnsureDatabaseExists(ctx context.Context, profile config.Profile) error {
	// Check connection to target DB first
	err := d.TestConnection(ctx, profile)
	if err == nil {
		return nil
	}

	// Try to connect to "postgres" system database
	sysProfile := profile
	sysProfile.Database = "postgres"
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?connect_timeout=3",
		sysProfile.User, sysProfile.Password, sysProfile.Host, sysProfile.Port, sysProfile.Database,
	)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to system database 'postgres' to check/create target database: %w", err)
	}
	defer conn.Close(ctx)

	// Check if target database exists
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)"
	err = conn.QueryRow(ctx, query, profile.Database).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if database exists: %w", err)
	}

	if exists {
		return nil
	}

	// Create database using sanitized name
	safeDBName := pgx.Identifier{profile.Database}.Sanitize()
	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", safeDBName))
	if err != nil {
		return fmt.Errorf("failed to create database %s: %w", profile.Database, err)
	}

	return nil
}
