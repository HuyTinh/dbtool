package tui

import (
	"strings"
	"testing"

	"dbtool/internal/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestConfirmationReviewsStayBoundedAndExposeSafetyDetailsAtDocumentBounds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		step       step
		bottomText string
	}{
		{name: "restore", step: stepConfirm, bottomText: "may drop database objects"},
		{name: "dump", step: stepDumpConfirm, bottomText: "Dump Options"},
		{name: "migrate", step: stepMigrateConfirm, bottomText: "may drop target database objects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := overflowingConfirmationModel(tc.step, 12)
			if got := lipgloss.Height(m.View()); got > m.height {
				t.Fatalf("top review height = %d, exceeds terminal height %d", got, m.height)
			}
			if view := m.View(); !strings.Contains(view, "position") || !strings.Contains(view, "PgUp/PgDn") {
				t.Fatalf("overflowing review must show position and scroll controls:\n%s", view)
			}

			m = updateConfirmationKey(t, m, tea.KeyDown)
			if got := m.confirmationReviewOffset(reviewForStep(tc.step)); got != 1 {
				t.Fatalf("down offset = %d, want 1", got)
			}
			m = updateConfirmationRunes(t, m, 'k')
			if got := m.confirmationReviewOffset(reviewForStep(tc.step)); got != 0 {
				t.Fatalf("k offset = %d, want 0", got)
			}
			m = updateConfirmationRunes(t, m, 'j')
			if got := m.confirmationReviewOffset(reviewForStep(tc.step)); got != 1 {
				t.Fatalf("j offset = %d, want 1", got)
			}
			m = updateConfirmationKey(t, m, tea.KeyUp)

			m = updateConfirmationKey(t, m, tea.KeyEnd)
			if got := lipgloss.Height(m.View()); got > m.height {
				t.Fatalf("bottom review height = %d, exceeds terminal height %d", got, m.height)
			}
			if view := m.View(); !strings.Contains(view, tc.bottomText) || !strings.Contains(view, "Enter") {
				t.Fatalf("bottom review must expose final details and fixed action footer:\n%s", view)
			}

			m = updateConfirmationKey(t, m, tea.KeyHome)
			if view := m.View(); !strings.Contains(view, "Source Profile") && !strings.Contains(view, "Target Profile") {
				t.Fatalf("home must return to the review start:\n%s", view)
			}

			m = updateConfirmationKey(t, m, tea.KeyPgDown)
			m = updateConfirmationKey(t, m, tea.KeyPgUp)
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 200})
			m = updated.(Model)
			if view := m.View(); strings.Contains(view, "position") || strings.Contains(view, "PgUp/PgDn") {
				t.Fatalf("fitting review must omit scroll controls:\n%s", view)
			}
		})
	}
}

func TestConfirmationEnterKeepsExistingExplicitProceedRoute(t *testing.T) {
	for _, tc := range []struct {
		name      string
		step      step
		result    step
		confirmed func(Model) bool
	}{
		{name: "restore", step: stepConfirm, result: stepRestoreResult, confirmed: func(m Model) bool { return m.result.Confirm }},
		{name: "dump", step: stepDumpConfirm, result: stepDumpResult, confirmed: func(m Model) bool { return m.result.DumpConfirm }},
		{name: "migrate", step: stepMigrateConfirm, result: stepMigrateResult, confirmed: func(m Model) bool { return m.result.MigrateConfirm }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := overflowingConfirmationModel(tc.step, 12)
			m.result.Profile.Driver = "missing-driver"
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = updated.(Model)
			if !tc.confirmed(m) || m.step != tc.result {
				t.Fatalf("enter route = confirmed:%v step:%v, want confirmed and step:%v", tc.confirmed(m), m.step, tc.result)
			}
		})
	}
}

func updateConfirmationKey(t *testing.T, m Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated.(Model)
}

func updateConfirmationRunes(t *testing.T, m Model, runeValue rune) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{runeValue}})
	return updated.(Model)
}

func reviewForStep(screen step) confirmationReview {
	switch screen {
	case stepDumpConfirm:
		return dumpConfirmationReview
	case stepMigrateConfirm:
		return migrateConfirmationReview
	default:
		return restoreConfirmationReview
	}
}

func overflowingConfirmationModel(screen step, height int) Model {
	m := NewModel(&config.Config{Profiles: map[string]config.Profile{}}, RestoreSettings{})
	m.step = screen
	m.width = 100
	m.height = height
	m.result.Profile = config.Profile{Name: "source", Driver: "postgres", Host: "source.example", Port: 5432, Database: "source_db", User: "source_user"}
	m.result.DestProfile = config.Profile{Name: "target", Driver: "postgres", Host: "target.example", Port: 5433, Database: "target_db", User: "target_user"}
	filters := []string{"public.accounts", "public.invoices", "public.payments", "public.audit_events", "public.notifications", "public.search_documents"}
	m.result.File = "backup.dump"
	m.result.DumpFile = "backup.dump"
	m.result.Settings = RestoreSettings{Format: "custom", Jobs: 4, Clean: true, IncludeTable: filters, ExcludeTable: filters}
	m.result.DumpSettings = DumpSettings{Format: "custom", IncludeTable: filters, ExcludeTable: filters}
	m.result.MigrateSettings = MigrateSettings{Format: "custom", Jobs: 4, Clean: true, IncludeTable: filters, ExcludeTable: filters}
	return m
}
