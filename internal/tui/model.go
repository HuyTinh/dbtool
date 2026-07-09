package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"
	"dbtool/internal/history"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gen2brain/beeep"
)

// --- Steps / screens ---
type step int

const (
	stepSelectMode step = iota // choose Restore or Dump
	stepSelectProfile
	stepSelectFile
	stepConfirm
	stepConfirmDelete
	stepEditProfileForm
	stepRestoring
	stepRestoreResult
	// Dump-specific
	stepDumpOutputPath
	stepDumpConfirm
	stepDumping
	stepDumpResult
	// Migrate-specific
	stepMigrateSelectDest
	stepMigrateConfirm
	stepMigrating
	stepMigrateResult
	stepDone
)

// --- File entry ---
type fileEntry struct {
	Name  string
	Path  string
	IsDir bool
	Size  int64
}

// --- Result returned when TUI completes ---
type Result struct {
	Profile  config.Profile
	File     string
	Settings RestoreSettings
	Confirm  bool
	// Dump-specific
	Mode         Mode
	DumpFile     string
	DumpSettings DumpSettings
	DumpConfirm  bool
	// Migrate-specific
	DestProfile     config.Profile
	MigrateSettings MigrateSettings
	MigrateConfirm  bool
}

// --- Main model ---
type Model struct {
	step step

	// Profile selector
	profiles   []config.Profile
	profileIdx int

	// Form inputs (for Add/Edit)
	inputs             []textinput.Model
	focusedIdx         int
	isEditing          bool
	origName           string
	formErr            string
	formInfo           string
	formNotice         string
	formRuntime        *config.RuntimeProfile
	formRuntimeDetails bool
	runtimeDetecting   bool
	runtimeDetectPct   float64
	runtimeDetectText  string

	// Configuration object reference for direct saving
	cfg *config.Config

	// File browser
	currentDir  string
	entries     []fileEntry
	fileIdx     int
	searchInput textinput.Model
	searching   bool

	// Result
	result Result

	// Restore progress state
	progressChan <-chan driver.Progress
	progressPct  float64
	progressText string
	restoreErr   error
	dryRunOutput string

	// Dump state
	dumpOutputInput  textinput.Model
	dumpProgressChan <-chan driver.Progress
	dumpProgressPct  float64
	dumpProgressText string
	dumpErr          error

	// Migrate state
	migrateDestIdx      int
	migrateTempPath     string
	migratePhase        int // 0=dump, 1=restore
	migrateProgressChan <-chan driver.Progress
	migrateProgressPct  float64
	migrateProgressText string
	migrateErr          error

	// Filter input mode
	filterInputMode bool
	filterInputType string // "include-table", "exclude-table", "include-schema", "exclude-schema"
	filterInput     textinput.Model

	// Terminal size
	width  int
	height int

	err error
}

func NewModel(cfg *config.Config, initSettings RestoreSettings) Model {
	profiles := profilesFromConfig(cfg)

	cwd, _ := os.Getwd()

	dumpInput := textinput.New()
	dumpInput.Placeholder = "e.g. /backups/mydb.dump"
	dumpInput.CharLimit = 512
	dumpInput.Width = 60

	searchInput := textinput.New()
	searchInput.Placeholder = "type to filter..."
	searchInput.CharLimit = 128
	searchInput.Width = 50

	filterInput := textinput.New()
	filterInput.Placeholder = "e.g. users,orders,products"
	filterInput.CharLimit = 1024
	filterInput.Width = 80

	m := Model{
		step:            stepSelectMode,
		profiles:        profiles,
		currentDir:      cwd,
		width:           80,
		height:          24,
		cfg:             cfg,
		dumpOutputInput: dumpInput,
		searchInput:     searchInput,
		filterInput:     filterInput,
	}
	m.result.Settings = initSettings
	m.result.DumpSettings = DumpSettings{Format: "custom"}
	m.result.MigrateSettings = MigrateSettings{Format: "custom", Jobs: 4}
	m.entries = listDir(cwd)
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

// --- Update ---
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case progressMsg:
		m.progressPct = msg.Percent
		m.progressText = msg.Message
		if msg.Err != nil {
			m.restoreErr = msg.Err
		}
		return m, listenToProgress(m.progressChan)

	case restoreFinishedMsg:
		m.finalizeRestore(msg.err)
		m.step = stepRestoreResult
		return m, nil

	case dumpProgressMsg:
		m.dumpProgressPct = msg.Percent
		m.dumpProgressText = msg.Message
		if msg.Err != nil {
			m.dumpErr = msg.Err
		}
		return m, listenToDumpProgress(m.dumpProgressChan)

	case dumpFinishedMsg:
		m.finalizeDump(msg.err)
		m.step = stepDumpResult
		return m, nil

	case migrateProgressMsg:
		m.migrateProgressPct = msg.Percent
		m.migrateProgressText = msg.Message
		if msg.Err != nil {
			m.migrateErr = msg.Err
		}
		return m, listenToMigrateProgress(m.migrateProgressChan, m.migratePhase)

	case migratePhaseFinishedMsg:
		return m.handleMigratePhaseFinished(msg)

	case runtimeDetectProgressMsg:
		m.runtimeDetecting = true
		m.runtimeDetectPct = msg.Percent
		m.runtimeDetectText = msg.Message
		return m, listenToRuntimeDetect(msg.Ch)

	case runtimeDetectFinishedMsg:
		m.runtimeDetecting = false
		m.runtimeDetectPct = 0
		m.runtimeDetectText = ""
		return m.finishRuntimeDetection(msg)

	case tea.KeyMsg:
		switch m.step {
		case stepSelectMode:
			return m.updateSelectMode(msg)
		case stepSelectProfile:
			return m.updateProfileSelector(msg)
		case stepSelectFile:
			return m.updateFileBrowser(msg)
		case stepConfirm:
			return m.updateConfirm(msg)
		case stepConfirmDelete:
			return m.updateConfirmDelete(msg)
		case stepEditProfileForm:
			return m.updateEditProfileForm(msg)
		case stepRestoreResult:
			switch msg.String() {
			case "q", "ctrl+c", "esc":
				m.step = stepDone
				return m, tea.Quit
			default:
				m.restoreErr = nil
				m.progressPct = 0
				m.progressText = ""
				m.step = stepSelectMode
			}
		case stepDumpOutputPath:
			return m.updateDumpOutputPath(msg)
		case stepDumpConfirm:
			return m.updateDumpConfirm(msg)
		case stepDumpResult:
			switch msg.String() {
			case "q", "ctrl+c", "esc":
				m.step = stepDone
				return m, tea.Quit
			default:
				m.dumpErr = nil
				m.dumpProgressPct = 0
				m.dumpProgressText = ""
				m.step = stepSelectMode
			}
		case stepMigrateSelectDest:
			return m.updateMigrateDestSelector(msg)
		case stepMigrateConfirm:
			return m.updateMigrateConfirm(msg)
		case stepMigrateResult:
			switch msg.String() {
			case "q", "ctrl+c", "esc":
				m.step = stepDone
				return m, tea.Quit
			default:
				m.migrateErr = nil
				m.migrateProgressPct = 0
				m.migrateProgressText = ""
				m.step = stepSelectMode
			}
		}
		// Propagate input updates for active textinput in dump output path
		if m.step == stepDumpOutputPath {
			var cmd tea.Cmd
			m.dumpOutputInput, cmd = m.dumpOutputInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m Model) updateProfileSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.profiles) == 0 {
		switch msg.String() {
		case "a", "A":
			m.startProfileForm(nil, false)
			m.step = stepEditProfileForm
			return m, nil
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.profileIdx > 0 {
			m.profileIdx--
		}
	case "down", "j":
		if m.profileIdx < len(m.profiles)-1 {
			m.profileIdx++
		}
	case "enter", " ":
		m.result.Profile = m.profiles[m.profileIdx]
		if m.result.Mode == ModeDump {
			m.dumpOutputInput.SetValue("")
			m.dumpOutputInput.Focus()
			m.step = stepDumpOutputPath
		} else if m.result.Mode == ModeMigrate {
			m.migrateDestIdx = 0
			if m.migrateDestIdx == m.profileIdx && len(m.profiles) > 1 {
				m.migrateDestIdx = 1
			}
			m.step = stepMigrateSelectDest
		} else {
			m.step = stepSelectFile
		}
	case "a", "A":
		m.startProfileForm(nil, false)
		m.step = stepEditProfileForm
	case "e", "E":
		selected := m.profiles[m.profileIdx]
		m.startProfileForm(&selected, true)
		m.step = stepEditProfileForm
	case "d", "D", "delete":
		m.step = stepConfirmDelete
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.step = stepSelectMode
	}
	return m, nil
}

