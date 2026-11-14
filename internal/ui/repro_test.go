package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
)

// Reproduces deleting a group through the real key path on a copy of the
// user's layout (skipped when no real layout is present).
func TestDeleteGroupRealLayout(t *testing.T) {
	real, _ := os.UserConfigDir()
	b, err := os.ReadFile(filepath.Join(real, "regia", "layout.json"))
	if err != nil {
		t.Skip("no real layout")
	}
	cache := claude.CachedAgents()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_ = os.MkdirAll(filepath.Dir(layout.Path()), 0o755)
	_ = os.WriteFile(layout.Path(), b, 0o644)

	m := newModel()
	m.layout.Collapsed = map[string]bool{} // unfold so the group is on screen
	_ = m.layout.Save()
	m.setAgents(cache)
	var gid string
	for id := range m.layout.Groups {
		if id[:2] == "g:" {
			gid = id
		}
	}
	if gid == "" {
		t.Skip("no user group in real layout")
	}
	m.cursorID = gid
	m.rebuild()
	if r := m.selected(); r == nil || r.ID != gid {
		t.Fatalf("cursor not on group; rows=%d selected=%+v", len(m.rows), r)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // leave the input: command keys
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.mode != modeConfirm {
		t.Fatalf("d did not ask for confirmation (mode %v, status %q)", m.mode, m.status)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if _, still := m.layout.Groups[gid]; still {
		t.Fatalf("group still in memory after d,d")
	}
	for _, r := range m.rows {
		if r.ID == gid {
			t.Fatalf("group still rendered")
		}
	}
	if l2 := layout.Load(); l2.Groups[gid] != "" {
		t.Fatalf("group still on disk")
	}
}
