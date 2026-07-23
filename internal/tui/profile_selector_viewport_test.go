package tui

import (
	"fmt"
	"strings"
	"testing"

	"dbtool/internal/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestProfileSelectorViewportNavigationKeepsSelectionVisible(t *testing.T) {
	m := selectorViewportModel(stepSelectProfile)
	m.height = 12

	assertSelectorFrame(t, m.viewProfileSelector(), m.height)
	assertContains(t, m.viewProfileSelector(), "profile-00")
	assertNotContains(t, m.viewProfileSelector(), "profile-11")

	m = updateProfileSelectorKey(t, m, tea.KeyDown)
	if m.profileIdx != 1 {
		t.Fatalf("down selected index = %d, want 1", m.profileIdx)
	}
	assertContains(t, m.viewProfileSelector(), m.profiles[m.profileIdx].Name)

	m = updateProfileSelectorKey(t, m, tea.KeyPgDown)
	if m.profileIdx <= 1 {
		t.Fatalf("pgdown selected index = %d, want progress", m.profileIdx)
	}
	assertContains(t, m.viewProfileSelector(), m.profiles[m.profileIdx].Name)

	m = updateProfileSelectorKey(t, m, tea.KeyEnd)
	if m.profileIdx != len(m.profiles)-1 {
		t.Fatalf("end selected index = %d, want %d", m.profileIdx, len(m.profiles)-1)
	}
	assertContains(t, m.viewProfileSelector(), "profile-11")
	assertSelectorFrame(t, m.viewProfileSelector(), m.height)

	m = updateProfileSelectorKey(t, m, tea.KeyHome)
	if m.profileIdx != 0 {
		t.Fatalf("home selected index = %d, want 0", m.profileIdx)
	}
	assertContains(t, m.viewProfileSelector(), "profile-00")

	m = updateProfileSelectorKey(t, m, tea.KeyUp)
	if m.profileIdx != 0 {
		t.Fatalf("up at first index = %d, want 0", m.profileIdx)
	}

	resized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 200})
	m = resized.(Model)
	if m.profileViewportOffset != 0 {
		t.Fatalf("resize offset = %d, want 0 when all profiles fit", m.profileViewportOffset)
	}
	assertNotContains(t, m.viewProfileSelector(), "Showing ")
}

func TestMigrateDestinationSelectorViewportNavigationKeepsSelectionVisible(t *testing.T) {
	m := selectorViewportModel(stepMigrateSelectDest)
	m.height = 14
	m.result.Profile = m.profiles[0]

	assertSelectorFrame(t, m.viewMigrateDestSelector(), m.height)
	m = updateMigrateDestSelectorKey(t, m, tea.KeyDown)
	if m.migrateDestIdx != 1 {
		t.Fatalf("down selected index = %d, want 1", m.migrateDestIdx)
	}
	assertContains(t, m.viewMigrateDestSelector(), m.profiles[m.migrateDestIdx].Name)

	m = updateMigrateDestSelectorKey(t, m, tea.KeyPgDown)
	if m.migrateDestIdx <= 1 {
		t.Fatalf("pgdown selected index = %d, want progress", m.migrateDestIdx)
	}
	assertContains(t, m.viewMigrateDestSelector(), m.profiles[m.migrateDestIdx].Name)

	m = updateMigrateDestSelectorKey(t, m, tea.KeyEnd)
	if m.migrateDestIdx != len(m.profiles)-1 {
		t.Fatalf("end selected index = %d, want %d", m.migrateDestIdx, len(m.profiles)-1)
	}
	assertContains(t, m.viewMigrateDestSelector(), "profile-11")
	assertSelectorFrame(t, m.viewMigrateDestSelector(), m.height)

	m = updateMigrateDestSelectorKey(t, m, tea.KeyHome)
	if m.migrateDestIdx != 0 {
		t.Fatalf("home selected index = %d, want 0", m.migrateDestIdx)
	}
	assertContains(t, m.viewMigrateDestSelector(), "profile-00")

	m = updateMigrateDestSelectorKey(t, m, tea.KeyUp)
	if m.migrateDestIdx != 0 {
		t.Fatalf("up at first index = %d, want 0", m.migrateDestIdx)
	}

	resized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 200})
	m = resized.(Model)
	if m.migrateDestViewportOffset != 0 {
		t.Fatalf("resize offset = %d, want 0 when all profiles fit", m.migrateDestViewportOffset)
	}
	assertNotContains(t, m.viewMigrateDestSelector(), "Showing ")
}

func selectorViewportModel(screen step) Model {
	profiles := make([]config.Profile, 12)
	for i := range profiles {
		profiles[i] = config.Profile{
			Name:     fmt.Sprintf("profile-%02d", i),
			Driver:   "postgres",
			Host:     "localhost",
			Port:     5432 + i,
			Database: fmt.Sprintf("database-%02d", i),
		}
	}
	return Model{step: screen, width: 80, height: 24, profiles: profiles}
}

func updateProfileSelectorKey(t *testing.T, m Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := m.updateProfileSelector(tea.KeyMsg{Type: key})
	return updated.(Model)
}

func updateMigrateDestSelectorKey(t *testing.T, m Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := m.updateMigrateDestSelector(tea.KeyMsg{Type: key})
	return updated.(Model)
}

func assertSelectorFrame(t *testing.T, view string, height int) {
	t.Helper()
	if got := lipgloss.Height(view); got > height {
		t.Fatalf("rendered height = %d, exceeds terminal height %d:\n%s", got, height, view)
	}
}

func assertContains(t *testing.T, view, want string) {
	t.Helper()
	if !strings.Contains(view, want) {
		t.Fatalf("view missing %q:\n%s", want, view)
	}
}

func assertNotContains(t *testing.T, view, want string) {
	t.Helper()
	if strings.Contains(view, want) {
		t.Fatalf("view unexpectedly contains %q:\n%s", want, view)
	}
}
