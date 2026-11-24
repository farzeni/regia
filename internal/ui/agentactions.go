package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

// Agents that organize the tree (regia mcp): an agent claims its group or
// the whole tree itself, with a reason (claim), or you grant it with O: its
// group, then the whole tree, then nothing (O on a claim takes it back).
// Either way the agent is marked ✦ and the agent box says what and why. A
// claim ends when its agent is done (releaseDone). U lists what agents
// changed, newest first, and undoes the one you pick. A new action is
// announced in the status line as it happens.

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
	case m.layout.Claims[id] != "": // its own claim: taken back
	case cur == "" && g != "":
		next = g
	case cur == "" || cur == g:
		next = group.Everything
	}
	delete(m.layout.Claims, id)
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

// releaseDone ends the claims of agents that are done or failed: a claim is
// for the work at hand (run on refresh).
func (m *model) releaseDone() {
	var done []string
	for id := range m.layout.Claims {
		if a := m.agents[id]; a != nil && (a.State == "done" || a.State == "failed") {
			done = append(done, id)
		}
	}
	if len(done) == 0 {
		return
	}
	// only what finished after its claim: a state file lagging behind the
	// turn that claimed doesn't end it
	claimed := map[string]time.Time{}
	for _, ac := range group.Actions() {
		if ac.Tool == "claim" {
			claimed[ac.By] = ac.At
		}
	}
	var gone []string
	for _, id := range done {
		if at := m.agents[id].UpdatedAt; at.IsZero() || at.After(claimed[id]) {
			gone = append(gone, id)
		}
	}
	if len(gone) == 0 {
		return
	}
	m.syncLayout()
	for _, id := range gone {
		delete(m.layout.Claims, id)
		delete(m.layout.Grants, id)
	}
	m.saveLayoutQuiet()
	if len(gone) == 1 {
		m.flash("%s is done: its claim on the tree ended", m.titleOf(gone[0]))
	}
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
	if a.Final {
		s += "  (can't be undone)"
	}
	return s
}

// actionsKey (U) opens the list of the agents' actions, newest first.
func (m *model) actionsKey() tea.Cmd {
	as := group.Actions()
	if len(as) == 0 {
		m.flash("no agent has changed the tree (agents organize it through regia mcp)")
		return nil
	}
	slices.Reverse(as)
	m.acts, m.actSel, m.mode = as, 0, modeActions
	return nil
}

// updateActions: j/k move, enter undoes the selected action, U, q or esc close.
func (m *model) updateActions(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		m.actSel = min(m.actSel+1, len(m.acts)-1)
	case "k", "up":
		m.actSel = max(m.actSel-1, 0)
	case "g", "home":
		m.actSel = 0
	case "G", "end":
		m.actSel = len(m.acts) - 1
	case "enter":
		a := m.acts[m.actSel]
		switch err := group.Undo(a); {
		case !a.Undone.IsZero():
			m.flash("already undone")
		case err != nil:
			m.flash("not undone: %v", err)
		default:
			m.syncLayout()
			m.rebuild()
			m.flash("undone: %s", a.Summary)
			m.acts[m.actSel].Undone = time.Now()
		}
	case "U", "q", "esc":
		m.mode, m.acts = modeNormal, nil
	}
	return nil
}

// actionsModal draws the list as a box over the screen, scrolled to keep the
// selection in view.
func (m *model) actionsModal() string {
	rows := max(3, m.h-8)
	top := min(max(0, m.actSel-rows/2), max(0, len(m.acts)-rows))
	ls := []string{stBold.Render("agent actions") + stDim.Render(fmt.Sprintf("   %d · j/k move · enter undoes · esc closes", len(m.acts))), ""}
	for i := top; i < min(len(m.acts), top+rows); i++ {
		l := trunc(actionLine(m.acts[i]), max(20, m.w-12))
		switch {
		case i == m.actSel:
			l = stAccent.Render("▸ ") + stBold.Render(l)
		case !m.acts[i].Undone.IsZero():
			l = "  " + stDim.Render(l)
		default:
			l = "  " + l
		}
		ls = append(ls, l)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(0, 1).MaxWidth(m.w).Render(strings.Join(ls, "\n"))
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
		switch {
		case !a.Undone.IsZero() || time.Since(a.At) >= time.Minute:
		case a.Final: // a session stopped or removed: nothing to undo
			m.flash("%s: %s", a.Name, a.Summary)
		default:
			m.flash("%s · U to undo", fmt.Sprintf("%s: %s", a.Name, a.Summary))
		}
	}
}
