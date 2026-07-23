package tui

import (
	"strings"
	"testing"

	"dbtool/internal/config"
	"dbtool/internal/pitr"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestPITRProfileSelectionOpensDashboard(t *testing.T) {
	cfg := &config.Config{Profiles: map[string]config.Profile{
		"source": {Name: "source", Driver: "postgres", Host: "localhost", Port: 5432, Database: "app"},
	}}
	m := NewModel(cfg, RestoreSettings{})
	m.result.Mode = ModePITR
	m.step = stepSelectProfile

	updated, _ := m.updateProfileSelector(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.step != stepPITRDashboard {
		t.Fatalf("step = %v, want PITR dashboard", got.step)
	}
	if got.result.Profile.Name != "source" {
		t.Fatalf("profile = %q, want source", got.result.Profile.Name)
	}
}

func TestPITRDashboardRendersPlanOnlyActions(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres"}
	m.step = stepPITRDashboard

	view := m.View()
	for _, marker := range []string{"pitr dashboard", "setup plan", "backup plan", "restore plan", "cleanup plan"} {
		if !strings.Contains(strings.ToLower(view), marker) {
			t.Fatalf("dashboard missing %q:\n%s", marker, view)
		}
	}
}

func TestPITRProfileSelectorUsesSourceCopy(t *testing.T) {
	if got := profileSelectorPrompt(ModePITR); got != "Choose a PostgreSQL profile for PITR" {
		t.Fatalf("profileSelectorPrompt = %q", got)
	}
	if got := profileSelectorSubtitle(ModePITR); got != "Review PITR readiness and recovery plans" {
		t.Fatalf("profileSelectorSubtitle = %q", got)
	}
}

func TestPITRDashboardDisablesPlansForNonPostgresProfile(t *testing.T) {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.result.Profile = config.Profile{Name: "source", Driver: "mysql"}
	m.step = stepPITRDashboard

	updated, _ := m.updatePITRDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	got := updated.(Model)
	if got.pitr.planTitle != "" {
		t.Fatalf("plan title = %q, want no plan for non-PostgreSQL profile", got.pitr.planTitle)
	}
}

func TestPITRDocumentsScrollWarningsAndBlockersWithinFixedFrame(t *testing.T) {
	for _, tc := range []struct {
		name       string
		step       step
		bottomText string
		prepare    func(*Model)
		offset     func(Model) int
		maxOffset  func(Model) int
	}{
		{
			name:       "dashboard notice",
			step:       stepPITRDashboard,
			bottomText: "cleanup plan",
			prepare: func(m *Model) {
				m.pitr.notice = strings.Repeat("WAL archive metadata is unavailable.\n", 12)
			},
			offset:    func(m Model) int { return m.pitr.dashboardOffset },
			maxOffset: func(m Model) int { return m.pitrDashboardMaxOffset() },
		},
		{
			name:       "recovery blockers",
			step:       stepPITRRecoveryPlan,
			bottomText: "warning-12",
			prepare: func(m *Model) {
				m.pitr.plan = pitr.RecoveryPlan{
					Blockers: []string{"blocker-01", "blocker-02", "blocker-03", "blocker-04", "blocker-05", "blocker-06", "blocker-07", "blocker-08"},
					Warnings: []string{"warning-01", "warning-02", "warning-03", "warning-04", "warning-05", "warning-06", "warning-07", "warning-08", "warning-09", "warning-10", "warning-11", "warning-12"},
				}
			},
			offset:    func(m Model) int { return m.pitr.recoveryPlanOffset },
			maxOffset: func(m Model) int { return m.pitrRecoveryPlanMaxOffset() },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
			m.step = tc.step
			m.width = 100
			m.height = 12
			m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Database: "app"}
			tc.prepare(&m)

			if got := lipgloss.Height(m.View()); got > m.height {
				t.Fatalf("top document height = %d, exceeds terminal height %d", got, m.height)
			}
			if view := m.View(); !strings.Contains(view, "PgUp/PgDn") || !strings.Contains(view, "position") {
				t.Fatalf("overflowing document must show scroll controls:\n%s", view)
			}
			if tc.maxOffset(m) == 0 {
				t.Fatal("expected document to overflow")
			}

			m = updatePITRDocumentKey(t, m, tea.KeyDown)
			if tc.offset(m) != 1 {
				t.Fatalf("down offset = %d, want 1", tc.offset(m))
			}
			m = updatePITRDocumentRune(t, m, 'j')
			m = updatePITRDocumentKey(t, m, tea.KeyPgDown)
			m = updatePITRDocumentKey(t, m, tea.KeyEnd)
			if tc.offset(m) != tc.maxOffset(m) {
				t.Fatalf("end offset = %d, want max %d", tc.offset(m), tc.maxOffset(m))
			}
			if view := m.View(); !strings.Contains(view, tc.bottomText) || !strings.Contains(view, "PITR") {
				t.Fatalf("bottom document must expose final warning content and fixed header/footer:\n%s", view)
			}

			m = updatePITRDocumentKey(t, m, tea.KeyHome)
			if tc.offset(m) != 0 {
				t.Fatalf("home offset = %d, want 0", tc.offset(m))
			}
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 200})
			m = updated.(Model)
			if tc.offset(m) != 0 || strings.Contains(m.View(), "PgUp/PgDn") {
				t.Fatalf("fitting document must clamp offset and omit overflow controls, offset=%d:\n%s", tc.offset(m), m.View())
			}
		})
	}
}

func updatePITRDocumentKey(t *testing.T, m Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated.(Model)
}

func updatePITRDocumentRune(t *testing.T, m Model, value rune) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}})
	return updated.(Model)
}
