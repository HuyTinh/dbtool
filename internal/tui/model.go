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
	Profile      config.Profile
	File         string
	Settings     RestoreSettings
	Confirm      bool
	// Dump-specific
	Mode         Mode
	DumpFile     string
	DumpSettings DumpSettings
	DumpConfirm  bool
}

// --- Main model ---
type Model struct {
	step step

	// Profile selector
	profiles   []config.Profile
	profileIdx int

	// Form inputs (for Add/Edit)
	inputs     []textinput.Model
	focusedIdx int
	isEditing  bool
	origName   string
	formErr    string

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
	dumpOutputInput textinput.Model
	dumpProgressChan <-chan driver.Progress
	dumpProgressPct  float64
	dumpProgressText string
	dumpErr          error

	// Terminal size
	width  int
	height int

	err error
}

func NewModel(cfg *config.Config, initSettings RestoreSettings) Model {
	profiles := make([]config.Profile, 0, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})

	cwd, _ := os.Getwd()

	dumpInput := textinput.New()
	dumpInput.Placeholder = "e.g. /backups/mydb.dump"
	dumpInput.CharLimit = 512
	dumpInput.Width = 60

	searchInput := textinput.New()
	searchInput.Placeholder = "type to filter..."
	searchInput.CharLimit = 128
	searchInput.Width = 50

	m := Model{
		step:            stepSelectMode,
		profiles:        profiles,
		currentDir:      cwd,
		width:           80,
		height:           24,
		cfg:             cfg,
		dumpOutputInput: dumpInput,
		searchInput:     searchInput,
	}
	m.result.Settings = initSettings
	m.result.DumpSettings = DumpSettings{Format: "custom"}
	m.entries = listDir(cwd)
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

type progressMsg driver.Progress
type restoreFinishedMsg struct {
	err error
}

func listenToProgress(ch <-chan driver.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return restoreFinishedMsg{}
		}
		return progressMsg(p)
	}
}

type dumpProgressMsg driver.Progress
type dumpFinishedMsg struct {
	err error
}

func listenToDumpProgress(ch <-chan driver.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return dumpFinishedMsg{}
		}
		return dumpProgressMsg(p)
	}
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
			m.inputs = m.initFormFields(nil)
			m.focusedIdx = 0
			m.isEditing = false
			m.formErr = ""
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
		} else {
			m.step = stepSelectFile
		}
	case "a", "A":
		m.inputs = m.initFormFields(nil)
		m.focusedIdx = 0
		m.isEditing = false
		m.formErr = ""
		m.step = stepEditProfileForm
	case "e", "E":
		selected := m.profiles[m.profileIdx]
		m.inputs = m.initFormFields(&selected)
		m.focusedIdx = 0
		m.isEditing = true
		m.origName = selected.Name
		m.formErr = ""
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
			m.fileIdx = 0
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
	case "+", "=":
		m.result.Settings.Jobs++
	case "-":
		if m.result.Settings.Jobs > 1 {
			m.result.Settings.Jobs--
		}
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
	}
	return ""
}

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
	var sb strings.Builder

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Select a Connection Profile")
	sb.WriteString(title + "\n\n")

	if len(m.profiles) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("  No profiles found.") + "\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("  Press [a] to add a new profile.\n\n"))
		sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("  Press q to quit.\n"))
		return sb.String()
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(colorAccent).Render("  Choose a profile to restore into:\n\n"))

	maxNameLen := 0
	for _, p := range m.profiles {
		if len(p.Name) > maxNameLen {
			maxNameLen = len(p.Name)
		}
	}
	if maxNameLen < 12 {
		maxNameLen = 12
	}

	for i, p := range m.profiles {
		selected := i == m.profileIdx
		badge := renderBadge(p.Driver, "#A78BFA")
		connStr := fmt.Sprintf("%s:%d/%s", p.Host, p.Port, p.Database)

		namePart := fmt.Sprintf("%-*s", maxNameLen+2, p.Name)
		row := renderRow(selected, namePart)
		connPart := lipgloss.NewStyle().Foreground(colorMuted).Render(" " + connStr)
		sb.WriteString("  " + row + " " + badge + connPart + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("  [up/down] navigate   [Enter] select   [a] add   [e] edit   [d] delete   [q] quit\n"))
	return sb.String()
}

