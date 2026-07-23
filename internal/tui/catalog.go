package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/driver"

	tea "github.com/charmbracelet/bubbletea"
)

type catalogSchemasLoadedMsg struct {
	schemas []string
	err     error
}

type catalogTablesLoadedMsg struct {
	tables []driver.CatalogTable
	err    error
}

func (m *Model) openCatalogSelector() tea.Cmd {
	m.catalogReturnStep = m.step
	m.catalogLoading = false
	drv, err := driver.Get(m.result.Profile.Driver)
	if err != nil {
		m.catalogErr = err.Error()
		m.step = stepSelectCatalog
		return nil
	}
	catalog, ok := drv.(driver.CatalogLister)
	if !ok {
		m.catalogErr = fmt.Sprintf("%s does not support object browsing; use text patterns instead", drv.Name())
		m.step = stepSelectCatalog
		return nil
	}

	m.catalogTab = 0
	m.catalogIdx = 0
	m.catalogErr = ""
	m.catalogLoading = true
	m.catalogSearching = false
	m.catalogSearch.SetValue("")
	m.catalogSearch.Blur()
	m.catalogSchemas = nil
	m.catalogTables = nil
	m.catalogExclude = len(m.getIncludeSchema()) == 0 && len(m.getIncludeTable()) == 0 && (len(m.getExcludeSchema()) > 0 || len(m.getExcludeTable()) > 0)
	m.syncCatalogSelectionMode()
	m.step = stepSelectCatalog

	profile := m.result.Profile
	return loadCatalogSchemas(catalog, profile)
}

func loadCatalogSchemas(catalog driver.CatalogLister, profile config.Profile) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		schemas, err := catalog.ListSchemas(ctx, profile)
		return catalogSchemasLoadedMsg{schemas: schemas, err: err}
	}
}

func loadCatalogTables(catalog driver.CatalogLister, profile config.Profile, schemas []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tables, err := catalog.ListTables(ctx, profile, schemas)
		return catalogTablesLoadedMsg{tables: tables, err: err}
	}
}

func (m Model) updateCatalogSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.catalogSearching {
		switch msg.String() {
		case "esc":
			m.catalogSearching = false
			m.catalogSearch.Blur()
			m.catalogSearch.SetValue("")
			m.catalogIdx = 0
			return m, nil
		case "up", "k", "down", "j", "enter", " ":
		default:
			var cmd tea.Cmd
			m.catalogSearch, cmd = m.catalogSearch.Update(msg)
			m.catalogIdx = 0
			return m, cmd
		}
	}
	if m.catalogLoading {
		if msg.String() == "esc" {
			m.step = m.catalogReturnStep
		}
		return m, nil
	}

	visibleCount := len(m.visibleCatalogItems())
	if visibleCount == 0 {
		m.catalogIdx = 0
	} else if m.catalogIdx >= visibleCount {
		m.catalogIdx = visibleCount - 1
	}

	switch msg.String() {
	case "up", "k":
		if m.catalogIdx > 0 {
			m.catalogIdx--
		}
	case "down", "j":
		if m.catalogIdx < visibleCount-1 {
			m.catalogIdx++
		}
	case "/":
		m.catalogSearching = true
		m.catalogSearch.Focus()
		return m, nil
	case "tab":
		if m.catalogTab == 0 {
			m.catalogTab = 1
			m.catalogIdx = 0
			m.catalogLoading = true
			m.catalogErr = ""
			drv, err := driver.Get(m.result.Profile.Driver)
			if err != nil {
				m.catalogLoading = false
				m.catalogErr = err.Error()
				return m, nil
			}
			catalog, ok := drv.(driver.CatalogLister)
			if !ok {
				m.catalogLoading = false
				m.catalogErr = "Object browsing is unavailable for this driver"
				return m, nil
			}
			return m, loadCatalogTables(catalog, m.result.Profile, m.selectedSchemas())
		}
		m.catalogTab = 0
		m.catalogIdx = 0
	case "x", "X":
		m.catalogExclude = !m.catalogExclude
		m.syncCatalogSelectionMode()
		m.catalogIdx = 0
	case " ":
		m.toggleCatalogSelection()
	case "enter":
		if m.catalogTab == 0 {
			m.applyCatalogSchemas()
		} else {
			m.applyCatalogTables()
		}
		m.step = m.catalogReturnStep
	case "esc":
		m.step = m.catalogReturnStep
	}
	return m, nil
}

func (m *Model) toggleCatalogSelection() {
	if m.catalogTab == 0 {
		items := m.visibleCatalogSchemas()
		if m.catalogIdx < len(items) {
			m.catalogSchemaSelected[items[m.catalogIdx]] = !m.catalogSchemaSelected[items[m.catalogIdx]]
		}
		return
	}
	items := m.visibleCatalogTables()
	if m.catalogIdx < len(items) {
		key := qualifiedTableName(items[m.catalogIdx])
		m.catalogTableSelected[key] = !m.catalogTableSelected[key]
	}
}

func (m Model) visibleCatalogItems() []string {
	if m.catalogTab == 0 {
		return m.visibleCatalogSchemas()
	}
	items := m.visibleCatalogTables()
	result := make([]string, len(items))
	for i, table := range items {
		result[i] = qualifiedTableName(table)
	}
	return result
}

func (m Model) visibleCatalogSchemas() []string {
	query := strings.ToLower(m.catalogSearch.Value())
	var schemas []string
	for _, schema := range m.catalogSchemas {
		if query == "" || strings.Contains(strings.ToLower(schema), query) {
			schemas = append(schemas, schema)
		}
	}
	return schemas
}