func (m Model) updateFileBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.searching {
		switch key {
		case "esc":
			m.searching = false
			m.searchInput.Blur()
			m.searchInput.SetValue("")
			m.fileIdx = 0
			return m, nil
		case "ctrl+c":
			m.searching = false
			m.searchInput.Blur()
			m.searchInput.SetValue("")
			m.step = stepSelectProfile
			return m, nil
		case "up", "k", "down", "j", "enter":
			// fall through to shared navigation/select block below
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.fileIdx = clampIndex(0, len(m.visibleEntries()))
			return m, cmd
		}
	} else if key == "/" {
		m.searching = true
		m.searchInput.Focus()
		m.searchInput.SetValue("")
		m.fileIdx = 0
		return m, nil
	}

	vis := m.visibleEntries()
	m.fileIdx = clampIndex(m.fileIdx, len(vis))
	switch key {
	case "up", "k":
		if m.fileIdx > 0 {
			m.fileIdx--
		}
	case "down", "j":
		if m.fileIdx < len(vis)-1 {
			m.fileIdx++
		}
	case "enter", " ":
		if len(vis) == 0 {
			return m, nil
		}
		selected := vis[m.fileIdx]
		if selected.IsDir {
			newDir := filepath.Join(m.currentDir, selected.Name)
			if selected.Name == ".." {
				newDir = filepath.Dir(m.currentDir)
			}
			m.currentDir = newDir
			m.entries = listDir(newDir)
			m.fileIdx = 0
			m.searching = false
			m.searchInput.Blur()
			m.searchInput.SetValue("")
		} else {
			m.result.File = selected.Path
			m.step = stepConfirm
		}
	case "backspace", "h", "left":
		parent := filepath.Dir(m.currentDir)
		if parent != m.currentDir {
			m.currentDir = parent
			m.entries = listDir(parent)
			m.fileIdx = 0
			m.searching = false
			m.searchInput.Blur()
			m.searchInput.SetValue("")
		}
	case "q", "ctrl+c", "esc":
		m.step = stepSelectProfile
	}
	return m, nil
}

// visibleEntries returns the file list the user actually sees: the full list
// when not searching, or a case-insensitive substring filter of names otherwise.
func (m Model) visibleEntries() []fileEntry {
	if !m.searching || m.searchInput.Value() == "" {
		return m.entries
	}
	q := strings.ToLower(m.searchInput.Value())
	var filtered []fileEntry
	for _, e := range m.entries {
		if strings.Contains(strings.ToLower(e.Name), q) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle filter input mode
	if m.filterInputMode {
		switch msg.String() {
		case "enter":
			m.saveFilterInput()
			return m, nil
		case "esc":
			m.cancelFilterInput()
			return m, nil
		default:
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			return m, cmd
		}
	}

	switch msg.String() {
	case "enter", "y":
		m.result.Confirm = true
		// 1. Get driver
		drv, err := driver.Get(m.result.Profile.Driver)
		if err != nil {
			m.restoreErr = err
			m.step = stepRestoreResult
			return m, nil
		}

		// 2. Detect format
		detectedFormat, err := drv.DetectFormat(m.result.File)
		if err != nil {
			m.restoreErr = err
			m.step = stepRestoreResult
			return m, nil
		}

		finalFormat := driver.Format(m.result.Settings.Format)
		if finalFormat == "" || finalFormat == "auto" {
			finalFormat = detectedFormat
		}

		opts := driver.RestoreOptions{
			Profile:         m.result.Profile,
			FilePath:        m.result.File,
			Format:          finalFormat,
			Jobs:            m.result.Settings.Jobs,
			Clean:           m.result.Settings.Clean,
			IncludeTable:    m.result.Settings.IncludeTable,
			ExcludeTable:    m.result.Settings.ExcludeTable,
			IncludeSchema:   m.result.Settings.IncludeSchema,
			ExcludeSchema:   m.result.Settings.ExcludeSchema,
			DryRun:          m.result.Settings.DryRun,
			CreateIfMissing: m.result.Settings.CreateIfMissing,
		}

		// 3. Check Dry Run
		if opts.DryRun {
			m.dryRunOutput = getDryRunCommand(opts)
			m.step = stepRestoreResult
			return m, nil
		}

		// 4. Ensure DB exists if requested
		if opts.CreateIfMissing {
			m.progressText = "Ensuring target database exists..."
			_ = drv.EnsureDatabaseExists(context.Background(), m.result.Profile)
		}

		// 5. Start Restore Asynchronously
		m.step = stepRestoring
		m.progressPct = 0
		m.progressText = "Starting restore..."
		m.restoreErr = nil

		ch, err := drv.Restore(context.Background(), opts)
		if err != nil {
			m.restoreErr = err
			m.step = stepRestoreResult
			return m, nil
		}
		m.progressChan = ch

		return m, listenToProgress(ch)

	case "n", "q", "ctrl+c", "esc":
		m.step = stepSelectFile
	case "c", "C":
		m.result.Settings.Clean = !m.result.Settings.Clean
	case "m", "M":
		m.result.Settings.CreateIfMissing = !m.result.Settings.CreateIfMissing
	case "o", "O":
		m.result.Settings.Optimize = !m.result.Settings.Optimize
	case "+", "=":
		m.result.Settings.Jobs++
	case "-":
		if m.result.Settings.Jobs > 1 {
			m.result.Settings.Jobs--
		}
	case "t":
		m.openFilterInput("include-table")
		return m, m.filterInput.Focus()
	case "T":
		m.openFilterInput("exclude-table")
		return m, m.filterInput.Focus()
	case "h":
		m.openFilterInput("include-schema")
		return m, m.filterInput.Focus()
	case "H":
		m.openFilterInput("exclude-schema")
		return m, m.filterInput.Focus()
	}
	return m, nil
}

func getDryRunCommand(opts driver.RestoreOptions) string {
	if opts.Format == driver.FormatPlain {
		return fmt.Sprintf("psql -h %s -p %d -U %s -d %s -f %s",
			opts.Profile.Host, opts.Profile.Port, opts.Profile.User, opts.Profile.Database, opts.FilePath)
	}
	cleanFlag := ""
	if opts.Clean {
		cleanFlag = " --clean --if-exists"
	}
	jobsFlag := ""
	if opts.Format == driver.FormatDirectory && opts.Jobs > 1 {
		jobsFlag = fmt.Sprintf(" -j %d", opts.Jobs)
	}
	filterFlags := ""
	for _, t := range opts.IncludeTable {
		filterFlags += " -t " + t
	}
	for _, t := range opts.ExcludeTable {
		filterFlags += " -T " + t
	}
	for _, s := range opts.IncludeSchema {
		filterFlags += " -n " + s
	}
	for _, s := range opts.ExcludeSchema {
		filterFlags += " -N " + s
	}
	return fmt.Sprintf("pg_restore -h %s -p %d -U %s -d %s%s%s%s -v %s",
		opts.Profile.Host, opts.Profile.Port, opts.Profile.User, opts.Profile.Database, jobsFlag, cleanFlag, filterFlags, opts.FilePath)
}

// --- View ---
func (m Model) View() string {
	if m.step == stepDone {
		return ""
	}
	// Show filter input overlay if active
	if m.filterInputMode {
		return m.viewFilterInput()
	}
	switch m.step {
	case stepSelectMode:
		return m.viewSelectMode()
	case stepSelectProfile:
		return m.viewProfileSelector()
	case stepSelectFile:
		return m.viewFileBrowser()
	case stepConfirm:
		return m.viewConfirm()
	case stepConfirmDelete:
		return m.viewConfirmDelete()
	case stepEditProfileForm:
		return m.viewEditProfileForm()
	case stepRestoring:
		return m.viewRestoring()
	case stepRestoreResult:
		return m.viewRestoreResult()
	case stepDumpOutputPath:
		return m.viewDumpOutputPath()
	case stepDumpConfirm:
		return m.viewDumpConfirm()
	case stepDumping:
		return m.viewDumping()
	case stepDumpResult:
		return m.viewDumpResult()
	case stepMigrateSelectDest:
		return m.viewMigrateDestSelector()
	case stepMigrateConfirm:
		return m.viewMigrateConfirm()
	case stepMigrating:
		return m.viewMigrating()
	case stepMigrateResult:
		return m.viewMigrateResult()
	}
	return ""
}

// ─── Filter Input Helpers ────────────────────────────────────────────────────

func (m *Model) openFilterInput(filterType string) {
	m.filterInputMode = true
	m.filterInputType = filterType

	var currentValues []string
	switch filterType {
	case "include-table":
		currentValues = m.getIncludeTable()
	case "exclude-table":
		currentValues = m.getExcludeTable()
	case "include-schema":
		currentValues = m.getIncludeSchema()
	case "exclude-schema":
		currentValues = m.getExcludeSchema()
	}

	m.filterInput.SetValue(strings.Join(currentValues, ", "))
	m.filterInput.Focus()
}

func (m *Model) saveFilterInput() {
	values := parseFilterValues(m.filterInput.Value())

	switch m.filterInputType {
	case "include-table":
		m.setIncludeTable(values)
	case "exclude-table":
		m.setExcludeTable(values)
	case "include-schema":
		m.setIncludeSchema(values)
	case "exclude-schema":
		m.setExcludeSchema(values)
	}

	m.filterInputMode = false
}

func (m *Model) cancelFilterInput() {
	m.filterInputMode = false
}

// Getters/Setters for filter values based on current step
func (m *Model) getIncludeTable() []string {
	switch m.step {
	case stepDumpConfirm:
		return m.result.DumpSettings.IncludeTable
	case stepMigrateConfirm:
		return m.result.MigrateSettings.IncludeTable
	default:
		return m.result.Settings.IncludeTable
	}
}

func (m *Model) getExcludeTable() []string {
	switch m.step {
	case stepDumpConfirm:
		return m.result.DumpSettings.ExcludeTable
	case stepMigrateConfirm:
		return m.result.MigrateSettings.ExcludeTable
	default:
		return m.result.Settings.ExcludeTable
	}
}

func (m *Model) getIncludeSchema() []string {
	switch m.step {
	case stepDumpConfirm:
		return m.result.DumpSettings.IncludeSchema
	case stepMigrateConfirm:
		return m.result.MigrateSettings.IncludeSchema
	default:
		return m.result.Settings.IncludeSchema
	}
}

func (m *Model) getExcludeSchema() []string {
	switch m.step {
	case stepDumpConfirm:
		return m.result.DumpSettings.ExcludeSchema
	case stepMigrateConfirm:
		return m.result.MigrateSettings.ExcludeSchema
	default:
		return m.result.Settings.ExcludeSchema
	}
}

func (m *Model) setIncludeTable(values []string) {
	switch m.step {
	case stepDumpConfirm:
		m.result.DumpSettings.IncludeTable = values
	case stepMigrateConfirm:
		m.result.MigrateSettings.IncludeTable = values
	default:
		m.result.Settings.IncludeTable = values
	}
}

func (m *Model) setExcludeTable(values []string) {
	switch m.step {
	case stepDumpConfirm:
		m.result.DumpSettings.ExcludeTable = values
	case stepMigrateConfirm:
		m.result.MigrateSettings.ExcludeTable = values
	default:
		m.result.Settings.ExcludeTable = values
	}
}

func (m *Model) setIncludeSchema(values []string) {
	switch m.step {
	case stepDumpConfirm:
		m.result.DumpSettings.IncludeSchema = values
	case stepMigrateConfirm:
		m.result.MigrateSettings.IncludeSchema = values
	default:
		m.result.Settings.IncludeSchema = values
	}
}

func (m *Model) setExcludeSchema(values []string) {
	switch m.step {
	case stepDumpConfirm:
		m.result.DumpSettings.ExcludeSchema = values
	case stepMigrateConfirm:
		m.result.MigrateSettings.ExcludeSchema = values
	default:
		m.result.Settings.ExcludeSchema = values
	}
}

// parseFilterValues splits comma-separated input into trimmed values
func parseFilterValues(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// viewFilterInput renders the filter input overlay
func (m Model) viewFilterInput() string {
	var sb strings.Builder

	var title string
	switch m.filterInputType {
	case "include-table":
		title = "Include Tables"
	case "exclude-table":
		title = "Exclude Tables"
	case "include-schema":
		title = "Include Schemas"
	case "exclude-schema":
		title = "Exclude Schemas"
	}

	header := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — " + title)
	sb.WriteString(header + "\n\n")

	sb.WriteString("  Enter values separated by commas:\n\n")
	sb.WriteString("  " + m.filterInput.View() + "\n\n")

	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
		"  [Enter] save   [esc] cancel\n"))

	return sb.String()
}