func (m Model) viewFileBrowser() string {
	var sb strings.Builder

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Select a Dump File")
	sb.WriteString(title + "\n\n")

	p := m.result.Profile
	badge := renderBadge(p.Driver, "#A78BFA")
	profileInfo := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(p.Name)
	connStr := lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("  %s:%d/%s", p.Host, p.Port, p.Database))
	sb.WriteString("  Profile: " + profileInfo + " " + badge + connStr + "\n")

	// Truncate path for display
	displayDir := m.currentDir
	maxDirLen := m.width - 12
	if maxDirLen > 10 && len(displayDir) > maxDirLen {
		displayDir = "..." + displayDir[len(displayDir)-maxDirLen:]
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("  Dir: ") +
		lipgloss.NewStyle().Foreground(colorSubtext).Render(displayDir) + "\n")

	if m.searching {
		sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("  Filter: ") +
			m.searchInput.View() + "\n")
	}

	vis := m.visibleEntries()
	sb.WriteString("\n")

	if len(vis) == 0 {
		if m.searching && m.searchInput.Value() != "" {
			sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("  (no matches)\n"))
		} else {
			sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("  (empty directory)\n"))
		}
	} else {
		// Only render visible window (scrolling)
		headerLines := 10
		if m.searching {
			headerLines = 11
		}
		visible := m.height - headerLines
		if visible < 5 {
			visible = 5
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
			e := vis[i]
			selected := i == m.fileIdx

			var badgeStr string
			var extra string

			if e.IsDir {
				badgeStr = renderBadge("DIR", "#FBBF24")
			} else {
				ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(e.Name), "."))
				if ext == "" {
					ext = "FILE"
				}
				badgeStr = renderBadge(ext, "#34D399")
				extra = lipgloss.NewStyle().Foreground(colorMuted).Render("  " + formatBytes(e.Size))
			}

			maxNameW := m.width - 20
			if maxNameW < 20 {
				maxNameW = 20
			}
			displayName := e.Name
			if len(displayName) > maxNameW {
				displayName = displayName[:maxNameW-3] + "..."
			}

			namePart := fmt.Sprintf("%-*s", maxNameW, displayName)
			row := renderRow(selected, namePart)
			sb.WriteString("  " + row + " " + badgeStr + extra + "\n")
		}

		// Scroll indicator
		total := len(vis)
		if total > visible {
			sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
				fmt.Sprintf("  ... showing %d-%d of %d items\n", start+1, end, total),
			))
		}
	}

	sb.WriteString("\n")
	if m.searching {
		sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
			"  [up/down] navigate results   [Enter] select   [esc] clear filter\n"))
	} else {
		sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
			"  [up/down] navigate   [/] filter   [Enter] open/select   [Backspace/left] parent   [esc] back\n"))
	}
	return sb.String()
}

func (m Model) viewConfirm() string {
	var sb strings.Builder

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Confirm Restore")
	sb.WriteString(title + "\n\n")

	p := m.result.Profile
	rows := []struct{ label, value string }{
		{"Profile", p.Name},
		{"Driver", p.Driver},
		{"Host", fmt.Sprintf("%s:%d", p.Host, p.Port)},
		{"Database", p.Database},
		{"User", p.User},
		{"File", m.result.File},
	}

	for _, row := range rows {
		label := lipgloss.NewStyle().Foreground(colorMuted).Width(14).Render(row.label + ":")
		val := lipgloss.NewStyle().Foreground(colorSubtext).Render(row.value)
		sb.WriteString("  " + label + " " + val + "\n")
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("  Restore Settings:") + "\n")

	s := m.result.Settings
	cleanVal := lipgloss.NewStyle().Foreground(colorMuted).Render("No")
	if s.Clean {
		cleanVal = lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("Yes (--clean: drop objects first)")
	}

	createDbVal := lipgloss.NewStyle().Foreground(colorMuted).Render("No")
	if s.CreateIfMissing {
		createDbVal = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("Yes (create DB if missing)")
	}

	settingsRows := []struct{ label, value string }{
		{"[c] Clean", cleanVal},
		{"[m] Create DB", createDbVal},
		{"[+/-] Jobs", fmt.Sprintf("%d parallel processes", s.Jobs)},
		{"Format", s.Format},
	}

	for _, row := range settingsRows {
		label := lipgloss.NewStyle().Foreground(colorMuted).Width(14).Render(row.label + ":")
		val := lipgloss.NewStyle().Foreground(colorSubtext).Render(row.value)
		sb.WriteString("  " + label + " " + val + "\n")
	}

	// Filter display
	if len(s.IncludeSchema) > 0 {
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorMuted).Width(14).Render("Inc Schema:") + " " + strings.Join(s.IncludeSchema, ", ") + "\n")
	}
	if len(s.ExcludeSchema) > 0 {
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorMuted).Width(14).Render("Exc Schema:") + " " + strings.Join(s.ExcludeSchema, ", ") + "\n")
	}
	if len(s.IncludeTable) > 0 {
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorMuted).Width(14).Render("Inc Table:") + " " + strings.Join(s.IncludeTable, ", ") + "\n")
	}
	if len(s.ExcludeTable) > 0 {
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorMuted).Width(14).Render("Exc Table:") + " " + strings.Join(s.ExcludeTable, ", ") + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render(
		"  ! Press Enter to execute database restoration.\n"))
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
		"  [c] toggle clean   [m] toggle create-db   [+/-] jobs   [Enter] proceed   [esc] back\n"))
	return sb.String()
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

