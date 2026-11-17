package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

// Group awareness in the UI: the blackboard of a hand-made group or a
// coordinator's agents shows in the panel; N opens it to add, edit or delete
// notes; M mutes a group or coordinator (its agents get nothing from regia
// about the group).

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

// noteTarget is the blackboard for the selected row: the row itself when it
// holds notes, else the selected agent's nearest group.
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

// notesKey (N) opens the blackboard of the selected group or coordinator (or
// of the selected agent's nearest one): ↑/↓ pick a note, n adds, e edits,
// x deletes, esc leaves. An empty blackboard asks for the first note.
func (m *model) notesKey(r *layout.Row) tea.Cmd {
	target := m.noteTarget(r)
	if target == "" {
		m.flash("notes belong to a hand-made group (n) or a coordinator's agents")
		return nil
	}
	if m.visRow[target] { // select it: its panel is where the notes are drawn
		m.cursorID = target
		m.rebuild()
	}
	m.notesFor = target
	m.timeline = false // the blackboard is what's drawn
	m.noteSel = len(group.ReadNotes(target)) - 1
	m.flash("blackboard of %s open · esc closes it", m.titleOf(target))
	if m.noteSel < 0 {
		m.addNote()
	}
	return nil
}

// updateNotes handles keys while a blackboard is open; a key it doesn't use
// closes the blackboard and then works as usual.
func (m *model) updateNotes(k tea.KeyMsg) (tea.Cmd, bool) {
	if m.cursorID != m.notesFor { // the selection moved (a click): the blackboard goes
		m.notesFor = ""
		return nil, false
	}
	notes := group.ReadNotes(m.notesFor)
	m.noteSel = max(0, min(m.noteSel, len(notes)-1))
	switch k.String() {
	case "esc", "q", "N":
		m.notesFor = ""
		m.flash("")
	case "j", "down":
		m.noteSel = max(0, min(m.noteSel+1, len(notes)-1))
	case "k", "up":
		m.noteSel = max(m.noteSel-1, 0)
	case "n", "a":
		m.addNote()
	case "e", "enter":
		if len(notes) == 0 {
			m.addNote()
			break
		}
		n, c := notes[m.noteSel], m.notesFor
		m.editNote(c, m.noteSel, n.Text, func(m *model, s string) tea.Cmd {
			if err := group.EditNote(c, n, s); err != nil {
				m.flash("not saved: %v", err)
			} else {
				m.flash("note edited: the agents of %s are told", m.titleOf(c))
			}
			return nil
		})
	case "x", "delete":
		if len(notes) == 0 {
			break
		}
		n, c := notes[m.noteSel], m.notesFor
		m.confirm("x", fmt.Sprintf("delete %s's note %q?", n.Name, trunc(n.Text, 40)), func(m *model) tea.Cmd {
			if err := group.DeleteNote(c, n); err != nil {
				m.flash("not deleted: %v", err)
			} else {
				m.flash("note deleted")
			}
			return nil
		})
	default:
		m.notesFor = ""
		return nil, false
	}
	return nil, true
}

// addNote asks for a note from you on the open blackboard.
func (m *model) addNote() {
	target := m.notesFor
	title := m.titleOf(target)
	m.editNote(target, -1, "", func(m *model, s string) tea.Cmd {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		if err := group.AddNote(target, group.Note{By: group.FromUser, Name: "you", Text: s}); err != nil {
			m.flash("note: %v", err)
			return nil
		}
		m.noteSel = len(group.ReadNotes(target)) - 1
		m.flash("note shared with %s (%d agents)", title, m.groupTree().Members(target))
		return nil
	})
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

// notesSection is the panel's blackboard block (a blank line, a heading, the
// notes), or a hint on a group without notes. With the blackboard open (N)
// it shows more notes and the one picked.
func (m *model) notesSection(container string, w, last int) []string {
	open := m.notesFor == container
	head := "blackboard  (N opens · M mutes)"
	switch {
	case open:
		head = "blackboard  ↑↓ · n add · e edit · x delete · esc"
		last = max(last, 12)
	case m.layout.Muted[container]:
		head = "blackboard  (muted: M unmutes)"
	}
	ns := m.notesLines(container, w, last, open)
	if len(ns) == 0 {
		if !group.IsGroup(container) && !open {
			return nil
		}
		ns = []string{stDim.Render("empty — agents write with regia note, N adds yours")}
	}
	return append([]string{"", stHead.Render(head)}, ns...)
}

// notesLines renders a container's notes for the panel: the latest, or with
// the blackboard open, a window that keeps the picked note in view.
func (m *model) notesLines(container string, w, last int, open bool) []string {
	notes, editing := m.noteForEdit(container, group.ReadNotes(container))
	if len(notes) == 0 {
		return nil
	}
	sel := -1
	start := max(0, len(notes)-last)
	if open {
		sel = max(0, min(m.noteSel, len(notes)-1))
		if editing >= 0 {
			sel = editing
		}
		start = min(start, sel)
	}
	end := min(len(notes), start+last)
	var out []string
	if start > 0 {
		out = append(out, stDim.Render(fmt.Sprintf("+%d older", start)))
	}
	now := time.Now()
	for i := start; i < end; i++ {
		n := notes[i]
		when := n.At.Format("15:04")
		if n.At.Format("2006-01-02") != now.Format("2006-01-02") {
			when = n.At.Format("Jan 2")
		}
		text := n.Text
		if !n.Edited.IsZero() && i != editing {
			text += " (edited)"
		}
		mark, room := "", w-len(when)-len(n.Name)-3
		if open {
			mark, room = "  ", room-2
			if i == sel {
				mark = "› "
			}
		}
		line := mark + stDim.Render(when+" ") + stAccent.Render(n.Name) + stDim.Render(": ")
		if i == editing {
			out = append(out, line+m.edit.view(room))
			continue
		}
		line += trunc(text, room)
		if i == sel {
			line = stSel.Render(line)
		}
		out = append(out, line)
	}
	if end < len(notes) {
		out = append(out, stDim.Render(fmt.Sprintf("+%d newer", len(notes)-end)))
	}
	return out
}
