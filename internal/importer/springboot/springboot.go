package springboot

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"dbtool/internal/importer"

	"gopkg.in/yaml.v3"
)

type SpringBootImporter struct{}

func (s *SpringBootImporter) Name() string {
	return "springboot"
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

func (s *SpringBootImporter) Detect(dir string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		name := strings.ToLower(d.Name())
		if strings.HasPrefix(name, "application") &&
			(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".properties")) {
			found = append(found, path)
		}
		return nil
	})
	return found, err
}

func (s *SpringBootImporter) Parse(filePath string) ([]importer.ImportedProfile, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".properties" {
		return s.parseProperties(filePath)
	}
	return s.parseYAML(filePath)
}

func (s *SpringBootImporter) parseProperties(filePath string) ([]importer.ImportedProfile, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	props := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			props[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	// Look for spring.datasource.* properties
	// Spring properties can have prefix like spring.datasource.url or spring.datasource.jdbc-url
	// We also support spring.datasource.<name>.url for multi-datasource
	type rawDS struct {
		url      string
		username string
		password string
		path     string
	}
	dsMap := make(map[string]*rawDS)

	for k, v := range props {
		if !strings.HasPrefix(k, "spring.datasource") {
			continue
		}
		suffix := strings.TrimPrefix(k, "spring.datasource")
		if suffix == "" {
			continue
		}
		// check if suffix is e.g. .url or .jdbc-url
		if suffix == ".url" || suffix == ".jdbc-url" || suffix == ".jdbcUrl" {
			if _, ok := dsMap["default"]; !ok {
				dsMap["default"] = &rawDS{path: "spring.datasource"}
			}
			dsMap["default"].url = v
		} else if suffix == ".username" {
			if _, ok := dsMap["default"]; !ok {
				dsMap["default"] = &rawDS{path: "spring.datasource"}
			}
			dsMap["default"].username = v
		} else if suffix == ".password" {
			if _, ok := dsMap["default"]; !ok {
				dsMap["default"] = &rawDS{path: "spring.datasource"}
			}
			dsMap["default"].password = v
		} else {
			// Multi datasource support: spring.datasource.<name>.url
			parts := strings.Split(suffix, ".")
			if len(parts) >= 3 {
				// suffix has leading '.' so parts[0] is empty, parts[1] is name, parts[2] is property
				name := parts[1]
				prop := parts[2]
				if _, ok := dsMap[name]; !ok {
					dsMap[name] = &rawDS{path: "spring.datasource." + name}
				}
				if prop == "url" || prop == "jdbc-url" || prop == "jdbcUrl" {
					dsMap[name].url = v
				} else if prop == "username" {
					dsMap[name].username = v
				} else if prop == "password" {
					dsMap[name].password = v
				}
			}
		}
	}

	var results []importer.ImportedProfile
	for name, r := range dsMap {
		if r.url == "" {
			continue
		}
		profile := buildImportedProfile(filePath, name, r.url, r.username, r.password, r.path)
		results = append(results, profile)
	}

	return results, nil
}

func (s *SpringBootImporter) parseYAML(filePath string) ([]importer.ImportedProfile, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	var results []importer.ImportedProfile

	for {
		var node interface{}
		err := dec.Decode(&node)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		var rawDsList []map[string]string
		findDatasources(node, nil, &rawDsList)

		for i, ds := range rawDsList {
			name := fmt.Sprintf("ds%d", i)
			// Suggest suggestedName from config file
			profile := buildImportedProfile(filePath, name, ds["url"], ds["username"], ds["password"], ds["path"])
			results = append(results, profile)
		}
	}

	return results, nil
}

func findDatasources(node interface{}, path []string, results *[]map[string]string) {
	m, ok := node.(map[string]interface{})
	if !ok {
		if list, ok := node.([]interface{}); ok {
			for _, item := range list {
				findDatasources(item, path, results)
			}
		}
		return
	}

	insideDatasource := false
	for _, p := range path {
		if p == "datasource" {
			insideDatasource = true
			break
		}
	}

	if insideDatasource {
		url := getStringValue(m, "url", "jdbc-url", "jdbcUrl")
		username := getStringValue(m, "username")
		password := getStringValue(m, "password")
		if url != "" {
			*results = append(*results, map[string]string{
				"url":      url,
				"username": username,
				"password": password,
				"path":     strings.Join(path, "."),
			})
			return
		}
	}

	for k, v := range m {
		findDatasources(v, append(path, k), results)
	}
}

func getStringValue(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if val, ok := m[k]; ok {
			if s, ok := val.(string); ok {
				return s
			}
			if val != nil {
				return fmt.Sprintf("%v", val)
			}
		}
	}
	return ""
}

var jdbcPattern = regexp.MustCompile(`^jdbc:(\w+)://([^:/]+)(?::(\d+))?/([^?]+)`)

var jdbcSchemeToDriver = map[string]string{
	"postgresql": "postgres",
	"postgres":   "postgres",
	"mysql":      "mysql",
	"mariadb":    "mysql",
}

func parseJDBCURL(url string) (driverName, host string, port int, database string, err error) {
	m := jdbcPattern.FindStringSubmatch(url)
	if m == nil {
		return "", "", 0, "", fmt.Errorf("unrecognized JDBC URL: %s", url)
	}
	driverName = jdbcSchemeToDriver[m[1]]
	if driverName == "" {
		driverName = m[1] // fallback if unrecognized scheme
	}
	host = m[2]
	if m[3] != "" {
		port, _ = strconv.Atoi(m[3])
	}
	database = m[4]
	return
}

var placeholderPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::([^}]*))?\}`)

func resolveString(raw string) (string, bool) {
	unresolved := false
	result := placeholderPattern.ReplaceAllStringFunc(raw, func(match string) string {
		m := placeholderPattern.FindStringSubmatch(match)
		if m == nil {
			return match
		}
		envVar := m[1]
		defaultVal := ""
		if len(m) > 2 {
			defaultVal = m[2]
		}
		if val, ok := os.LookupEnv(envVar); ok {
			return val
		}
		if defaultVal != "" {
			return defaultVal
		}
		unresolved = true
		return ""
	})
	return result, unresolved
}

func buildImportedProfile(filePath, name, rawURL, rawUser, rawPass, path string) importer.ImportedProfile {
	urlResolved, _ := resolveString(rawURL)
	usernameResolved, _ := resolveString(rawUser)
	passwordResolved, passUnresolved := resolveString(rawPass)

	driverName, host, port, dbname, err := parseJDBCURL(urlResolved)
	if err != nil {
		driverName = "postgres" // default fallback
	}

	if port == 0 {
		if driverName == "postgres" {
			port = 5432
		} else if driverName == "mysql" {
			port = 3306
		}
	}

	suggestedName := "default"
	base := filepath.Base(filePath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if strings.HasPrefix(base, "application-") {
		suggestedName = strings.TrimPrefix(base, "application-")
	} else if base == "application" {
		suggestedName = "local"
	}
	if name != "default" && name != "ds0" {
		suggestedName = suggestedName + "-" + name
	}

	return importer.ImportedProfile{
		SuggestedName: suggestedName,
		Driver:        driverName,
		Host:          host,
		Port:          port,
		Database:      dbname,
		Username:      usernameResolved,
		Password:      passwordResolved,
		PasswordIsRef: passUnresolved,
		Source:        fmt.Sprintf("%s -> %s", filepath.Base(filePath), path),
	}
}

func init() {
	importer.Register(&SpringBootImporter{})
}
