package pitr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PgHBAUpdateResult describes an attempted pg_hba.conf update.
type PgHBAUpdateResult struct {
	Changed    bool
	BackupPath string
	Rule       string
}

// EnsureReplicationPgHBA appends rule to pg_hba.conf if it is not already present.
// Before changing the file it creates a timestamped backup next to the original file.
func EnsureReplicationPgHBA(hbaPath, rule string, now time.Time) (PgHBAUpdateResult, error) {
	result := PgHBAUpdateResult{Rule: rule}

	if strings.TrimSpace(hbaPath) == "" {
		return result, fmt.Errorf("pg_hba.conf path is empty")
	}
	if strings.TrimSpace(rule) == "" {
		return result, fmt.Errorf("pg_hba.conf rule is empty")
	}

	info, err := os.Stat(hbaPath)
	if err != nil {
		return result, fmt.Errorf("cannot stat pg_hba.conf %s: %w", hbaPath, err)
	}
	if info.IsDir() {
		hbaPath = filepath.Join(hbaPath, "pg_hba.conf")
		info, err = os.Stat(hbaPath)
		if err != nil {
			return result, fmt.Errorf("cannot stat pg_hba.conf %s: %w", hbaPath, err)
		}
	}

	data, err := os.ReadFile(hbaPath)
	if err != nil {
		return result, fmt.Errorf("cannot read pg_hba.conf %s: %w", hbaPath, err)
	}

	if pgHBARuleExists(string(data), rule) {
		return result, nil
	}

	if now.IsZero() {
		now = time.Now()
	}
	backupPath := fmt.Sprintf("%s.dbtool-backup-%s", hbaPath, now.Format("20060102-150405"))
	if _, err := os.Stat(backupPath); err == nil {
		backupPath = fmt.Sprintf("%s.%d", backupPath, now.UnixNano())
	} else if !os.IsNotExist(err) {
		return result, fmt.Errorf("cannot check pg_hba.conf backup path %s: %w", backupPath, err)
	}

	if err := os.WriteFile(backupPath, data, info.Mode().Perm()); err != nil {
		return result, fmt.Errorf("cannot create pg_hba.conf backup %s: %w", backupPath, err)
	}

	var builder strings.Builder
	builder.Write(data)
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		builder.WriteByte('\n')
	}
	builder.WriteString("\n# Added by dbtool PITR setup for pg_basebackup replication access\n")
	builder.WriteString(rule)
	builder.WriteByte('\n')

	tmpPath := filepath.Join(filepath.Dir(hbaPath), "."+filepath.Base(hbaPath)+".dbtool.tmp")
	if err := os.WriteFile(tmpPath, []byte(builder.String()), info.Mode().Perm()); err != nil {
		return result, fmt.Errorf("cannot write temporary pg_hba.conf %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, hbaPath); err != nil {
		_ = os.Remove(tmpPath)
		return result, fmt.Errorf("cannot replace pg_hba.conf %s: %w", hbaPath, err)
	}

	result.Changed = true
	result.BackupPath = backupPath
	return result, nil
}

func pgHBARuleExists(content, rule string) bool {
	want := strings.Join(strings.Fields(rule), " ")
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Join(strings.Fields(trimmed), " ") == want {
			return true
		}
	}
	return false
}
