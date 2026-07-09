package integrity

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type TOCEntry struct {
	ID     string
	OID1   string
	OID2   string
	Type   string
	Schema string
	Name   string
	Owner  string
}

type TOCEntryList []TOCEntry

func (entries TOCEntryList) Tables() []TOCEntry {
	var result []TOCEntry
	for _, e := range entries {
		if e.Type == "TABLE" {
			result = append(result, e)
		}
	}
	return result
}

func (entries TOCEntryList) Views() []TOCEntry {
	var result []TOCEntry
	for _, e := range entries {
		if e.Type == "VIEW" || e.Type == "MATERIALIZED VIEW" {
			result = append(result, e)
		}
	}
	return result
}

func (entries TOCEntryList) Schemas() []string {
	seen := make(map[string]bool)
	var result []string
	for _, e := range entries {
		if e.Schema != "" && !seen[e.Schema] {
			seen[e.Schema] = true
			result = append(result, e.Schema)
		}
	}
	return result
}

func ParseTOC(filePath string) (TOCEntryList, string, error) {
	binary := "pg_restore"
	if _, err := exec.LookPath(binary); err != nil {
		return nil, "", fmt.Errorf("pg_restore not found in PATH: %w", err)
	}

	cmd := exec.Command(binary, "-l", filePath)
	out, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("failed to read archive TOC (file might be plain SQL or corrupt): %w", err)
	}

	raw := string(out)
	version := extractDumpVersion(raw)

	var entries TOCEntryList
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		parts := strings.SplitN(line, ";", 2)
		if len(parts) < 2 {
			continue
		}

		id := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])
		if len(fields) < 5 {
			continue
		}

		// Handle multi-word types like "MATERIALIZED VIEW"
		objType := fields[2]
		schemaIdx := 3
		if objType == "MATERIALIZED" && len(fields) > 3 && fields[3] == "VIEW" {
			objType = "MATERIALIZED VIEW"
			schemaIdx = 4
		}

		if schemaIdx+2 > len(fields) {
			continue
		}

		entry := TOCEntry{
			ID:     id,
			OID1:   fields[0],
			OID2:   fields[1],
			Type:   objType,
			Schema: fields[schemaIdx],
			Name:   fields[schemaIdx+1],
		}
		if schemaIdx+2 < len(fields) {
			entry.Owner = fields[schemaIdx+2]
		}
		entries = append(entries, entry)
	}

	return entries, version, nil
}

var pgVersionRegex = regexp.MustCompile(`;\s*Dumped (?:from|by)\s+.*?(\d+\.\d+(?:\.\d+)?)`)

func extractDumpVersion(rawTOC string) string {
	scanner := bufio.NewScanner(strings.NewReader(rawTOC))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, ";") {
			continue
		}
		if matches := pgVersionRegex.FindStringSubmatch(line); len(matches) > 1 {
			return matches[1]
		}
	}
	return ""
}