func (m Model) updateEditProfileForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "esc":
		m.step = stepSelectProfile
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
	}

	if err := config.SaveConfig(m.cfg); err != nil {
		m.formErr = fmt.Sprintf("Failed to save config: %v", err)
		return m, nil
	}

	// Reload profiles list
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
	profiles := make([]config.Profile, 0, len(m.cfg.Profiles))
	for _, p := range m.cfg.Profiles {
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})
	m.profiles = profiles

	if m.result.Profile.Name != "" {
		if p, ok := m.cfg.Profiles[m.result.Profile.Name]; ok {
			p.Name = m.result.Profile.Name
			m.result.Profile = p
		} else {
			m.result.Profile = config.Profile{}
		}
	}
}

func (m Model) viewEditProfileForm() string {
	var sb strings.Builder

	titleStr := "dbtool — Add Connection Profile"
	if m.isEditing {
		titleStr = fmt.Sprintf("dbtool — Edit Profile: %s", m.origName)
	}

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render(titleStr)
	sb.WriteString(title + "\n\n")

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

	sb.WriteString(panelStyle.Width(m.width - 4).Render(formContent))
	sb.WriteString("\n")

	if m.formErr != "" {
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true).Render("Error: "+m.formErr) + "\n\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
		"  [tab/shift+tab/up/down] navigate   [Enter] save on last field   [esc] cancel\n"))
	return sb.String()
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
	sb.WriteString("  " + labelStyle.Render("Database:") + " " + valueStyle.Render(fmt.Sprintf("%s @ %s:%d", selected.Database, selected.Host, selected.Port)) + "\n\n")

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

	width := m.width - 10
	if width < 20 {
		width = 20
	}
	if width > 60 {
		width = 60
	}

	filledW := int(float64(width) * (m.progressPct / 100.0))
	if filledW < 0 {
		filledW = 0
	}
	if filledW > width {
		filledW = width
	}
	emptyW := width - filledW

	filledStr := lipgloss.NewStyle().Foreground(colorSuccess).Render(strings.Repeat("█", filledW))
	emptyStr := lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Repeat("░", emptyW))

	sb.WriteString(fmt.Sprintf("  [%s%s] %.0f%%\n\n", filledStr, emptyStr, m.progressPct))
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSubtext).Render(m.progressText) + "\n")

	return sb.String()
}

