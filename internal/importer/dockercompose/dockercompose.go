package dockercompose

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"dbtool/internal/importer"

	"gopkg.in/yaml.v3"
)

type DockerComposeImporter struct{}

func (d *DockerComposeImporter) Name() string {
	return "docker-compose"
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

func (d *DockerComposeImporter) Detect(dir string) ([]string, error) {
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
		if name == "docker-compose.yml" || name == "docker-compose.yaml" || name == "compose.yml" || name == "compose.yaml" {
			found = append(found, path)
		}
		return nil
	})
	return found, err
}

type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
}

type ComposeService struct {
	Image       string      `yaml:"image"`
	Ports       []interface{} `yaml:"ports"`
	Environment interface{} `yaml:"environment"`
}

func (d *DockerComposeImporter) Parse(filePath string) ([]importer.ImportedProfile, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var compose ComposeFile
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&compose); err != nil {
		return nil, err
	}

	var results []importer.ImportedProfile

	for serviceName, service := range compose.Services {
		dbType := detectDatabaseType(serviceName, service.Image)
		if dbType == "" {
			continue
		}

		envMap := parseEnvironment(service.Environment)
		hostPort, containerPort := parsePorts(service.Ports, dbType)

		// Extract credentials depending on database type
		var dbname, user, pass string
		switch dbType {
		case "postgres":
			dbname = envMap["POSTGRES_DB"]
			user = envMap["POSTGRES_USER"]
			pass = envMap["POSTGRES_PASSWORD"]
			if user == "" {
				user = "postgres" // default Postgres user
			}
		case "mysql":
			dbname = envMap["MYSQL_DATABASE"]
			user = envMap["MYSQL_USER"]
			pass = envMap["MYSQL_PASSWORD"]
			if pass == "" {
				pass = envMap["MYSQL_ROOT_PASSWORD"]
			}
			if user == "" && pass == envMap["MYSQL_ROOT_PASSWORD"] {
				user = "root"
			}
		case "mongodb":
			dbname = envMap["MONGO_INITDB_DATABASE"]
			user = envMap["MONGO_INITDB_ROOT_USERNAME"]
			pass = envMap["MONGO_INITDB_ROOT_PASSWORD"]
		}

		// Clean DB credentials (resolve placeholder if needed, though docker-compose environment vars are usually literals or standard env vars)
		dbname = resolveValue(dbname)
		user = resolveValue(user)
		pass = resolveValue(pass)

		// We need at least hostPort or containerPort to connect to.
		// If docker-compose service exposes no ports to host, we default to localhost with container's standard port.
		port := hostPort
		if port == 0 {
			port = containerPort
		}
		if port == 0 {
			switch dbType {
			case "postgres":
				port = 5432
			case "mysql":
				port = 3306
			case "mongodb":
				port = 27017
			}
		}

		// Build suggested profile name
		suggestedName := "compose-" + serviceName
		if dbname == "" {
			dbname = "postgres" // fallback DB name for default connection checking
		}

		results = append(results, importer.ImportedProfile{
			SuggestedName: suggestedName,
			Driver:        dbType,
			Host:          "localhost", // docker-compose runs locally, so host machine accesses it via localhost
			Port:          port,
			Database:      dbname,
			Username:      user,
			Password:      pass,
			PasswordIsRef: isRef(pass),
			Source:        fmt.Sprintf("%s -> service %q", filepath.Base(filePath), serviceName),
		})
	}

	return results, nil
}

func detectDatabaseType(serviceName, image string) string {
	serviceName = strings.ToLower(serviceName)
	image = strings.ToLower(image)

	// Exclude tools like pgadmin, phpmyadmin, mongo-express, etc.
	excludePatterns := []string{"admin", "express", "web", "gui", "client"}
	for _, p := range excludePatterns {
		if strings.Contains(serviceName, p) || strings.Contains(image, p) {
			return ""
		}
	}

	if strings.Contains(image, "postgres") || strings.Contains(serviceName, "postgres") || strings.Contains(serviceName, "pg") {
		return "postgres"
	}
	if strings.Contains(image, "mysql") || strings.Contains(serviceName, "mysql") {
		return "mysql"
	}
	if strings.Contains(image, "mariadb") || strings.Contains(serviceName, "mariadb") {
		return "mysql"
	}
	if strings.Contains(image, "mongo") || strings.Contains(serviceName, "mongo") {
		return "mongodb"
	}

	// Secondary check: if name is just "db" or "database" and image is postgres/mysql/mariadb/mongo
	if serviceName == "db" || serviceName == "database" {
		if strings.Contains(image, "postgres") {
			return "postgres"
		}
		if strings.Contains(image, "mysql") || strings.Contains(image, "mariadb") {
			return "mysql"
		}
		if strings.Contains(image, "mongo") {
			return "mongodb"
		}
	}

	return ""
}

func parseEnvironment(env interface{}) map[string]string {
	res := make(map[string]string)
	if env == nil {
		return res
	}

	switch v := env.(type) {
	case map[string]interface{}:
		for key, val := range v {
			if val != nil {
				res[key] = fmt.Sprintf("%v", val)
			}
		}
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				parts := strings.SplitN(str, "=", 2)
				if len(parts) == 2 {
					res[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return res
}

func parsePorts(ports []interface{}, dbType string) (hostPort int, containerPort int) {
	defaultContainerPort := 5432
	switch dbType {
	case "postgres":
		defaultContainerPort = 5432
	case "mysql":
		defaultContainerPort = 3306
	case "mongodb":
		defaultContainerPort = 27017
	}

	if len(ports) == 0 {
		return 0, defaultContainerPort
	}

	// Parse first port mapping
	firstPort := ports[0]
	var portStr string
	switch p := firstPort.(type) {
	case int:
		return p, p
	case string:
		portStr = p
	default:
		portStr = fmt.Sprintf("%v", firstPort)
	}

	// Port can be "80", "5432:5432", "127.0.0.1:5432:5432", "5430-5435:5430-5435"
	parts := strings.Split(portStr, ":")
	if len(parts) == 1 {
		// Just container port, e.g. "5432"
		cp, _ := strconv.Atoi(parts[0])
		return 0, cp
	} else if len(parts) == 2 {
		// e.g. "5433:5432"
		hp, _ := strconv.Atoi(parts[0])
		cp, _ := strconv.Atoi(parts[1])
		return hp, cp
	} else if len(parts) == 3 {
		// e.g. "127.0.0.1:5433:5432"
		hp, _ := strconv.Atoi(parts[1])
		cp, _ := strconv.Atoi(parts[2])
		return hp, cp
	}

	return 0, defaultContainerPort
}

var placeholderPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func resolveValue(raw string) string {
	return placeholderPattern.ReplaceAllStringFunc(raw, func(match string) string {
		m := placeholderPattern.FindStringSubmatch(match)
		if m == nil {
			return match
		}
		var envVar string
		var defaultVal string
		if m[1] != "" {
			envVar = m[1]
			defaultVal = m[2]
		} else {
			envVar = m[3]
		}

		if val, ok := os.LookupEnv(envVar); ok {
			return val
		}
		return defaultVal
	})
}

func isRef(raw string) bool {
	return placeholderPattern.MatchString(raw)
}

func init() {
	importer.Register(&DockerComposeImporter{})
}
