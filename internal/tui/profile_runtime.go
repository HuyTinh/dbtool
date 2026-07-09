package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/importer"
	dockercompose "dbtool/internal/importer/dockercompose"

	tea "github.com/charmbracelet/bubbletea"
)

type runtimeCandidate struct {
	profile importer.ImportedProfile
	score   int
}

var (
	dockerRuntimeProfileDetector  = detectDockerRuntimeProfile
	composeRuntimeProfileDetector = detectComposeRuntimeProfile
)

func (m Model) detectProfileRuntime() (tea.Model, tea.Cmd) {
	profile, err := m.profileFromForm()
	if err != nil {
		m.formErr = err.Error()
		return m, nil
	}

	m.formErr = ""
	m.formNotice = ""
	m.runtimeDetecting = true
	m.runtimeDetectPct = 10
	m.runtimeDetectText = "Checking running Docker containers..."

	updates := make(chan tea.Msg, 8)
	go runRuntimeDetection(profile, m.currentDir, updates)
	return m, listenToRuntimeDetect(updates)
}

func (m Model) finishRuntimeDetection(msg runtimeDetectFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.formErr = msg.Err.Error()
		return m, nil
	}

	m.formErr = ""
	m.formNotice = ""
	if msg.Profile.Runtime != nil {
		m.formRuntime = cloneRuntimeProfile(msg.Profile.Runtime)
		m.formInfo = formatRuntimeSummary(msg.Profile.Runtime)
		m.formRuntimeDetails = true
		m.applyDetectedProfileToForm(msg.Profile)
		return m, nil
	}

	m.formNotice = msg.Message
	if m.formRuntime != nil {
		m.formInfo = formatRuntimeSummary(m.formRuntime)
	} else {
		m.formInfo = defaultManualRuntimeMessage()
	}
	return m, nil
}

func runRuntimeDetection(profile config.Profile, dir string, updates chan tea.Msg) {
	defer close(updates)
	notify := func(percent float64, message string) {
		updates <- runtimeDetectProgressMsg{Percent: percent, Message: message, Ch: updates}
		time.Sleep(120 * time.Millisecond)
	}

	detected, message, err := detectRuntimeProfile(profile, dir, notify)
	updates <- runtimeDetectFinishedMsg{Profile: detected, Message: message, Err: err}
}

func (m Model) profileFromForm() (config.Profile, error) {
	driverName := strings.TrimSpace(m.inputs[1].Value())
	if driverName != "postgres" && driverName != "mysql" {
		return config.Profile{}, fmt.Errorf("driver must be 'postgres' or 'mysql' before runtime detection")
	}

	port := 0
	portText := strings.TrimSpace(m.inputs[3].Value())
	if portText != "" {
		parsed, err := strconv.Atoi(portText)
		if err != nil || parsed <= 0 {
			return config.Profile{}, fmt.Errorf("port must be a valid positive integer before runtime detection")
		}
		port = parsed
	}

	return config.Profile{
		Name:     strings.TrimSpace(m.inputs[0].Value()),
		Driver:   driverName,
		Host:     strings.TrimSpace(m.inputs[2].Value()),
		Port:     port,
		User:     strings.TrimSpace(m.inputs[4].Value()),
		Password: m.inputs[5].Value(),
		Database: strings.TrimSpace(m.inputs[6].Value()),
	}, nil
}

func (m *Model) applyDetectedProfileToForm(detected config.Profile) {
	if shouldAutofillHost(strings.TrimSpace(m.inputs[2].Value())) {
		m.inputs[2].SetValue(detected.Host)
	}
	if shouldAutofillPort(strings.TrimSpace(m.inputs[3].Value()), strings.TrimSpace(m.inputs[1].Value())) && detected.Port > 0 {
		m.inputs[3].SetValue(strconv.Itoa(detected.Port))
	}
	if shouldAutofillUser(strings.TrimSpace(m.inputs[4].Value()), strings.TrimSpace(m.inputs[1].Value())) {
		m.inputs[4].SetValue(detected.User)
	}
	if strings.TrimSpace(m.inputs[5].Value()) == "" && detected.Password != "" {
		m.inputs[5].SetValue(detected.Password)
	}
	if strings.TrimSpace(m.inputs[6].Value()) == "" && detected.Database != "" {
		m.inputs[6].SetValue(detected.Database)
	}
}

func detectRuntimeProfile(input config.Profile, dir string, notify func(float64, string)) (config.Profile, string, error) {
	if notify != nil {
		notify(20, "Checking running Docker containers...")
	}
	dockerDetected, dockerMessage, dockerErr := dockerRuntimeProfileDetector(input, dir)
	if dockerErr == nil && dockerDetected.Runtime != nil {
		if notify != nil {
			notify(100, "Matched running Docker container.")
		}
		return dockerDetected, dockerMessage, nil
	}

	if notify != nil {
		if dockerErr != nil {
			notify(65, "Docker CLI unavailable; scanning local compose files...")
		} else {
			notify(65, "No running container match; scanning local compose files...")
		}
	}

	composeDetected, composeMessage, composeErr := composeRuntimeProfileDetector(input, dir)
	if composeErr != nil {
		if dockerErr != nil {
			return input, "", dockerErr
		}
		return input, "", composeErr
	}
	if composeDetected.Runtime != nil {
		if notify != nil {
			notify(100, "Matched Docker Compose service.")
		}
		return composeDetected, composeMessage, nil
	}
	if dockerErr == nil && dockerMessage != "" {
		return input, dockerMessage, nil
	}
	return composeDetected, composeMessage, nil
}

