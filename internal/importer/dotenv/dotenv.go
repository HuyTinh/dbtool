package dotenv

import (
	"bufio"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"dbtool/internal/importer"
)

type DotenvImporter struct{}

func (d *DotenvImporter) Name() string {
	return "dotenv"
}

var skipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	"vendor":       true,
	"target":       true,
	"build":        true,
	"dist":         true,
	".gradle":      true,
	".idea":        true,
	".vscode":      true,
}

func (d *DotenvImporter) Detect(dir string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(dir, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.IsDir() {
			if skipDirs[de.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(de.Name())
		if name == ".env" || (strings.HasPrefix(name, ".env.") && !strings.HasSuffix(name, ".example") && !strings.HasSuffix(name, ".sample")) {
			found = append(found, path)
		}
		return nil
	})
	return found, err
}

func (d *DotenvImporter) Parse(filePath string) ([]importer.ImportedProfile, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	envMap := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])

			// Strip quotes if present
			if len(val) >= 2 {
				if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
					val = val[1 : len(val)-1]
				}
			}
			envMap[key] = val
		}
	}

	// Resolve environment variable references in the parsed map
	resolvedMap := make(map[string]string)
	for k, v := range envMap {
		resolvedMap[k] = resolveVal(v, envMap)
	}

	var results []importer.ImportedProfile

	// 1. Check for URL-based connection strings
	urlKeys := []string{"DATABASE_URL", "DB_URL", "POSTGRES_URL", "PG_URL"}
	var dbURL string
	var urlSource string
	for _, key := range urlKeys {
		if val, ok := resolvedMap[key]; ok && val != "" {
			dbURL = val
			urlSource = key
			break
		}
	}

	if dbURL != "" {
		if profile, err := parseConnectionURL(dbURL, filePath, urlSource); err == nil {
			results = append(results, profile)
		}
	}

	// 2. Check for separate keys
	var host, dbname, user, pass, driver string
	var port int

	hostKeys := []string{"DB_HOST", "DATABASE_HOST", "POSTGRES_HOST", "PGHOST"}
	portKeys := []string{"DB_PORT", "DATABASE_PORT", "POSTGRES_PORT", "PGPORT"}
	dbKeys := []string{"DB_DATABASE", "DB_NAME", "DATABASE_NAME", "POSTGRES_DB", "PGDATABASE"}
	userKeys := []string{"DB_USERNAME", "DB_USER", "DATABASE_USER", "POSTGRES_USER", "PGUSER"}
	passKeys := []string{"DB_PASSWORD", "DATABASE_PASSWORD", "POSTGRES_PASSWORD", "PGPASSWORD"}
	driverKeys := []string{"DB_CONNECTION", "DB_DRIVER", "DATABASE_DRIVER"}

	for _, k := range hostKeys {
		if val := resolvedMap[k]; val != "" {
			host = val
			break
		}
	}
	for _, k := range portKeys {
		if val := resolvedMap[k]; val != "" {
			if p, err := strconv.Atoi(val); err == nil {
				port = p
			}
			break
		}
	}
	for _, k := range dbKeys {
		if val := resolvedMap[k]; val != "" {
			dbname = val
			break
		}
	}
	for _, k := range userKeys {
		if val := resolvedMap[k]; val != "" {
			user = val
			break
		}
	}
	for _, k := range passKeys {
		if val := resolvedMap[k]; val != "" {
			pass = val
			break
		}
	}
	for _, k := range driverKeys {
		if val := resolvedMap[k]; val != "" {
			driver = val
			break
		}
	}

	// Fallback/Inference
	if driver == "" {
		driver = "postgres" // default
	} else {
		driver = strings.ToLower(driver)
		if driver == "postgresql" {
			driver = "postgres"
		}
	}

	if port == 0 {
		if driver == "postgres" {
			port = 5432
		} else if driver == "mysql" {
			port = 3306
		}
	}

	// Only add separate profile if we have at least a host and database
	if host != "" && dbname != "" {
		suggestedName := getSuggestedName(filePath, "")
		results = append(results, importer.ImportedProfile{
			SuggestedName: suggestedName,
			Driver:        driver,
			Host:          host,
			Port:          port,
			Database:      dbname,
			Username:      user,
			Password:      pass,
			PasswordIsRef: isRef(envMap[getHostKey(resolvedMap, passKeys)]),
			Source:        fmt.Sprintf("%s -> separate keys", filepath.Base(filePath)),
		})
	}

	return results, nil
}

func getHostKey(resolved map[string]string, keys []string) string {
	for _, k := range keys {
		if _, ok := resolved[k]; ok {
			return k
		}
	}
	return ""
}

var placeholderPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func resolveVal(raw string, envMap map[string]string) string {
	return placeholderPattern.ReplaceAllStringFunc(raw, func(match string) string {
		m := placeholderPattern.FindStringSubmatch(match)
		if m == nil {
			return match
		}
		var envVar string
		var defaultVal string
		if m[1] != "" { // matches ${VAR...}
			envVar = m[1]
			defaultVal = m[2]
		} else { // matches $VAR
			envVar = m[3]
		}

		if val, ok := os.LookupEnv(envVar); ok {
			return val
		}
		if val, ok := envMap[envVar]; ok {
			return val
		}
		return defaultVal
	})
}

func isRef(raw string) bool {
	return placeholderPattern.MatchString(raw)
}

func parseConnectionURL(rawURL, filePath, key string) (importer.ImportedProfile, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return importer.ImportedProfile{}, err
	}

	scheme := strings.ToLower(u.Scheme)
	driver := scheme
	if scheme == "postgresql" {
		driver = "postgres"
	}

	host := u.Hostname()
	portStr := u.Port()
	port := 0
	if portStr != "" {
		port, _ = strconv.Atoi(portStr)
	}

	if port == 0 {
		if driver == "postgres" {
			port = 5432
		} else if driver == "mysql" {
			port = 3306
		} else if driver == "mongodb" {
			port = 27017
		}
	}

	username := u.User.Username()
	password, _ := u.User.Password()
	database := strings.TrimPrefix(u.Path, "/")

	suggestedName := getSuggestedName(filePath, scheme)

	return importer.ImportedProfile{
		SuggestedName: suggestedName,
		Driver:        driver,
		Host:          host,
		Port:          port,
		Database:      database,
		Username:      username,
		Password:      password,
		PasswordIsRef: isRef(rawURL),
		Source:        fmt.Sprintf("%s -> %s", filepath.Base(filePath), key),
	}, nil
}

func getSuggestedName(filePath string, dbType string) string {
	base := filepath.Base(filePath)
	base = strings.TrimSuffix(base, filepath.Ext(base))

	var suffix string
	if strings.HasPrefix(base, ".env.") {
		suffix = strings.TrimPrefix(base, ".env.")
	} else if base == ".env" {
		suffix = "local"
	}

	if suffix == "" {
		suffix = "dotenv"
	}

	if dbType != "" && dbType != "postgres" && dbType != "postgresql" {
		suffix = suffix + "-" + dbType
	}

	return suffix
}

func init() {
	importer.Register(&DotenvImporter{})
}
