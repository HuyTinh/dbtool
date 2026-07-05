package safety

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// CheckDatabaseHasData checks if the specified Postgres database has any user tables.
func CheckDatabaseHasData(ctx context.Context, host string, port int, user, password, dbname string) (bool, error) {
	// construct pg connection string
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?connect_timeout=3", user, password, host, port, dbname)

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return false, fmt.Errorf("failed to connect to database for safety check: %w", err)
	}
	defer conn.Close(ctx)

	var count int
	// Query for tables in non-system schemas
	query := `
		SELECT count(*) 
		FROM information_schema.tables 
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema') 
		  AND table_schema NOT LIKE 'pg_toast%'
	`
	err = conn.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to scan table count for safety check: %w", err)
	}

	return count > 0, nil
}

// PromptConfirm asks the user for confirmation on terminal.
func PromptConfirm(prompt string) bool {
	fmt.Print(prompt + " [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	text = strings.TrimSpace(strings.ToLower(text))
	return text == "y" || text == "yes"
}
