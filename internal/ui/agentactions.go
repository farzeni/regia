package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

// Agents that organize the tree (regia mcp): O grants the selected agent its
// group, then the whole tree, then nothing; U lists what agents changed,
// newest first, and undoes the one you pick. A new action is announced in the
// status line as it happens.

// grantGlyph marks an agent the user let organize part of the tree.
const grantGlyph = "✦"

// grantFor is what the agent row id may organize, in words ("" when nothing).
func (m *model) grantFor(id string) string {
	switch g := m.layout.Grants[id]; g {
	case "":
		return ""
	case group.Everything:
		return "the whole tree"
	default:
		return m.titleOf(g)
	}
}

// grantGroup is the group an agent is in, for O: the nearest group row
// above it ("" at the top level).
func (m *model) grantGroup(id string) string {
	for p, n := m.layout.EffParent(id, m.agents), 0; p != "" && n < 64; p, n = m.layout.EffParent(p, m.agents), n+1 {
		if m.layout.IsGroup(p) {
			return p
		}
	}
	return ""
}

// grantKey (O) moves the selected agent's grant one step: its group, the
// whole tree, nothing.
func (m *model) grantKey(r *layout.Row) tea.Cmd {
	if r == nil || r.Agent == nil {
		m.flash("O lets an agent organize part of the tree: select an agent")
		return nil
	}
	m.syncLayout()
	id, cur, g := r.ID, m.layout.Grants[r.ID], m.grantGroup(r.ID)
	next := ""
	switch {
	case cur == "" && g != "":
		next = g
	case cur == "" || cur == g:
		next = group.Everything
	}
	if next == "" {
		delete(m.layout.Grants, id)
		m.flash("%s organizes nothing any more", m.titleOf(id))
	} else {
		m.layout.Grants[id] = next
		m.flash("%s may organize %s through regia mcp (O: %s)", m.titleOf(id), m.grantFor(id), map[bool]string{true: "whole tree", false: "none"}[next != group.Everything])
	}
	m.saveLayout()
	return nil
}

// actionLine is "14:05 orchestrator · moved 2 items into backend".
func actionLine(a group.Action) string {
	when := a.At.Format("15:04")
	if a.At.Format("2006-01-02") != time.Now().Format("2006-01-02") {
		when = a.At.Format("Jan 2 15:04")
	}
	s := when + "  " + a.Name + " · " + a.Summary
	if !a.Undone.IsZero() {
		s += "  (undone)"
	}
	return s
}

// actionsKey (U) lists the agents' latest actions; a key undoes one.
func (m *model) actionsKey() tea.Cmd {
	as := group.Actions()
	if len(as) == 0 {
		m.flash("no agent has changed the tree (agents organize it through regia mcp)")
		return nil
	}
	var items []menuItem
	for i := len(as) - 1; i >= 0 && len(items) < len(menuKeys); i-- {
		a := as[i]
		items = append(items, menuItem{string(menuKeys[len(items)]), actionLine(a), func(m *model) tea.Cmd {
			if !a.Undone.IsZero() {
				m.flash("already undone")
				return nil
			}
			if err := group.Undo(a); err != nil {
				m.flash("not undone: %v", err)
				return nil
			}
			m.syncLayout()
			m.rebuild()
			m.flash("undone: %s", a.Summary)
			return nil
		}})
	}
	m.menu, m.menuTitle, m.mode = items, "agent actions · a key undoes one", modeMenu
	return nil
}

// noticeActions announces an action an agent just made (run on refresh). The
// first look only takes note of the log.
func (m *model) noticeActions() {
	at := group.ActionsChanged()
	first := !m.actionsLooked
	m.actionsLooked = true
	if first || at.Equal(m.actionsSeen) {
		m.actionsSeen = at
		return
	}
	m.actionsSeen = at
	if as := group.Actions(); len(as) > 0 {
		a := as[len(as)-1]
		if a.Undone.IsZero() && time.Since(a.At) < time.Minute {
			m.flash("%s · U to undo", fmt.Sprintf("%s: %s", a.Name, a.Summary))
		}
	}
}
