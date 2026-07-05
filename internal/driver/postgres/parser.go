package postgres

import (
	"regexp"
	"strings"

	"dbtool/internal/driver"
)

var knownHarmlessPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)role ".*" already exists`),
	regexp.MustCompile(`(?i)relation ".*" already exists, skipping`),
	regexp.MustCompile(`(?i)already exists`),
}

func parseLine(line string) driver.Progress {
	line = strings.TrimSpace(line)
	if line == "" {
		return driver.Progress{}
	}
	return driver.Progress{
		Message: line,
	}
}

func classifyStderrLine(line string) driver.Progress {
	line = strings.TrimSpace(line)
	if line == "" {
		return driver.Progress{}
	}

	// Check if the line matches any known harmless patterns
	for _, p := range knownHarmlessPatterns {
		if p.MatchString(line) {
			return driver.Progress{
				Message: "[Ignored Warning] " + line,
			}
		}
	}

	// Flag errors but do not set Progress.Err directly, as some errors might not fail pg_restore
	if strings.Contains(strings.ToUpper(line), "ERROR") {
		return driver.Progress{
			Message: "[Database Error] " + line,
		}
	}

	return driver.Progress{
		Message: line,
	}
}
