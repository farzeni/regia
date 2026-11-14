package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

// Group awareness in the UI: the shared notes of a hand-made group or a
// coordinator's agents show in the panel, N adds one from you, M mutes a
// group or coordinator (its agents get nothing from regia about the group).

func (m *model) groupTree() *group.Tree {
	return &group.Tree{L: m.layout, Spawns: m.layout.Spawns}
}

// isContainer says whether a row can hold notes: a hand-made group, or an
// agent with agents under it.
func (m *model) isContainer(r *layout.Row) bool {
	if r == nil {
		return false
	}
	if r.Group {
		return group.IsGroup(r.ID)
	}
	return r.HasKids
}

// noteTarget is where a note from you goes for the selected row: the row
// itself when it holds notes, else the selected agent's nearest group.
func (m *model) noteTarget(r *layout.Row) string {
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

func (m *model) addNoteKey(r *layout.Row) tea.Cmd {
	target := m.noteTarget(r)
	if target == "" {
		m.flash("notes belong to a hand-made group (n) or a coordinator's agents")
		return nil
	}
	title := m.titleOf(target)
	m.prompt("note for "+title+": ", "", func(m *model, s string) tea.Cmd {
		if err := group.AddNote(target, group.Note{By: group.FromUser, Name: "you", Text: s}); err != nil {
			m.flash("note: %v", err)
			return nil
		}
		m.flash("note shared with %s (%d agents)", title, m.groupTree().Members(target))
		return nil
	})
	return nil
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

// notesSection is the panel's "shared notes" block (a blank line, a heading,
// the notes), or a hint on a group without notes.
func (m *model) notesSection(container string, w, last int) []string {
	head := "shared notes  (N adds · M mutes)"
	if m.layout.Muted[container] {
		head = "shared notes  (muted: M unmutes)"
	}
	ns := m.notesLines(container, w, last)
	if len(ns) == 0 {
		if !group.IsGroup(container) {
			return nil
		}
		ns = []string{stDim.Render("none yet — agents write them with regia note")}
	}
	return append([]string{"", stHead.Render(head)}, ns...)
}

// notesLines renders a container's latest notes for the panel.
func (m *model) notesLines(container string, w, last int) []string {
	notes := group.ReadNotes(container)
	if len(notes) == 0 {
		return nil
	}
	var out []string
	if len(notes) > last {
		out = append(out, stDim.Render(fmt.Sprintf("+%d older", len(notes)-last)))
		notes = notes[len(notes)-last:]
	}
	now := time.Now()
	for _, n := range notes {
		when := n.At.Format("15:04")
		if n.At.Format("2006-01-02") != now.Format("2006-01-02") {
			when = n.At.Format("Jan 2")
		}
		pre := stDim.Render(when+" ") + stAccent.Render(n.Name) + stDim.Render(": ")
		out = append(out, pre+trunc(n.Text, w-len(when)-len(n.Name)-3))
	}
	return out
}