// ─── Render Helpers ──────────────────────────────────────────────────────────

// renderRow renders a single list row.
// selected rows: bold accent foreground + "▶" prefix
// normal rows: muted foreground + " " prefix
func renderRow(selected bool, content string) string {
	if selected {
		prefix := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▶ ")
		text := lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(content)
		return prefix + text
	}
	prefix := lipgloss.NewStyle().Foreground(colorMuted).Render("  ")
	text := lipgloss.NewStyle().Foreground(colorSubtext).Render(content)
	return prefix + text
}

func renderBadge(label, bg string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color(bg)).
		Bold(true).
		Padding(0, 1).
		Render(label)
}

func (m Model) viewProfileSelector() string {
	var body strings.Builder

	body.WriteString(renderSectionTitle(profileSelectorPrompt(m.result.Mode)))
	body.WriteString("\n\n")

	if len(m.profiles) == 0 {
		body.WriteString(renderCard(m.width, "No profiles found", warningStyle.Render("Press A to add a new connection profile.")))
	} else {
		body.WriteString(renderProfileRows(m.width, m.profiles, m.profileIdx, ""))
	}

	return renderScreenFrame(m.width, "Select Connection Profile", profileSelectorSubtitle(m.result.Mode), body.String(), []keyHint{
		{Key: "↑/↓", Label: "navigate"},
		{Key: "Enter", Label: "select"},
		{Key: "A", Label: "add"},
		{Key: "E", Label: "edit"},
		{Key: "D", Label: "delete"},
		{Key: "Esc", Label: "back", Danger: true},
		{Key: "Q", Label: "quit", Danger: true},
	})
}

func profileSelectorPrompt(mode Mode) string {
	switch mode {
	case ModeDump:
		return "Choose a source profile to dump"
	case ModeMigrate:
		return "Choose a source profile to migrate"
	default:
		return "Choose a target profile to restore into"
	}
}

func profileSelectorSubtitle(mode Mode) string {
	switch mode {
	case ModeDump:
		return "Source database for backup export"
	case ModeMigrate:
		return "Source database for profile-to-profile copy"
	default:
		return "Target database for dump restore"
	}
}

func (m Model) viewFileBrowser() string {
	var body strings.Builder

	p := m.result.Profile
	body.WriteString(renderProfileSummaryCard(m.width, "Target Profile", p.Name, p.Driver, p.Host, p.Port, p.Database, p.User))
	body.WriteString("\n\n")

	details := renderKeyValueGrid([]kvRow{{Label: "Path", Value: truncateMiddle(m.currentDir, safePanelWidth(m.width)-14)}}, 10)
	if m.searching {
		details += "\n" + renderKeyValueGrid([]kvRow{{Label: "Filter", Value: m.searchInput.View()}}, 10)
	}
	body.WriteString(renderCard(m.width, "Browse Dump Files", details))
	body.WriteString("\n\n")

	vis := m.visibleEntries()
	if len(vis) == 0 {
		message := "Directory is empty"
		if m.searching && m.searchInput.Value() != "" {
			message = fmt.Sprintf("No files match %q", m.searchInput.Value())
		}
		body.WriteString(renderCard(m.width, "No Results", mutedStyle.Render(message)))
	} else {
		var listBody strings.Builder
		headerLines := 14
		if m.searching {
			headerLines = 15
		}
		visible := m.height - headerLines
		if visible < 3 {
			visible = 3
		}
		start := 0
		if m.fileIdx >= visible {
			start = m.fileIdx - visible + 1
		}
		end := start + visible
		if end > len(vis) {
			end = len(vis)
		}

		for i := start; i < end; i++ {
			if i > start {
				listBody.WriteString("\n")
			}
			listBody.WriteString(renderFileEntryRow(m.width, vis[i], i == m.fileIdx))
		}
		body.WriteString(renderCard(m.width, "Directory Entries", listBody.String()))

		if len(vis) > visible {
			body.WriteString("\n\n")
			body.WriteString(mutedStyle.Render(fmt.Sprintf("Showing %d-%d of %d items", start+1, end, len(vis))))
		}
	}

	hints := []keyHint{{Key: "↑/↓", Label: "navigate"}, {Key: "/", Label: "filter"}, {Key: "Enter", Label: "open/select"}, {Key: "⌫/←", Label: "parent"}, {Key: "Esc", Label: "back", Danger: true}}
	if m.searching {
		hints = []keyHint{{Key: "↑/↓", Label: "navigate"}, {Key: "Enter", Label: "select"}, {Key: "Esc", Label: "clear filter", Danger: true}}
	}

	return renderScreenFrame(m.width, "Select Dump File", "Choose a dump artifact for restore", body.String(), hints)
}