func (m Model) visibleCatalogTables() []driver.CatalogTable {
	query := strings.ToLower(m.catalogSearch.Value())
	var tables []driver.CatalogTable
	for _, table := range m.catalogTables {
		if query == "" || strings.Contains(strings.ToLower(qualifiedTableName(table)), query) {
			tables = append(tables, table)
		}
	}
	return tables
}

func (m Model) selectedSchemas() []string {
	return selectedMapValues(m.catalogSchemaSelected)
}

func (m *Model) applyCatalogSchemas() {
	if m.catalogExclude {
		m.setExcludeSchema(m.selectedSchemas())
		m.setExcludeTable(nil)
		m.setIncludeSchema(nil)
		return
	}
	m.setIncludeSchema(m.selectedSchemas())
	m.setExcludeSchema(nil)
	m.setIncludeTable(nil)
}

func (m *Model) applyCatalogTables() {
	values := selectedMapValues(m.catalogTableSelected)
	if m.catalogExclude {
		m.setExcludeTable(values)
		m.setExcludeSchema(nil)
		m.setIncludeTable(nil)
		return
	}
	m.setIncludeTable(values)
	m.setExcludeTable(nil)
	m.setIncludeSchema(nil)
}

func (m *Model) syncCatalogSelectionMode() {
	if m.catalogExclude {
		m.catalogSchemaSelected = selectedValues(m.getExcludeSchema())
		m.catalogTableSelected = selectedValues(m.getExcludeTable())
		return
	}
	m.catalogSchemaSelected = selectedValues(m.getIncludeSchema())
	m.catalogTableSelected = selectedValues(m.getIncludeTable())
}

func selectedValues(values []string) map[string]bool {
	selected := make(map[string]bool, len(values))
	for _, value := range values {
		selected[value] = true
	}
	return selected
}

func selectedMapValues(selected map[string]bool) []string {
	var values []string
	for value, enabled := range selected {
		if enabled {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	return values
}

func qualifiedTableName(table driver.CatalogTable) string {
	return table.Schema + "." + table.Name
}

func (m Model) viewCatalogSelector() string {
	tabName := "Schemas"
	if m.catalogTab == 1 {
		tabName = "Tables"
	}

	var body strings.Builder
	body.WriteString(renderProfileSummaryCard(m.width, "Source Profile", m.result.Profile.Name, m.result.Profile.Driver, m.result.Profile.Host, m.result.Profile.Port, m.result.Profile.Database, m.result.Profile.User))
	body.WriteString("\n\n")
	if m.catalogErr != "" {
		body.WriteString(renderCard(m.width, "Object Browser Unavailable", errorStyle.Render(m.catalogErr)))
		return renderScreenFrame(m.width, "Select Database Objects", "Use T/H for text patterns when catalog browsing is unavailable", body.String(), []keyHint{{Key: "Esc", Label: "back", Danger: true}})
	}
	if m.catalogLoading {
		body.WriteString(renderCard(m.width, "Loading Catalog", mutedStyle.Render("Fetching objects from PostgreSQL...")))
		return renderScreenFrame(m.width, "Select Database Objects", "Loading database catalog", body.String(), []keyHint{{Key: "Esc", Label: "cancel", Danger: true}})
	}

	if m.catalogSearching {
		body.WriteString(renderCard(m.width, "Search", m.catalogSearch.View()))
		body.WriteString("\n\n")
	}

	items := m.visibleCatalogItems()
	if len(items) == 0 {
		message := "No objects found"
		if m.catalogTab == 1 && len(m.catalogSchemas) == 0 {
			message = "No tables found in the selected schemas"
		}
		body.WriteString(renderCard(m.width, "No Results", mutedStyle.Render(message)))
	} else {
		visible := m.height - 13
		if m.catalogSearching {
			visible--
		}
		if visible < 3 {
			visible = 3
		}
		start := 0
		if m.catalogIdx >= visible {
			start = m.catalogIdx - visible + 1
		}
		end := start + visible
		if end > len(items) {
			end = len(items)
		}

		var rows strings.Builder
		for i := start; i < end; i++ {
			if i > start {
				rows.WriteByte('\n')
			}
			selected := false
			if m.catalogTab == 0 {
				selected = m.catalogSchemaSelected[items[i]]
			} else {
				selected = m.catalogTableSelected[items[i]]
			}
			check := "☐"
			if selected {
				check = "☑"
			}
			rows.WriteString(renderRow(i == m.catalogIdx, check+" "+items[i]))
		}
		body.WriteString(renderCard(m.width, tabName, rows.String()))
		if len(items) > visible {
			body.WriteString("\n\n")
			body.WriteString(mutedStyle.Render(fmt.Sprintf("Showing %d-%d of %d items", start+1, end, len(items))))
		}
	}

	mode := "include"
	if m.catalogExclude {
		mode = "exclude"
	}
	subtitle := "Select schemas to " + mode + "; Tab loads tables in selected schemas"
	if m.catalogTab == 1 {
		subtitle = "Select tables to " + mode + "; table selection replaces schema selection"
	}
	return renderScreenFrame(m.width, "Select Database Objects", subtitle, body.String(), []keyHint{
		{Key: "↑/↓", Label: "navigate"},
		{Key: "Space", Label: "toggle"},
		{Key: "Tab", Label: "schemas/tables"},
		{Key: "X", Label: "include/exclude"},
		{Key: "/", Label: "search"},
		{Key: "Enter", Label: "apply"},
		{Key: "Esc", Label: "back", Danger: true},
	})
}
