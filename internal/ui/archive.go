package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Archive hides agents from regia (layout.json), leaving Claude Code alone:
// `e` archives the selection (and what's under it), `A` shows the archive.
// Select mode (`v`) marks many rows to archive or remove in one go.

// shown says whether an agent appears in the tree with the current options.
func (m *model) shown(id string) bool {
	a := m.agents[id]
	if a == nil || !m.opts.Visible(a, m.layout.Alias[id], m.layout.Pins[id]) {
		return false
	}
	return !m.layout.IsArchived(id) || m.opts.ShowArchived || m.opts.Filter != ""
}

// under is the selection's agents as shown: the agent itself and everything
// below it, folded or not ("": everything), in tree order.
func (m *model) under(id string) []string {
	saved := m.layout.Collapsed
	m.layout.Collapsed = map[string]bool{} // Flatten skips what's under a fold
	rows := m.layout.Flatten(m.agents, m.opts)
	m.layout.Collapsed = saved
	var ids []string
	for _, r := range rows {
		if r.Agent != nil && (id == "" || m.layout.IsDescendant(r.ID, id)) && m.shown(r.ID) {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// archiveSelection is `e`: archive the selected agent (or group) with what's
// under it, busy agents excepted; on an archived agent, bring it back.
func (m *model) archiveSelection() tea.Cmd {
	r := m.selected()
	if r == nil {
		return nil
	}
	if r.Agent != nil && m.layout.IsArchived(r.ID) {
		ids := m.archivedUnder(r.ID)
		m.layout.Unarchive(ids...)
		m.flash("unarchived %s", count(len(ids), "agent"))
		m.saveLayout()
		return nil
	}
	var ids []string
	busy := 0
	for _, id := range m.under(r.ID) {
		if m.agents[id].State == "busy" {
			busy++
			continue
		}
		if !m.layout.IsArchived(id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		m.flash("nothing to archive (busy agents stay)")
		return nil
	}
	do := func(m *model) tea.Cmd {
		m.layout.Archive(ids...)
		msg := "archived " + count(len(ids), "agent")
		if busy > 0 {
			msg += fmt.Sprintf(" (%d busy left)", busy)
		}
		m.flash("%s · A shows the archive, e there brings back", msg)
		m.saveLayout()
		return nil
	}
	if len(ids) == 1 && r.Agent != nil {
		return do(m)
	}
	m.confirm("e", fmt.Sprintf("archive %s under %q? (hidden from regia only)", count(len(ids), "agent"), r.Title), do)
	return nil
}

// archivedUnder is id and its archived descendants.
func (m *model) archivedUnder(id string) []string {
	ids := []string{id}
	for aid := range m.layout.Archived {
		if aid != id && m.layout.IsDescendant(aid, id) {
			ids = append(ids, aid)
		}
	}
	return ids
}

// ---------------------------------------------------------------- select mode

// updateSelect handles the keys that mean something else in select mode;
// the rest (movement, folding, filter…) work as usual.
func (m *model) updateSelect(k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "esc":
		m.selecting, m.marked = false, nil
		m.flash("select mode off")
		return nil, true
	case " ", "v":
		if k.String() == "v" { // v again: leave, like esc
			m.selecting, m.marked = false, nil
			return nil, true
		}
		if r := m.selected(); r != nil {
			ids := m.under(r.ID)
			all := len(ids) > 0
			for _, id := range ids {
				all = all && m.marked[id]
			}
			for _, id := range ids {
				if all {
					delete(m.marked, id)
				} else {
					m.marked[id] = true
				}
			}
			m.moveCursor(1)
		}
		return nil, true
	case "*": // everything shown; again: nothing
		ids := m.under("")
		if len(m.marked) >= len(ids) {
			m.marked = map[string]bool{}
		} else {
			for _, id := range ids {
				m.marked[id] = true
			}
		}
		return nil, true
	case "e":
		return m.archiveMarked(), true
	case "m":
		m.moveMarked()
		return nil, true
	case "n":
		m.groupMarked()
		return nil, true
	case "x":
		return m.removeMarked(), true
	case "f":
		m.pinMarked()
		return nil, true
	}
	return nil, false
}

func (m *model) markedIDs() []string {
	var ids []string
	for _, id := range m.under("") {
		if m.marked[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

func (m *model) archiveMarked() tea.Cmd {
	ids := m.markedIDs()
	if len(ids) == 0 {
		m.flash("nothing marked: space marks a row (a group: all of it), * marks all")
		return nil
	}
	allArchived := true
	for _, id := range ids {
		allArchived = allArchived && m.layout.IsArchived(id)
	}
	if allArchived {
		m.layout.Unarchive(ids...)
		m.flash("unarchived %s", count(len(ids), "agent"))
	} else {
		m.layout.Archive(ids...)
		m.flash("archived %s · A shows the archive", count(len(ids), "agent"))
	}
	m.selecting, m.marked = false, nil
	m.saveLayout()
	return nil
}

type removedBatchMsg struct {
	removed    []string
	refused    []string // "name: reason"
	refusedIDs []string
}

func (m *model) removeMarked() tea.Cmd {
	var ids, names []string
	for _, id := range m.markedIDs() {
		if a := m.agents[id]; a != nil && !a.Interactive {
			ids = append(ids, id)
			names = append(names, m.titleOf(id))
		}
	}
	if len(ids) == 0 {
		m.flash("nothing marked that claude rm can remove (interactive sessions can't be)")
		return nil
	}
	list := strings.Join(names, ", ")
	m.confirm("x", fmt.Sprintf("remove %s with claude rm (%s)? worktrees deleted if safe; conversations kept", count(len(ids), "session"), trunc(list, 60)), func(m *model) tea.Cmd {
		m.selecting, m.marked = false, nil
		m.hideRemoved(ids...)
		m.flash("removing %s…", count(len(ids), "session"))
		return func() tea.Msg {
			var res removedBatchMsg
			for i, id := range ids {
				if out, err := removeAgent(id); err != nil {
					res.refused = append(res.refused, names[i]+": "+oneLine(out, err))
					res.refusedIDs = append(res.refusedIDs, id)
				} else {
					res.removed = append(res.removed, id)
				}
			}
			return res
		}
	})
	return nil
}

func count(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// markCounts is, for every node (agent, group, "" for the whole tree), how
// many shown agents are under it and how many of those are marked. Every
// row's checkbox needs it, so it is built once a frame from one walk of the
// tree: under() flattens the whole tree, once a row was seconds a keypress.
func (m *model) markCounts() map[string][2]int {
	if m.markCount != nil {
		return m.markCount
	}
	c := map[string][2]int{}
	for _, id := range m.under("") {
		marked := m.marked[id]
		for cur, n := id, 0; n < 64; n++ { // the agent, its ancestors, the root (as IsDescendant)
			x := c[cur]
			x[0]++
			if marked {
				x[1]++
			}
			c[cur] = x
			if cur == "" {
				break
			}
			cur = m.layout.Parent[cur]
		}
	}
	m.markCount = c
	return c
}

// markFor is the select-mode checkbox for a row: ◉ marked, ○ not, ◐ a group
// partly marked; nothing outside select mode.
func (m *model) markFor(id string) string {
	if !m.selecting {
		return ""
	}
	x := m.markCounts()[id]
	st := lipgloss.NewStyle().Foreground(cBlocked)
	switch {
	case x[0] > 0 && x[1] == x[0]:
		return st.Render("◉ ")
	case x[1] > 0:
		return st.Render("◐ ")
	}
	return stDim.Render("○ ")
}