func (m Model) viewConfirm() string {
	var body strings.Builder
	p := m.result.Profile
	s := m.result.Settings

	body.WriteString(renderProfileSummaryCard(m.width, "Target Profile", p.Name, p.Driver, p.Host, p.Port, p.Database, p.User))
	body.WriteString("\n")
	body.WriteString(renderCard(m.width, "Dump File", renderKeyValueGrid([]kvRow{
		{Label: "Path", Value: truncateMiddle(m.result.File, safePanelWidth(m.width)-14)},
		{Label: "Format", Value: s.Format, Kind: badgePrimary},
		{Label: "Jobs", Value: fmt.Sprintf("%d parallel processes", s.Jobs)},
	}, 10)))
	body.WriteString("\n")

	cleanValue := "No"
	cleanKind := badgeNeutral
	if s.Clean {
		cleanValue = "CLEAN"
		cleanKind = badgeDanger
	}
	createValue := "No"
	createKind := badgeNeutral
	if s.CreateIfMissing {
		createValue = "CREATE"
		createKind = badgeSuccess
	}
	optimizeValue := "No"
	optimizeKind := badgeNeutral
	if s.Optimize {
		optimizeValue = "OPTIMIZE"
		optimizeKind = badgeSuccess
	}
	options := renderKeyValueGrid([]kvRow{
		{Label: "Clean", Value: cleanValue, Kind: cleanKind},
		{Label: "Create DB", Value: createValue, Kind: createKind},
		{Label: "Optimize", Value: optimizeValue, Kind: optimizeKind},
	}, 12)
	options += "\n" + renderFilterRows(s.IncludeSchema, s.ExcludeSchema, s.IncludeTable, s.ExcludeTable)
	body.WriteString(renderCard(m.width, "Restore Options", options))

	if s.Clean {
		body.WriteString("\n")
		body.WriteString(renderCard(m.width, "Safety Review", renderDanger("Clean mode may drop database objects before restore.")))
	}

	return renderScreenFrame(m.width, "Confirm Restore", "Pre-flight review before database restore", body.String(), []keyHint{
		{Key: "C", Label: "clean"},
		{Key: "M", Label: "create-db"},
		{Key: "O", Label: "optimize"},
		{Key: "+/-", Label: "jobs"},
		{Key: "T/H", Label: "filters"},
		{Key: "Enter", Label: "proceed"},
		{Key: "Esc", Label: "back", Danger: true},
	})
}

// --- Helpers ---
func listDir(dir string) []fileEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var result []fileEntry

	// Add ".." to navigate to parent
	if filepath.Dir(dir) != dir {
		result = append(result, fileEntry{Name: "..", Path: filepath.Dir(dir), IsDir: true})
	}

	var dirs, files []fileEntry
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		fe := fileEntry{
			Name:  e.Name(),
			Path:  filepath.Join(dir, e.Name()),
			IsDir: e.IsDir(),
			Size:  info.Size(),
		}
		if e.IsDir() {
			dirs = append(dirs, fe)
		} else {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext == ".tar" || ext == ".sql" || ext == ".dump" || ext == ".gz" || ext == ".bak" || ext == "" {
				files = append(files, fe)
			}
		}
	}

	result = append(result, dirs...)
	result = append(result, files...)
	return result
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// --- Profile Management Logic ---

func (m Model) initFormFields(p *config.Profile) []textinput.Model {
	inputs := make([]textinput.Model, 7)
	for i := range inputs {
		t := textinput.New()
		t.Cursor.Style = lipgloss.NewStyle().Foreground(colorAccent)
		t.CharLimit = 64
		inputs[i] = t
	}

	inputs[0].Placeholder = "Profile Name (e.g., local-dev)"
	inputs[0].Focus()

	inputs[1].Placeholder = "Driver (postgres or mysql)"
	inputs[2].Placeholder = "Host (e.g., localhost)"
	inputs[3].Placeholder = "Port (e.g., 5432)"
	inputs[4].Placeholder = "User (e.g., postgres)"
	inputs[5].Placeholder = "Password"
	inputs[5].EchoMode = textinput.EchoPassword
	inputs[5].EchoCharacter = '*'
	inputs[6].Placeholder = "Database Name"

	if p != nil {
		inputs[0].SetValue(p.Name)
		inputs[1].SetValue(p.Driver)
		inputs[2].SetValue(p.Host)
		inputs[3].SetValue(strconv.Itoa(p.Port))
		inputs[4].SetValue(p.User)
		inputs[5].SetValue(p.Password)
		inputs[6].SetValue(p.Database)
	} else {
		// Defaults
		inputs[1].SetValue("postgres")
		inputs[2].SetValue("localhost")
		inputs[3].SetValue("5432")
		inputs[4].SetValue("postgres")
	}

	return inputs
}

func (m *Model) startProfileForm(p *config.Profile, isEditing bool) {
	m.inputs = m.initFormFields(p)
	m.focusedIdx = 0
	m.isEditing = isEditing
	m.formErr = ""
	m.formInfo = defaultManualRuntimeMessage()
	m.formNotice = ""
	m.formRuntime = nil
	m.formRuntimeDetails = false
	m.runtimeDetecting = false
	m.runtimeDetectPct = 0
	m.runtimeDetectText = ""
	m.origName = ""
	if p != nil {
		m.formRuntime = cloneRuntimeProfile(p.Runtime)
		if p.Runtime != nil {
			m.formInfo = formatRuntimeSummary(p.Runtime)
			m.formRuntimeDetails = true
		}
		if isEditing {
			m.origName = p.Name
		}
	}
}

func (m Model) updateEditProfileForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "esc":
		m.step = stepSelectProfile
		return m, nil
	case "f2", "ctrl+r":
		if m.runtimeDetecting {
			return m, nil
		}
		return m.detectProfileRuntime()
	}

	if m.runtimeDetecting {
		return m, nil
	}

	switch keyMsg.String() {
	case "f3":
		if m.formRuntime != nil {
			m.formRuntimeDetails = !m.formRuntimeDetails
		}
		return m, nil
	case "f4":
		m.formRuntime = nil
		m.formInfo = defaultManualRuntimeMessage()
		m.formNotice = "Runtime metadata cleared. Connection fields stay unchanged."
		m.formRuntimeDetails = false
		return m, nil

	case "tab", "shift+tab", "up", "down", "enter":
		s := keyMsg.String()

		// Save form if Enter is pressed on the last field or any field when valid
		if s == "enter" && m.focusedIdx == len(m.inputs)-1 {
			return m.saveProfileForm()
		}

		// Navigate fields
		if s == "up" || s == "shift+tab" {
			m.focusedIdx--
		} else {
			m.focusedIdx++
		}

		if m.focusedIdx < 0 {
			m.focusedIdx = len(m.inputs) - 1
		} else if m.focusedIdx >= len(m.inputs) {
			m.focusedIdx = 0
		}

		for i := 0; i < len(m.inputs); i++ {
			if i == m.focusedIdx {
				m.inputs[i].Focus()
			} else {
				m.inputs[i].Blur()
			}
		}

		return m, nil
	}

	// Update the focused text input
	var cmd tea.Cmd
	m.inputs[m.focusedIdx], cmd = m.inputs[m.focusedIdx].Update(msg)
	return m, cmd
}

func (m Model) saveProfileForm() (tea.Model, tea.Cmd) {
	// Validate
	name := strings.TrimSpace(m.inputs[0].Value())
	driver := strings.TrimSpace(m.inputs[1].Value())
	host := strings.TrimSpace(m.inputs[2].Value())
	portStr := strings.TrimSpace(m.inputs[3].Value())
	user := strings.TrimSpace(m.inputs[4].Value())
	pass := m.inputs[5].Value()
	dbName := strings.TrimSpace(m.inputs[6].Value())

	if name == "" {
		m.formErr = "Profile name cannot be empty"
		return m, nil
	}
	if driver != "postgres" && driver != "mysql" {
		m.formErr = "Driver must be 'postgres' or 'mysql'"
		return m, nil
	}
	portInt, err := strconv.Atoi(portStr)
	if err != nil || portInt <= 0 {
		m.formErr = "Port must be a valid positive integer"
		return m, nil
	}
	if host == "" {
		m.formErr = "Host cannot be empty"
		return m, nil
	}
	if user == "" {
		m.formErr = "User cannot be empty"
		return m, nil
	}
	if dbName == "" {
		m.formErr = "Database cannot be empty"
		return m, nil
	}

	if _, exists := m.cfg.Profiles[name]; exists && (!m.isEditing || name != m.origName) {
		m.formErr = fmt.Sprintf("Profile %q already exists", name)
		return m, nil
	}

	if m.cfg.Profiles == nil {
		m.cfg.Profiles = make(map[string]config.Profile)
	}

	// Preserve metadata that is managed by profile import/runtime detection and
	// is not exposed in the text fields.
	runtime := cloneRuntimeProfile(m.formRuntime)
	if runtime == nil && m.isEditing {
		if existing, ok := m.cfg.Profiles[m.origName]; ok {
			runtime = cloneRuntimeProfile(existing.Runtime)
		}
	}
	wasActiveProfile := m.isEditing && m.result.Profile.Name == m.origName

	// Update Config
	if m.isEditing && name != m.origName {
		delete(m.cfg.Profiles, m.origName)
	}

	m.cfg.Profiles[name] = config.Profile{
		Driver:   driver,
		Host:     host,
		Port:     portInt,
		User:     user,
		Password: pass,
		Database: dbName,
		Runtime:  runtime,
	}

	if err := config.SaveConfig(m.cfg); err != nil {
		m.formErr = fmt.Sprintf("Failed to save config: %v", err)
		return m, nil
	}

	// Reload profiles list
	if wasActiveProfile {
		m.result.Profile.Name = name
	}
	m.reloadProfiles()

	// Return to profile selection
	m.step = stepSelectProfile
	return m, nil
}

func (m Model) updateConfirmDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "y", "Y", "enter":
		selected := m.profiles[m.profileIdx]
		delete(m.cfg.Profiles, selected.Name)
		_ = config.SaveConfig(m.cfg)

		m.reloadProfiles()
		if m.profileIdx >= len(m.profiles) && m.profileIdx > 0 {
			m.profileIdx = len(m.profiles) - 1
		}
		m.step = stepSelectProfile
	case "n", "N", "esc":
		m.step = stepSelectProfile
	}
	return m, nil
}

