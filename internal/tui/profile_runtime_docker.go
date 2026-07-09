package tui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/importer"
)

var dockerCommandRunner = runDockerCommand

type dockerInspectRecord struct {
	Name   string `json:"Name"`
	Config struct {
		Image  string            `json:"Image"`
		Env    []string          `json:"Env"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	Mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

func detectDockerRuntimeProfile(input config.Profile, dir string) (config.Profile, string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return input, "", nil
	}

	psOutput, err := dockerCommandRunner("ps", "-q")
	if err != nil {
		return input, "", fmt.Errorf("docker runtime detection failed: %w", err)
	}

	ids := strings.Fields(strings.TrimSpace(psOutput))
	if len(ids) == 0 {
		return input, "No running Docker database containers found; checking compose files.", nil
	}

	candidates := make([]runtimeCandidate, 0, len(ids))
	for _, id := range ids {
		inspectOutput, err := dockerCommandRunner("inspect", id)
		if err != nil {
			continue
		}
		profile, ok := importedProfileFromDockerInspect(inspectOutput)
		if !ok {
			continue
		}
		score := scoreImportedProfile(input, profile)
		if score > 0 {
			candidates = append(candidates, runtimeCandidate{profile: profile, score: score})
		}
	}

	if len(candidates) == 0 {
		return input, "No running Docker database containers matched the current form values; checking compose files.", nil
	}

	return resolveRuntimeCandidates(input, candidates, "container")
}

func runDockerCommand(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return "", fmt.Errorf("%w: %s", err, trimmed)
		}
		return "", err
	}
	return string(out), nil
}

func importedProfileFromDockerInspect(raw string) (importer.ImportedProfile, bool) {
	var inspectRecords []dockerInspectRecord
	if err := json.Unmarshal([]byte(raw), &inspectRecords); err != nil {
		return importer.ImportedProfile{}, false
	}
	if len(inspectRecords) == 0 {
		return importer.ImportedProfile{}, false
	}

	rec := inspectRecords[0]
	containerName := strings.TrimPrefix(strings.TrimSpace(rec.Name), "/")
	driverName := detectDockerDatabaseType(containerName, rec.Config.Image)
	if driverName == "" {
		return importer.ImportedProfile{}, false
	}

	env := envSliceToMap(rec.Config.Env)
	database, user, password := dockerCredentialsFromEnv(driverName, env)
	if user == "" {
		user = defaultUser(driverName)
	}
	if database == "" && driverName == "postgres" {
		database = "postgres"
	}

	port := dockerHostPort(driverName, rec.NetworkSettings.Ports)
	if port == 0 {
		port = defaultPort(driverName)
	}

	labels := rec.Config.Labels
	serviceName := labels["com.docker.compose.service"]
	sourceFile := firstLabelValue(labels["com.docker.compose.project.config_files"])
	suggestedName := "docker-" + containerName
	if serviceName != "" {
		suggestedName = "docker-" + serviceName
	}

	mounts := make([]config.MountMapping, 0, len(rec.Mounts))
	for _, mount := range rec.Mounts {
		mounts = append(mounts, config.MountMapping{
			Type:   strings.ToLower(mount.Type),
			Source: mount.Source,
			Target: mount.Destination,
		})
	}

	return importer.ImportedProfile{
		SuggestedName: suggestedName,
		Driver:        driverName,
		Host:          "localhost",
		Port:          port,
		Database:      database,
		Username:      user,
		Password:      password,
		Source:        fmt.Sprintf("docker inspect -> container %q", containerName),
		Runtime: &config.RuntimeProfile{
			Type:        "docker",
			Source:      "docker-cli",
			SourceFile:  filepath.Clean(sourceFile),
			ServiceName: serviceName,
			Container:   containerName,
			Mounts:      mounts,
		},
	}, true
}

func detectDockerDatabaseType(name, image string) string {
	name = strings.ToLower(name)
	image = strings.ToLower(image)

	excludePatterns := []string{"admin", "express", "web", "gui", "client"}
	for _, pattern := range excludePatterns {
		if strings.Contains(name, pattern) || strings.Contains(image, pattern) {
			return ""
		}
	}

	switch {
	case strings.Contains(name, "postgres"), strings.Contains(name, "pg"), strings.Contains(image, "postgres"):
		return "postgres"
	case strings.Contains(name, "mysql"), strings.Contains(image, "mysql"):
		return "mysql"
	default:
		return ""
	}
}

func envSliceToMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, raw, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		result[key] = raw
	}
	return result
}

func dockerCredentialsFromEnv(driverName string, env map[string]string) (database, user, password string) {
	switch driverName {
	case "mysql":
		database = env["MYSQL_DATABASE"]
		user = env["MYSQL_USER"]
		password = env["MYSQL_PASSWORD"]
		if password == "" {
			password = env["MYSQL_ROOT_PASSWORD"]
		}
		if user == "" && password == env["MYSQL_ROOT_PASSWORD"] {
			user = "root"
		}
	default:
		database = env["POSTGRES_DB"]
		user = env["POSTGRES_USER"]
		password = env["POSTGRES_PASSWORD"]
	}
	return database, user, password
}

func dockerHostPort(driverName string, ports map[string][]struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}) int {
	primaryKey := fmt.Sprintf("%d/tcp", defaultPort(driverName))
	if port := firstDockerPort(ports[primaryKey]); port > 0 {
		return port
	}
	for _, bindings := range ports {
		if port := firstDockerPort(bindings); port > 0 {
			return port
		}
	}
	return 0
}

func firstDockerPort(bindings []struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}) int {
	for _, binding := range bindings {
		port, err := strconv.Atoi(strings.TrimSpace(binding.HostPort))
		if err == nil && port > 0 {
			return port
		}
	}
	return 0
}

func firstLabelValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	return strings.TrimSpace(parts[0])
}

func resolveRuntimeCandidates(input config.Profile, candidates []runtimeCandidate, matchedNoun string) (config.Profile, string, error) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].profile.SuggestedName < candidates[j].profile.SuggestedName
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > 1 && candidates[0].score == candidates[1].score {
		return input, fmt.Sprintf("Multiple Docker %ss matched (%s, %s); refine host/port/database before detecting again.", matchedNoun, candidates[0].profile.SuggestedName, candidates[1].profile.SuggestedName), nil
	}

	matched := importedProfileToConfig(candidates[0].profile)
	merged := mergeDetectedProfile(input, matched)
	return merged, formatRuntimeSummary(matched.Runtime), nil
}