func detectComposeRuntimeProfile(input config.Profile, dir string) (config.Profile, string, error) {
	imp := dockercompose.DockerComposeImporter{}
	composeFiles, err := imp.Detect(dir)
	if err != nil {
		return input, "", fmt.Errorf("runtime detection failed while scanning compose files: %w", err)
	}
	if len(composeFiles) == 0 {
		return input, "No Docker Compose files found near current workspace; keeping manual/local settings.", nil
	}

	var candidates []runtimeCandidate
	for _, composeFile := range composeFiles {
		profiles, err := imp.Parse(composeFile)
		if err != nil {
			continue
		}
		for _, candidate := range profiles {
			score := scoreImportedProfile(input, candidate)
			if score > 0 {
				candidates = append(candidates, runtimeCandidate{profile: candidate, score: score})
			}
		}
	}
	if len(candidates) == 0 {
		return input, "No Docker database service matched the current form values; keeping manual/local settings.", nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].profile.SuggestedName < candidates[j].profile.SuggestedName
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > 1 && candidates[0].score == candidates[1].score {
		return input, fmt.Sprintf("Multiple Docker services matched (%s, %s); refine host/port/database before detecting again.", candidates[0].profile.SuggestedName, candidates[1].profile.SuggestedName), nil
	}

	matched := importedProfileToConfig(candidates[0].profile)
	merged := mergeDetectedProfile(input, matched)
	return merged, formatRuntimeSummary(matched.Runtime), nil
}

func scoreImportedProfile(input config.Profile, candidate importer.ImportedProfile) int {
	if candidate.Driver != input.Driver {
		return -1
	}

	score := 10
	if isLocalHost(input.Host) && isLocalHost(candidate.Host) {
		score += 5
	}
	if input.Port > 0 && candidate.Port == input.Port {
		score += 50
	}
	if input.Database != "" && strings.EqualFold(candidate.Database, input.Database) {
		score += 25
	}
	if input.User != "" && strings.EqualFold(candidate.Username, input.User) {
		score += 15
	}
	if input.Name != "" {
		lowerName := strings.ToLower(input.Name)
		if strings.Contains(strings.ToLower(candidate.SuggestedName), lowerName) {
			score += 10
		}
		if candidate.Runtime != nil && strings.Contains(strings.ToLower(candidate.Runtime.ServiceName), lowerName) {
			score += 10
		}
	}
	if candidate.Runtime != nil && candidate.Runtime.Container != "" && input.Name != "" && strings.Contains(strings.ToLower(candidate.Runtime.Container), strings.ToLower(input.Name)) {
		score += 5
	}
	return score
}

func importedProfileToConfig(p importer.ImportedProfile) config.Profile {
	return config.Profile{
		Name:     p.SuggestedName,
		Driver:   p.Driver,
		Host:     p.Host,
		Port:     p.Port,
		User:     p.Username,
		Password: p.Password,
		Database: p.Database,
		Runtime:  cloneRuntimeProfile(p.Runtime),
	}
}

func mergeDetectedProfile(current, detected config.Profile) config.Profile {
	merged := current
	if shouldAutofillHost(current.Host) {
		merged.Host = detected.Host
	}
	if shouldAutofillPort(strconv.Itoa(current.Port), current.Driver) && detected.Port > 0 {
		merged.Port = detected.Port
	}
	if shouldAutofillUser(current.User, current.Driver) {
		merged.User = detected.User
	}
	if strings.TrimSpace(current.Password) == "" {
		merged.Password = detected.Password
	}
	if strings.TrimSpace(current.Database) == "" {
		merged.Database = detected.Database
	}
	merged.Runtime = cloneRuntimeProfile(detected.Runtime)
	return merged
}

func shouldAutofillHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	return host == "" || host == "localhost" || host == "127.0.0.1"
}

func shouldAutofillPort(portText, driverName string) bool {
	portText = strings.TrimSpace(portText)
	if portText == "" {
		return true
	}
	return portText == strconv.Itoa(defaultPort(driverName))
}

func shouldAutofillUser(user, driverName string) bool {
	user = strings.TrimSpace(strings.ToLower(user))
	return user == "" || user == strings.ToLower(defaultUser(driverName))
}

func defaultPort(driverName string) int {
	switch driverName {
	case "mysql":
		return 3306
	default:
		return 5432
	}
}

func defaultUser(driverName string) string {
	switch driverName {
	case "mysql":
		return "root"
	default:
		return "postgres"
	}
}

func isLocalHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	return host == "" || host == "localhost" || host == "127.0.0.1"
}

func formatRuntimeSummary(runtimeProfile *config.RuntimeProfile) string {
	if runtimeProfile == nil {
		return defaultManualRuntimeMessage()
	}
	parts := []string{fmt.Sprintf("Runtime: %s", runtimeProfile.Type)}
	if runtimeProfile.Source != "" {
		parts = append(parts, runtimeProfile.Source)
	}
	if runtimeProfile.ServiceName != "" {
		parts = append(parts, "service "+runtimeProfile.ServiceName)
	}
	if runtimeProfile.Container != "" {
		parts = append(parts, "container "+runtimeProfile.Container)
	}
	if runtimeProfile.SourceFile != "" {
		parts = append(parts, filepath.Base(runtimeProfile.SourceFile))
	}
	return strings.Join(parts, " • ")
}

func defaultManualRuntimeMessage() string {
	return "Runtime: manual/local • Press F2/Ctrl+R to detect a running Docker container or fall back to local compose files."
}

func cloneRuntimeProfile(src *config.RuntimeProfile) *config.RuntimeProfile {
	if src == nil {
		return nil
	}
	clone := *src
	if src.Mounts != nil {
		clone.Mounts = append([]config.MountMapping(nil), src.Mounts...)
	}
	return &clone
}