func (m *Model) reloadProfiles() {
	m.profiles = profilesFromConfig(m.cfg)

	if m.result.Profile.Name != "" {
		if p, ok := m.cfg.Profiles[m.result.Profile.Name]; ok {
			p.Name = m.result.Profile.Name
			m.result.Profile = p
		} else {
			m.result.Profile = config.Profile{}
		}
	}
}

func profilesFromConfig(cfg *config.Config) []config.Profile {
	if cfg == nil {
		return nil
	}

	profiles := make([]config.Profile, 0, len(cfg.Profiles))
	for name, p := range cfg.Profiles {
		p.Name = name
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})
	return profiles
}

func (m Model) viewEditProfileForm() string {
	var body strings.Builder

	title := "Add Connection Profile"
	subtitle := "Manual connection setup"
	if m.isEditing {
		title = fmt.Sprintf("Edit Profile: %s", m.origName)
		subtitle = "Update connection settings and runtime metadata"
	}

	labels := []string{
		"Profile Name:",
		"DB Driver:",
		"Host:",
		"Port:",
		"User:",
		"Password:",
		"Database:",
	}

	formContent := ""
	for i := range m.inputs {
		label := lipgloss.NewStyle().Foreground(colorMuted).Width(14).Render(labels[i])
		formContent += "  " + label + " " + m.inputs[i].View() + "\n"
	}
	body.WriteString(renderCard(m.width, "Connection Fields", strings.TrimRight(formContent, "\n")))
	body.WriteString("\n")
	body.WriteString(m.renderRuntimeStatusCard())
	if m.formRuntime != nil && m.formRuntimeDetails {
		body.WriteString("\n")
		body.WriteString(m.renderRuntimeDetailsCard())
	}
	if m.formErr != "" {
		body.WriteString("\n")
		body.WriteString(renderCard(m.width, "Validation Error", errorStyle.Render(m.formErr)))
	}

	return renderScreenFrame(m.width, title, subtitle, body.String(), m.editProfileFormHints())
}

func (m Model) renderRuntimeStatusCard() string {
	if m.runtimeDetecting {
		rows := []kvRow{{Label: "State", Value: "DETECTING", Kind: badgePrimary}}
		body := renderKeyValueGrid(rows, 0)
		body += "\n" + renderProgressBar(progressWidth(m.width), m.runtimeDetectPct)
		if m.runtimeDetectText != "" {
			body += "\n" + subtextStyle.Render(truncateMiddle(m.runtimeDetectText, safePanelWidth(m.width)-4))
		}
		return renderCard(m.width, "Runtime Detection", body)
	}

	stateLabel, stateKind := runtimeStateBadge(m.formRuntime, m.formNotice)
	sourceLabel, sourceKind := runtimeSourceBadge(m.formRuntime)
	rows := []kvRow{{Label: "State", Value: stateLabel, Kind: stateKind}}
	if sourceLabel != "" {
		rows = append(rows, kvRow{Label: "Source", Value: sourceLabel, Kind: sourceKind})
	}
	body := renderKeyValueGrid(rows, 0)
	message := m.formInfo
	if m.formNotice != "" {
		message = m.formNotice
	}
	if message != "" {
		body += "\n" + subtextStyle.Render(truncateMiddle(message, safePanelWidth(m.width)-4))
	}
	return renderCard(m.width, "Runtime Detection", body)
}

func (m Model) renderRuntimeDetailsCard() string {
	rows := runtimeDetailRows(m.formRuntime)
	body := renderKeyValueGrid(rows, 13)
	return renderCard(m.width, "Docker Runtime Details", body)
}

func (m Model) editProfileFormHints() []keyHint {
	detailsLabel := "show details"
	clearLabel := "clear runtime"
	if m.formRuntimeDetails {
		detailsLabel = "hide details"
	}
	if m.formRuntime == nil {
		clearLabel = "clear runtime"
	}
	return []keyHint{
		{Key: "Tab/Shift+Tab", Label: "navigate"},
		{Key: "F2/Ctrl+R", Label: "detect runtime"},
		{Key: "F3", Label: detailsLabel},
		{Key: "F4", Label: clearLabel},
		{Key: "Enter", Label: "save on last field"},
		{Key: "Esc", Label: "cancel", Danger: true},
	}
}

func runtimeStateBadge(runtimeProfile *config.RuntimeProfile, notice string) (string, badgeKind) {
	if runtimeProfile != nil {
		return "DOCKER DETECTED", badgeSuccess
	}
	if notice != "" {
		return "NO MATCH", badgeWarning
	}
	return "MANUAL", badgeNeutral
}

func runtimeSourceBadge(runtimeProfile *config.RuntimeProfile) (string, badgeKind) {
	if runtimeProfile == nil {
		return "", badgeNeutral
	}
	switch runtimeProfile.Source {
	case "docker-cli":
		return "LIVE CONTAINER", badgeSuccess
	case "docker-compose":
		return "COMPOSE FALLBACK", badgeWarning
	default:
		return strings.ToUpper(runtimeProfile.Source), badgePrimary
	}
}

func runtimeDetailRows(runtimeProfile *config.RuntimeProfile) []kvRow {
	rows := []kvRow{}
	if runtimeProfile == nil {
		return rows
	}
	rows = append(rows, kvRow{Label: "Runtime", Value: strings.ToUpper(runtimeProfile.Type), Kind: badgePrimary})
	if runtimeProfile.Source != "" {
		rows = append(rows, kvRow{Label: "Source", Value: runtimeProfile.Source})
	}
	if runtimeProfile.ServiceName != "" {
		rows = append(rows, kvRow{Label: "Service", Value: runtimeProfile.ServiceName})
	}
	if runtimeProfile.Container != "" {
		rows = append(rows, kvRow{Label: "Container", Value: runtimeProfile.Container})
	}
	if runtimeProfile.SourceFile != "" {
		rows = append(rows, kvRow{Label: "Compose File", Value: filepath.Base(runtimeProfile.SourceFile)})
	}
	if len(runtimeProfile.Mounts) > 0 {
		rows = append(rows, kvRow{Label: "Mounts", Value: fmt.Sprintf("%d", len(runtimeProfile.Mounts))})
	}
	if runtimeProfile.Paths.DataDirectory != "" {
		rows = append(rows, kvRow{Label: "Data Dir", Value: runtimeProfile.Paths.DataDirectory})
	}
	if runtimeProfile.Paths.HostHBAFile != "" {
		rows = append(rows, kvRow{Label: "Host HBA", Value: runtimeProfile.Paths.HostHBAFile})
	}
	return rows
}

func (m Model) viewConfirmDelete() string {
	var sb strings.Builder

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Delete Profile Confirmation")
	sb.WriteString(title + "\n\n")

	selected := m.profiles[m.profileIdx]
	sb.WriteString("  " + warningStyle.Render("Are you sure you want to delete this profile?") + "\n\n")
	sb.WriteString("  " + labelStyle.Render("Profile:") + " " + valueStyle.Render(selected.Name) + "\n")
	sb.WriteString("  " + labelStyle.Render("Host:") + " " + valueStyle.Render(fmt.Sprintf("%s:%d", selected.Host, selected.Port)) + "\n")
	sb.WriteString("  " + labelStyle.Render("Database:") + " " + valueStyle.Render(selected.Database) + "\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorMuted).Render("This only removes the local dbtool profile; it does not delete the database.") + "\n\n")

	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
		"  [y/Enter] yes, delete   [n/esc] no, keep it\n"))
	return sb.String()
}

func (m *Model) finalizeRestore(err error) {
	m.restoreErr = err

	drv, _ := driver.Get(m.result.Profile.Driver)
	detectedFormat, _ := drv.DetectFormat(m.result.File)
	finalFormat := driver.Format(m.result.Settings.Format)
	if finalFormat == "" || finalFormat == "auto" {
		finalFormat = detectedFormat
	}
	opts := driver.RestoreOptions{
		Profile:         m.result.Profile,
		FilePath:        m.result.File,
		Format:          finalFormat,
		Jobs:            m.result.Settings.Jobs,
		Clean:           m.result.Settings.Clean,
		IncludeTable:    m.result.Settings.IncludeTable,
		ExcludeTable:    m.result.Settings.ExcludeTable,
		IncludeSchema:   m.result.Settings.IncludeSchema,
		ExcludeSchema:   m.result.Settings.ExcludeSchema,
		DryRun:          m.result.Settings.DryRun,
		CreateIfMissing: m.result.Settings.CreateIfMissing,
	}
	cmdString := getDryRunCommand(opts)

	historyRec := history.HistoryRecord{
		File:    m.result.File,
		Profile: m.result.Profile.Name,
		Time:    time.Now(),
		Success: err == nil,
		Command: cmdString,
	}
	if err != nil {
		historyRec.Error = err.Error()
	}
	_ = history.AppendHistory(historyRec)

	if err != nil {
		_ = beeep.Notify("DBTool Restore Failed", fmt.Sprintf("Profile: %s\nError: %v", m.result.Profile.Name, err), "")
	} else {
		_ = beeep.Notify("DBTool Restore Success", fmt.Sprintf("Database %s restored successfully", m.result.Profile.Database), "")

		// Post-restore optimization
		if m.result.Settings.Optimize {
			optDrv, _ := driver.Get(m.result.Profile.Driver)
			if optErr := optDrv.Optimize(context.Background(), m.result.Profile); optErr != nil {
				_ = beeep.Notify("DBTool Optimization Warning", fmt.Sprintf("Restore succeeded but optimization failed: %v", optErr), "")
			}
		}
	}
}

