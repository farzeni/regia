package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/layout"
)

// Pins keep sessions in sight: a pinned agent stays in the tree when it's
// done, like one still working, until you unpin it (or archive it). `f` pins
// or unpins the selected agent, or every agent of a group; in select mode,
// the marked ones.

const pinGlyph = "⚑"

func (m *model) pinKey(r *layout.Row) tea.Cmd {
	if r == nil {
		return nil
	}
	ids := []string{r.ID}
	if r.Agent == nil { // a group: all its agents as shown
		ids = m.under(r.ID)
	}
	m.togglePins(ids)
	return nil
}

func (m *model) pinMarked() {
	ids := m.markedIDs()
	if len(ids) == 0 {
		m.flash("nothing marked: space marks a row (a group: all of it), * marks all")
		return
	}
	m.togglePins(ids)
	m.selecting, m.marked = false, nil
}

// togglePins unpins ids when they're all pinned, else pins them all.
func (m *model) togglePins(ids []string) {
	if len(ids) == 0 {
		m.flash("no agents to pin here")
		return
	}
	all := true
	for _, id := range ids {
		all = all && m.layout.Pins[id]
	}
	m.layout.SetPins(!all, ids...)
	what := m.titleOf(ids[0])
	if len(ids) > 1 {
		what = count(len(ids), "agent")
	}
	if all {
		m.flash("unpinned %s: hidden again once done and seen (a shows finished)", what)
	} else {
		m.flash("pinned %s: stays in the list even when done · f again unpins", what)
	}
	m.saveLayout()
}
