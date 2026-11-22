package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

// Group awareness in the UI: M mutes a group or coordinator (its agents get
// nothing from regia about the group).

func (m *model) groupTree() *group.Tree {
	return &group.Tree{L: m.layout, Spawns: m.layout.Spawns}
}

// isContainer says whether a row is a group agents are aware of: a hand-made
// group, or an agent with agents under it.
func (m *model) isContainer(r *layout.Row) bool {
	if r == nil {
		return false
	}
	if r.Group {
		return group.IsGroup(r.ID)
	}
	return r.HasKids
}

// groupTarget is the group for the selected row: the row itself when it is
// one, else the selected agent's nearest group.
func (m *model) groupTarget(r *layout.Row) string {
	if r == nil {
		return ""
	}
	if m.isContainer(r) {
		return r.ID
	}
	if r.Agent != nil {
		return m.groupTree().Resolve(r.ID).Nearest()
	}
	return ""
}

func (m *model) muteKey(r *layout.Row) tea.Cmd {
	if !m.isContainer(r) {
		m.flash("M mutes a hand-made group or a coordinator")
		return nil
	}
	m.syncLayout()
	if m.layout.Muted == nil {
		m.layout.Muted = map[string]bool{}
	}
	if m.layout.Muted[r.ID] {
		delete(m.layout.Muted, r.ID)
		m.flash("%s: its agents see the group again", r.Title)
	} else {
		m.layout.Muted[r.ID] = true
		m.flash("muted %s: its agents get nothing from regia about the group (M again: unmute)", r.Title)
	}
	m.saveLayout()
	return nil
}