func (m Model) viewRestoring() string {
	var sb strings.Builder

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Restoring Database")
	sb.WriteString(title + "\n\n")

	sb.WriteString("  Profile: " + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(m.result.Profile.Name) + "\n")
	sb.WriteString("  File:    " + lipgloss.NewStyle().Foreground(colorSubtext).Render(m.result.File) + "\n\n")

	barWidth := progressWidth(m.width)
	sb.WriteString(fmt.Sprintf("  [%s] %.0f%%\n\n", renderProgressBar(barWidth, m.progressPct), clampPercent(m.progressPct)))
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSubtext).Render(m.progressText) + "\n")

	return sb.String()
}

func (m Model) viewRestoreResult() string {
	var body strings.Builder

	if m.result.Settings.DryRun {
		body.WriteString(renderCard(m.width, "✓ Dry Run Completed", "Command that would run:\n"+m.dryRunOutput))
	} else if m.restoreErr != nil {
		body.WriteString(renderCard(m.width, "! Restore Failed", errorStyle.Render(m.restoreErr.Error())))
	} else {
		body.WriteString(renderCard(m.width, "✓ Restore Completed", renderKeyValueGrid([]kvRow{
			{Label: "Profile", Value: m.result.Profile.Name},
			{Label: "Database", Value: m.result.Profile.Database},
			{Label: "File", Value: truncateMiddle(m.result.File, safePanelWidth(m.width)-12)},
		}, 10)))
	}

	return renderScreenFrame(m.width, "Restoration Result", "Final state", body.String(), []keyHint{
		{Key: "Any key", Label: "another operation"},
		{Key: "Q/Esc", Label: "quit", Danger: true},
	})
}

func (m Model) updateSelectMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r", "R", "1":
		m.result.Mode = ModeRestore
		if m.hasActiveProfile() {
			m.step = stepSelectFile
		} else {
			m.step = stepSelectProfile
		}
	case "d", "D", "2":
		m.result.Mode = ModeDump
		if m.hasActiveProfile() {
			m.dumpOutputInput.SetValue("")
			m.dumpOutputInput.Focus()
			m.step = stepDumpOutputPath
		} else {
			m.step = stepSelectProfile
		}
	case "m", "M", "3":
		m.result.Mode = ModeMigrate
		if m.hasActiveProfile() {
			m.step = stepMigrateSelectDest
		} else {
			m.step = stepSelectProfile
		}
	case "p", "P":
		if m.profileIdx >= len(m.profiles) {
			m.profileIdx = 0
		}
		for i, p := range m.profiles {
			if p.Name == m.result.Profile.Name {
				m.profileIdx = i
				break
			}
		}
		m.step = stepSelectProfile
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) hasActiveProfile() bool {
	if m.result.Profile.Name == "" {
		return false
	}
	_, ok := m.cfg.Profiles[m.result.Profile.Name]
	return ok
}

func (m Model) viewSelectMode() string {
	var body strings.Builder

	if m.hasActiveProfile() {
		p := m.result.Profile
		body.WriteString(renderProfileSummaryCard(m.width, "Active Profile", p.Name, p.Driver, p.Host, p.Port, p.Database, p.User))
		body.WriteString("\n\n")
	}

	body.WriteString(renderSectionTitle("Choose an operation"))
	body.WriteString("\n\n")
	body.WriteString(renderOperationCard(m.width, "R", "Restore", "TARGET DB", "Import a dump file into a database"))
	body.WriteString("\n")
	body.WriteString(renderOperationCard(m.width, "D", "Dump", "BACKUP", "Export a database to a dump file"))
	body.WriteString("\n")
	body.WriteString(renderOperationCard(m.width, "M", "Migrate", "SRC → DST", "Copy a database between profiles"))

	profileLabel := "choose profile"
	if m.hasActiveProfile() {
		profileLabel = "switch profile"
	}
	return renderScreenFrame(m.width, "Select Operation", "Dark Database Command Center", body.String(), []keyHint{
		{Key: "R", Label: "restore"},
		{Key: "D", Label: "dump"},
		{Key: "M", Label: "migrate"},
		{Key: "P", Label: profileLabel},
		{Key: "Q", Label: "quit", Danger: true},
	})
}

// ─── Profile selector: route to correct next step based on mode ─────────────

// Override profile selector Enter to route to dump or restore file steps.
// This is handled inside updateProfileSelector already; we just need to intercept
// the "enter" case and redirect when mode == ModeDump.

// ─── Dump Output Path ────────────────────────────────────────────────────────

func (m Model) updateDumpOutputPath(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		path := strings.TrimSpace(m.dumpOutputInput.Value())
		if path == "" {
			return m, nil
		}
		m.result.DumpFile = path
		m.step = stepDumpConfirm
		return m, nil
	case "esc", "ctrl+c":
		m.step = stepSelectProfile
		return m, nil
	}
	var cmd tea.Cmd
	m.dumpOutputInput, cmd = m.dumpOutputInput.Update(msg)
	return m, cmd
}

func (m Model) viewDumpOutputPath() string {
	var sb strings.Builder
	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Dump Output Path")
	sb.WriteString(title + "\n\n")

	sb.WriteString("  Profile: " + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(m.result.Profile.Name) + "\n\n")
	sb.WriteString("  Enter the output file path for the dump:\n\n")
	sb.WriteString("  " + m.dumpOutputInput.View() + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
		"  Enter to confirm • Esc to go back\n"))
	return sb.String()
}

// ─── Dump Confirm ────────────────────────────────────────────────────────────

func (m Model) updateDumpConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle filter input mode
	if m.filterInputMode {
		switch msg.String() {
		case "enter":
			m.saveFilterInput()
			return m, nil
		case "esc":
			m.cancelFilterInput()
			return m, nil
		default:
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			return m, cmd
		}
	}

	switch msg.String() {
	case "enter", "y":
		m.result.DumpConfirm = true
		drv, err := driver.Get(m.result.Profile.Driver)
		if err != nil {
			m.dumpErr = err
			m.step = stepDumpResult
			return m, nil
		}

		format := driver.Format(m.result.DumpSettings.Format)
		if format == "" {
			format = driver.FormatCustom
		}

		opts := driver.DumpOptions{
			Profile:       m.result.Profile,
			FilePath:      m.result.DumpFile,
			Format:        format,
			IncludeTable:  m.result.DumpSettings.IncludeTable,
			ExcludeTable:  m.result.DumpSettings.ExcludeTable,
			IncludeSchema: m.result.DumpSettings.IncludeSchema,
			ExcludeSchema: m.result.DumpSettings.ExcludeSchema,
		}

		ch, err := drv.Dump(context.Background(), opts)
		if err != nil {
			m.dumpErr = err
			m.step = stepDumpResult
			return m, nil
		}
		m.dumpProgressChan = ch
		m.step = stepDumping
		m.dumpProgressPct = 0
		m.dumpProgressText = "Starting dump..."
		return m, listenToDumpProgress(ch)

	case "n", "q", "ctrl+c", "esc":
		m.step = stepDumpOutputPath
	case "t":
		m.openFilterInput("include-table")
		return m, m.filterInput.Focus()
	case "T":
		m.openFilterInput("exclude-table")
		return m, m.filterInput.Focus()
	case "h":
		m.openFilterInput("include-schema")
		return m, m.filterInput.Focus()
	case "H":
		m.openFilterInput("exclude-schema")
		return m, m.filterInput.Focus()
	}
	return m, nil
}

func (m Model) viewDumpConfirm() string {
	var body strings.Builder
	format := m.result.DumpSettings.Format
	if format == "" {
		format = "custom"
	}
	p := m.result.Profile
	s := m.result.DumpSettings

	body.WriteString(renderProfileSummaryCard(m.width, "Source Profile", p.Name, p.Driver, p.Host, p.Port, p.Database, p.User))
	body.WriteString("\n")
	body.WriteString(renderCard(m.width, "Output File", renderKeyValueGrid([]kvRow{
		{Label: "Path", Value: truncateMiddle(m.result.DumpFile, safePanelWidth(m.width)-14)},
		{Label: "Format", Value: strings.ToUpper(format), Kind: badgePrimary},
	}, 10)))
	body.WriteString("\n")
	body.WriteString(renderCard(m.width, "Dump Options", renderFilterRows(s.IncludeSchema, s.ExcludeSchema, s.IncludeTable, s.ExcludeTable)))

	return renderScreenFrame(m.width, "Confirm Dump", "Pre-flight review before exporting source database", body.String(), []keyHint{
		{Key: "Enter/Y", Label: "dump"},
		{Key: "T/H", Label: "filters"},
		{Key: "Esc", Label: "back", Danger: true},
	})
}

