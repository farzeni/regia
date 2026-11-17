package ui

import (
	"github.com/farzeni/regia/internal/layout"
)

// Moving: m picks up the selection (in select mode: everything marked),
// p puts it down where the cursor is. p puts it where you'd expect: on a
// group, inside it (at the top); on an agent, right after it. P does the
// other one: after a group, or inside an agent (under it, as its coordinator).

func (m *model) pickUp(ids []string, what string) {
	m.cut = ids
	m.flash("moving %s — p: into a group / after an agent · P: after a group / under an agent · esc cancels", what)
}

func (m *model) putDown(r *layout.Row, other bool) {
	inside := m.layout.IsGroup(r.ID) != other
	var err error
	if inside {
		err = m.layout.Put(m.cut, r.ID, "", m.agents)
	} else {
		err = m.layout.Put(m.cut, m.layout.EffParent(r.ID, m.agents), r.ID, m.agents)
	}
	if err != nil {
		m.flash("%v", err)
		return
	}
	what := m.titleOf(m.cut[0])
	if len(m.cut) > 1 {
		what = count(len(m.cut), "item")
	}
	if inside {
		m.flash("moved %s into %s", what, r.Title)
	} else {
		m.flash("moved %s after %s", what, r.Title)
	}
	m.cursorID, m.cut = m.cut[0], nil
	m.saveLayout()
}

// moveMarked is m in select mode.
func (m *model) moveMarked() {
	ids := m.layout.Topmost(m.markedIDs())
	if len(ids) == 0 {
		m.flash("nothing marked: space marks a row (a group: all of it), * marks all")
		return
	}
	m.selecting, m.marked = false, nil
	m.pickUp(ids, count(len(ids), "agent"))
}

// groupMarked is n in select mode: a new group holding what's marked, where
// the first of it was.
func (m *model) groupMarked() {
	ids := m.layout.Topmost(m.markedIDs())
	if len(ids) == 0 {
		m.flash("nothing marked: space marks a row (a group: all of it), * marks all")
		return
	}
	m.newGroupInPlace(func() (string, error) { return m.layout.GroupOf("", ids, m.agents) }, func(m *model, gid, title string) {
		m.selecting, m.marked = false, nil
		m.flash("grouped %s in %s", count(len(ids), "agent"), title)
	})
}