func (m Model) viewRestoreResult() string {
	var sb strings.Builder

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Restoration Result")
	sb.WriteString(title + "\n\n")

	if m.result.Settings.DryRun {
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("Dry Run Completed Successfully") + "\n\n")
		sb.WriteString("  Command that would run:\n")
		sb.WriteString(panelStyle.Width(m.width - 4).Render(m.dryRunOutput) + "\n\n")
	} else if m.restoreErr != nil {
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true).Render("❌ Restoration Failed") + "\n\n")
		sb.WriteString("  Error details:\n")
		sb.WriteString(panelStyle.Width(m.width - 4).Render(m.restoreErr.Error()) + "\n\n")
	} else {
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("✓ Database Restored Successfully!") + "\n\n")
		sb.WriteString("  Profile: " + valueStyle.Render(m.result.Profile.Name) + "\n")
		sb.WriteString("  Database: " + valueStyle.Render(m.result.Profile.Database) + "\n")
		sb.WriteString("  Dump File: " + valueStyle.Render(m.result.File) + "\n\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
		"  Press any key to do another operation • Q / Esc to quit\n"))
	return sb.String()
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
	var sb strings.Builder

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Select Operation")
	sb.WriteString(title + "\n\n")

	if m.hasActiveProfile() {
		p := m.result.Profile
		badge := renderBadge(p.Driver, "#A78BFA")
		conn := lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("  %s:%d/%s", p.Host, p.Port, p.Database))
		sb.WriteString("  Active: " +
			lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(p.Name) + " " + badge + conn + "\n\n")
	}

	sb.WriteString(renderRow(false, "Choose what you want to do:") + "\n\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("  [R]  ") +
		lipgloss.NewStyle().Foreground(colorText).Render("Restore") + " — Import a dump file into a database\n\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("  [D]  ") +
		lipgloss.NewStyle().Foreground(colorText).Render("Dump") + "    — Export a database to a dump file\n\n")

	hint := "\n  Press R or D to begin"
	if m.hasActiveProfile() {
		hint += " • [p] switch profile"
	} else {
		hint += " • [p] choose profile"
	}
	hint += " • q to quit\n"
	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(hint))
	return sb.String()
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
	}
	return m, nil
}

func (m Model) viewDumpConfirm() string {
	var sb strings.Builder
	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Confirm Dump")
	sb.WriteString(title + "\n\n")

	format := m.result.DumpSettings.Format
	if format == "" {
		format = "custom"
	}

	sb.WriteString(panelStyle.Width(m.width-4).Render(
		labelStyle.Render("Profile")+" "+valueStyle.Render(m.result.Profile.Name)+"\n"+
			labelStyle.Render("Host   ")+" "+valueStyle.Render(fmt.Sprintf("%s:%d", m.result.Profile.Host, m.result.Profile.Port))+"\n"+
			labelStyle.Render("DB     ")+" "+valueStyle.Render(m.result.Profile.Database)+"\n"+
			labelStyle.Render("Output ")+" "+valueStyle.Render(m.result.DumpFile)+"\n"+
			labelStyle.Render("Format ")+" "+valueStyle.Render(format),
	) + "\n\n")

	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
		"  Enter / Y to start dump • N / Esc to go back\n"))
	return sb.String()
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

	width := m.width - 10
	if width < 20 {
		width = 20
	}
	if width > 60 {
		width = 60
	}

	filledW := int(float64(width) * (m.dumpProgressPct / 100.0))
	if filledW < 0 {
		filledW = 0
	}
	if filledW > width {
		filledW = width
	}
	emptyW := width - filledW

	filledStr := lipgloss.NewStyle().Foreground(colorSuccess).Render(strings.Repeat("█", filledW))
	emptyStr := lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Repeat("░", emptyW))

	sb.WriteString(fmt.Sprintf("  [%s%s] %.0f%%\n\n", filledStr, emptyStr, m.dumpProgressPct))
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
	var sb strings.Builder

	title := lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorPrimary).
		Bold(true).
		Padding(0, 2).
		Render("dbtool — Dump Result")
	sb.WriteString(title + "\n\n")

	if m.dumpErr != nil {
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true).Render("❌ Dump Failed") + "\n\n")
		sb.WriteString("  Error details:\n")
		sb.WriteString(panelStyle.Width(m.width-4).Render(m.dumpErr.Error()) + "\n\n")
	} else {
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("✓ Database Dumped Successfully!") + "\n\n")
		sb.WriteString("  Profile: " + valueStyle.Render(m.result.Profile.Name) + "\n")
		sb.WriteString("  Database: " + valueStyle.Render(m.result.Profile.Database) + "\n")
		sb.WriteString("  Output File: " + valueStyle.Render(m.result.DumpFile) + "\n\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
		"  Press any key to do another operation • Q / Esc to quit\n"))
	return sb.String()
}