// ─── Dumping ─────────────────────────────────────────────────────────────────

func (m Model) viewDumping() string {
	var sb strings.Builder

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Dumping Database")
	sb.WriteString(title + "\n\n")

	sb.WriteString("  Profile: " + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(m.result.Profile.Name) + "\n")
	sb.WriteString("  Output:  " + lipgloss.NewStyle().Foreground(colorSubtext).Render(m.result.DumpFile) + "\n\n")

	barWidth := progressWidth(m.width)
	sb.WriteString(fmt.Sprintf("  [%s] %.0f%%\n\n", renderProgressBar(barWidth, m.dumpProgressPct), clampPercent(m.dumpProgressPct)))
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSubtext).Render(m.dumpProgressText) + "\n")

	return sb.String()
}

// finalizeDump records history and sends desktop notification.
func (m *Model) finalizeDump(err error) {
	format := driver.Format(m.result.DumpSettings.Format)
	if format == "" {
		format = driver.FormatCustom
	}
	cmdString := fmt.Sprintf("pg_dump -h %s -p %d -U %s -d %s -f %s -F %s",
		m.result.Profile.Host, m.result.Profile.Port,
		m.result.Profile.User, m.result.Profile.Database,
		m.result.DumpFile, format)

	historyRec := history.HistoryRecord{
		File:    m.result.DumpFile,
		Profile: m.result.Profile.Name,
		Time:    time.Now(),
		Success: err == nil,
		Command: cmdString,
	}
	if err != nil {
		historyRec.Error = err.Error()
		m.dumpErr = err
	}
	_ = history.AppendHistory(historyRec)

	if err != nil {
		_ = beeep.Notify("DBTool Dump Failed", fmt.Sprintf("Profile: %s\nError: %v", m.result.Profile.Name, err), "")
	} else {
		_ = beeep.Notify("DBTool Dump Success", fmt.Sprintf("Database %s dumped to %s", m.result.Profile.Database, m.result.DumpFile), "")
	}
}

func (m Model) viewDumpResult() string {
	var body strings.Builder

	if m.dumpErr != nil {
		body.WriteString(renderCard(m.width, "! Dump Failed", errorStyle.Render(m.dumpErr.Error())))
	} else {
		body.WriteString(renderCard(m.width, "✓ Dump Completed", renderKeyValueGrid([]kvRow{
			{Label: "Profile", Value: m.result.Profile.Name},
			{Label: "Database", Value: m.result.Profile.Database},
			{Label: "Output", Value: truncateMiddle(m.result.DumpFile, safePanelWidth(m.width)-12)},
		}, 10)))
	}

	return renderScreenFrame(m.width, "Dump Result", "Final state", body.String(), []keyHint{
		{Key: "Any key", Label: "another operation"},
		{Key: "Q/Esc", Label: "quit", Danger: true},
	})
}

// ─── Migrate: destination profile selector ───────────────────────────────────

func (m Model) updateMigrateDestSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.profiles) == 0 {
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.step = stepSelectMode
		}
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		if m.migrateDestIdx > 0 {
			m.migrateDestIdx--
		}
	case "down", "j":
		if m.migrateDestIdx < len(m.profiles)-1 {
			m.migrateDestIdx++
		}
	case "enter", " ":
		candidate := m.profiles[m.migrateDestIdx]
		if candidate.Name == m.result.Profile.Name {
			return m, nil
		}
		m.result.DestProfile = candidate
		m.step = stepMigrateConfirm
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.step = stepSelectProfile
	}
	return m, nil
}

func (m Model) viewMigrateDestSelector() string {
	var body strings.Builder

	src := m.result.Profile
	body.WriteString(renderProfileSummaryCard(m.width, "Source Profile", src.Name, src.Driver, src.Host, src.Port, src.Database, src.User))
	body.WriteString("\n\n")
	body.WriteString(renderSectionTitle("Choose a destination profile"))
	body.WriteString("\n\n")

	if len(m.profiles) == 0 {
		body.WriteString(renderCard(m.width, "No profiles found", warningStyle.Render("Add another profile before migrating.")))
	} else {
		body.WriteString(renderProfileRows(m.width, m.profiles, m.migrateDestIdx, src.Name))
	}

	return renderScreenFrame(m.width, "Select Destination Profile", "Target database for profile-to-profile copy", body.String(), []keyHint{
		{Key: "↑/↓", Label: "navigate"},
		{Key: "Enter", Label: "select"},
		{Key: "Esc", Label: "back", Danger: true},
		{Key: "Q", Label: "quit", Danger: true},
	})
}

// ─── Migrate: confirm ────────────────────────────────────────────────────────

func (m Model) updateMigrateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle filter input mode
	if m.filterInputMode {
		switch msg.String() {
		case "enter":
			m.saveFilterInput()
			return m, nil
		case "esc":
			m.cancelFilterInput()
			return m, nil
		default:
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			return m, cmd
		}
	}

	switch msg.String() {
	case "enter", "y":
		m.result.MigrateConfirm = true
		src := m.result.Profile
		dst := m.result.DestProfile

		if src.Name == dst.Name {
			m.migrateErr = fmt.Errorf("source and destination profiles must be different")
			m.step = stepMigrateResult
			return m, nil
		}
		if src.Driver != dst.Driver {
			m.migrateErr = fmt.Errorf("cross-driver migration is not supported (source: %s, dest: %s)", src.Driver, dst.Driver)
			m.step = stepMigrateResult
			return m, nil
		}

		drv, err := driver.Get(src.Driver)
		if err != nil {
			m.migrateErr = err
			m.step = stepMigrateResult
			return m, nil
		}

		format := driver.Format(m.result.MigrateSettings.Format)
		if format == "" {
			format = driver.FormatCustom
		}

		tempPath, err := createMigrateTempPath(format)
		if err != nil {
			m.migrateErr = err
			m.step = stepMigrateResult
			return m, nil
		}
		m.migrateTempPath = tempPath

		dumpOpts := driver.DumpOptions{
			Profile:       src,
			FilePath:      tempPath,
			Format:        format,
			SchemaOnly:    m.result.MigrateSettings.SchemaOnly,
			DataOnly:      m.result.MigrateSettings.DataOnly,
			IncludeTable:  m.result.MigrateSettings.IncludeTable,
			ExcludeTable:  m.result.MigrateSettings.ExcludeTable,
			IncludeSchema: m.result.MigrateSettings.IncludeSchema,
			ExcludeSchema: m.result.MigrateSettings.ExcludeSchema,
		}

		ch, err := drv.Dump(context.Background(), dumpOpts)
		if err != nil {
			_ = os.RemoveAll(tempPath)
			m.migrateTempPath = ""
			m.migrateErr = err
			m.step = stepMigrateResult
			return m, nil
		}

		m.migrateProgressChan = ch
		m.migratePhase = 0
		m.migrateProgressPct = 0
		m.migrateProgressText = "Phase 1/2: Dumping from source..."
		m.migrateErr = nil
		m.step = stepMigrating
		return m, listenToMigrateProgress(ch, 0)

	case "n", "q", "ctrl+c", "esc":
		m.step = stepMigrateSelectDest
	case "c", "C":
		m.result.MigrateSettings.Clean = !m.result.MigrateSettings.Clean
	case "m", "M":
		m.result.MigrateSettings.CreateIfMissing = !m.result.MigrateSettings.CreateIfMissing
	case "s", "S":
		m.result.MigrateSettings.SchemaOnly = !m.result.MigrateSettings.SchemaOnly
		if m.result.MigrateSettings.SchemaOnly {
			m.result.MigrateSettings.DataOnly = false
		}
	case "a", "A":
		m.result.MigrateSettings.DataOnly = !m.result.MigrateSettings.DataOnly
		if m.result.MigrateSettings.DataOnly {
			m.result.MigrateSettings.SchemaOnly = false
		}
	case "o", "O":
		m.result.MigrateSettings.Optimize = !m.result.MigrateSettings.Optimize
	case "t":
		m.openFilterInput("include-table")
		return m, m.filterInput.Focus()
	case "T":
		m.openFilterInput("exclude-table")
		return m, m.filterInput.Focus()
	case "h":
		m.openFilterInput("include-schema")
		return m, m.filterInput.Focus()
	case "H":
		m.openFilterInput("exclude-schema")
		return m, m.filterInput.Focus()
	}
	return m, nil
}

func (m Model) viewMigrateConfirm() string {
	var body strings.Builder
	src := m.result.Profile
	dst := m.result.DestProfile
	s := m.result.MigrateSettings

	body.WriteString(renderProfileSummaryCard(m.width, "Source Profile", src.Name, src.Driver, src.Host, src.Port, src.Database, src.User))
	body.WriteString("\n")
	body.WriteString(lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Align(lipgloss.Center).Width(safePanelWidth(m.width)).Render("SRC → DST"))
	body.WriteString("\n")
	body.WriteString(renderProfileSummaryCard(m.width, "Target Profile", dst.Name, dst.Driver, dst.Host, dst.Port, dst.Database, dst.User))
	body.WriteString("\n")

	cleanValue := "No"
	cleanKind := badgeNeutral
	if s.Clean {
		cleanValue = "CLEAN"
		cleanKind = badgeDanger
	}
	createValue := "No"
	createKind := badgeNeutral
	if s.CreateIfMissing {
		createValue = "CREATE"
		createKind = badgeSuccess
	}
	modeValue := "FULL"
	modeKind := badgePrimary
	if s.SchemaOnly {
		modeValue = "SCHEMA ONLY"
	} else if s.DataOnly {
		modeValue = "DATA ONLY"
	}
	optimizeValue := "No"
	optimizeKind := badgeNeutral
	if s.Optimize {
		optimizeValue = "OPTIMIZE"
		optimizeKind = badgeSuccess
	}

	options := renderKeyValueGrid([]kvRow{
		{Label: "Mode", Value: modeValue, Kind: modeKind},
		{Label: "Clean", Value: cleanValue, Kind: cleanKind},
		{Label: "Create DB", Value: createValue, Kind: createKind},
		{Label: "Optimize", Value: optimizeValue, Kind: optimizeKind},
		{Label: "Jobs", Value: fmt.Sprintf("%d", s.Jobs)},
		{Label: "Format", Value: s.Format, Kind: badgePrimary},
	}, 12)
	options += "\n" + renderFilterRows(s.IncludeSchema, s.ExcludeSchema, s.IncludeTable, s.ExcludeTable)
	body.WriteString(renderCard(m.width, "Migrate Options", options))
	body.WriteString("\n")

	safety := renderDanger("Migration target may be overwritten during restore phase.")
	if s.Clean {
		safety += "\n" + renderDanger("Clean mode may drop target database objects before migrate restore.")
	}
	body.WriteString(renderCard(m.width, "Safety Review", safety))

	return renderScreenFrame(m.width, "Confirm Migrate", "Review source and target before copying data", body.String(), []keyHint{
		{Key: "C", Label: "clean"},
		{Key: "M", Label: "create-db"},
		{Key: "S/A", Label: "mode"},
		{Key: "O", Label: "optimize"},
		{Key: "T/H", Label: "filters"},
		{Key: "Enter", Label: "proceed"},
		{Key: "Esc", Label: "back", Danger: true},
	})
}

// ─── Migrating (2-phase progress) ─────────────────────────────────────────────

func (m Model) viewMigrating() string {
	var sb strings.Builder

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Migrating Database")
	sb.WriteString(title + "\n\n")

	sb.WriteString("  Source: " + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(m.result.Profile.Name) + "\n")
	sb.WriteString("  Target: " + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(m.result.DestProfile.Name) + "\n\n")

	phaseLabel := "Phase 1/2: Dump"
	if m.migratePhase == 1 {
		phaseLabel = "Phase 2/2: Restore"
	}

	barWidth := progressWidth(m.width)
	sb.WriteString(fmt.Sprintf("  [%s] %.0f%%  %s\n\n", renderProgressBar(barWidth, m.migrateProgressPct), clampPercent(m.migrateProgressPct), phaseLabel))
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSubtext).Render(m.migrateProgressText) + "\n")

	return sb.String()
}

// handleMigratePhaseFinished transitions between the dump and restore phases,
// or finalizes the migration after the restore phase completes.
func (m Model) handleMigratePhaseFinished(msg migratePhaseFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil || m.migrateErr != nil {
		err := msg.err
		if err == nil {
			err = m.migrateErr
		}
		m.finalizeMigrate(err)
		m.step = stepMigrateResult
		return m, nil
	}

	if msg.phase == 0 {
		src := m.result.Profile
		dst := m.result.DestProfile
		drv, err := driver.Get(src.Driver)
		if err != nil {
			m.finalizeMigrate(err)
			m.step = stepMigrateResult
			return m, nil
		}

		format := driver.Format(m.result.MigrateSettings.Format)
		if format == "" {
			format = driver.FormatCustom
		}
		detected, err := drv.DetectFormat(m.migrateTempPath)
		if err != nil || detected == driver.FormatUnknown {
			detected = format
		}

		restoreOpts := driver.RestoreOptions{
			Profile:         dst,
			FilePath:        m.migrateTempPath,
			Format:          detected,
			Jobs:            m.result.MigrateSettings.Jobs,
			Clean:           m.result.MigrateSettings.Clean,
			IncludeTable:    m.result.MigrateSettings.IncludeTable,
			ExcludeTable:    m.result.MigrateSettings.ExcludeTable,
			IncludeSchema:   m.result.MigrateSettings.IncludeSchema,
			ExcludeSchema:   m.result.MigrateSettings.ExcludeSchema,
			CreateIfMissing: m.result.MigrateSettings.CreateIfMissing,
		}

		ch, err := drv.Restore(context.Background(), restoreOpts)
		if err != nil {
			m.finalizeMigrate(err)
			m.step = stepMigrateResult
			return m, nil
		}
		m.migrateProgressChan = ch
		m.migratePhase = 1
		m.migrateProgressPct = 0
		m.migrateProgressText = "Phase 2/2: Restoring into target..."
		return m, listenToMigrateProgress(ch, 1)
	}

	m.finalizeMigrate(nil)
	m.step = stepMigrateResult
	return m, nil
}

// finalizeMigrate records history (dump + restore entries) and sends a notification,
// then cleans up the temp dump file.
func (m *Model) finalizeMigrate(err error) {
	src := m.result.Profile
	dst := m.result.DestProfile

	if m.migrateTempPath != "" {
		defer os.RemoveAll(m.migrateTempPath)
	}

	now := time.Now()
	dumpCmd := fmt.Sprintf("migrate(dump): pg_dump -h %s -p %d -U %s -d %s -f %s",
		src.Host, src.Port, src.User, src.Database, m.migrateTempPath)

	dumpRec := history.HistoryRecord{
		File:    m.migrateTempPath,
		Profile: src.Name,
		Time:    now,
		Success: m.migratePhase >= 1,
		Command: dumpCmd,
	}
	if m.migratePhase == 0 && err != nil {
		dumpRec.Success = false
		dumpRec.Error = err.Error()
	}
	_ = history.AppendHistory(dumpRec)

	if m.migratePhase >= 1 {
		restoreCmd := fmt.Sprintf("migrate(restore): pg_restore -h %s -p %d -U %s -d %s -f %s",
			dst.Host, dst.Port, dst.User, dst.Database, m.migrateTempPath)
		restoreRec := history.HistoryRecord{
			File:    m.migrateTempPath,
			Profile: dst.Name,
			Time:    now,
			Success: err == nil,
			Command: restoreCmd,
		}
		if err != nil {
			restoreRec.Error = err.Error()
		}
		_ = history.AppendHistory(restoreRec)
	}

	if err != nil {
		m.migrateErr = err
		_ = beeep.Notify("DBTool Migrate Failed", fmt.Sprintf("%s -> %s: %v", src.Name, dst.Name, err), "")
	} else {
		_ = beeep.Notify("DBTool Migrate Success", fmt.Sprintf("%s -> %s migrated", src.Database, dst.Database), "")

		// Post-migrate optimization on target
		if m.result.MigrateSettings.Optimize {
			optDrv, _ := driver.Get(dst.Driver)
			if optErr := optDrv.Optimize(context.Background(), dst); optErr != nil {
				_ = beeep.Notify("DBTool Optimization Warning", fmt.Sprintf("Migration succeeded but optimization failed: %v", optErr), "")
			}
		}
	}
}

func (m Model) viewMigrateResult() string {
	var body strings.Builder

	if m.migrateErr != nil {
		body.WriteString(renderCard(m.width, "! Migration Failed", errorStyle.Render(m.migrateErr.Error())))
	} else {
		body.WriteString(renderCard(m.width, "✓ Migration Completed", renderKeyValueGrid([]kvRow{
			{Label: "Source", Value: fmt.Sprintf("%s (%s)", m.result.Profile.Name, m.result.Profile.Database)},
			{Label: "Target", Value: fmt.Sprintf("%s (%s)", m.result.DestProfile.Name, m.result.DestProfile.Database)},
		}, 10)))
	}

	return renderScreenFrame(m.width, "Migration Result", "Final state", body.String(), []keyHint{
		{Key: "Any key", Label: "another operation"},
		{Key: "Q/Esc", Label: "quit", Danger: true},
	})
}

// createMigrateTempPath allocates a temp path for the intermediate dump file/dir.
func createMigrateTempPath(format driver.Format) (string, error) {
	pattern := "dbtool-migrate-*.dump"
	if format == driver.FormatDirectory {
		pattern = "dbtool-migrate-*"
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	f.Close()
	if format == driver.FormatDirectory {
		_ = os.Remove(name)
		if err := os.MkdirAll(name, 0755); err != nil {
			return "", err
		}
	}
	return name, nil
}
