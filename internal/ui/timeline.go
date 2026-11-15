package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/gitinfo"
	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

// The group timeline (T): for a group, or a coordinator with its agents, one
// feed of what happened, oldest first and newest at the bottom like the rest
// of the panel: the agents' state changes (from their timeline.jsonl), the
// blackboard notes, the commits on their branches (git log, read in the
// background and cached) and the PRs they opened (state.json). Every source
// is optional: what can't be read is left out.

// tlItem is one line of the timeline.
type tlItem struct {
	At    time.Time
	Who   string
	Icon  string
	Color lipgloss.TerminalColor
	Text  string
	Dim   bool // routine (back to work): shown, but quiet
}

// timelineAgents lists the agents in container (the container itself, when
// it is a coordinator), in start order.
func (m *model) timelineAgents(container string) []*claude.Agent {
	var as []*claude.Agent
	for id, a := range m.agents {
		if a != nil && m.layout.IsDescendant(id, container) {
			as = append(as, a)
		}
	}
	claude.SortByStart(as)
	return as
}

// logKeys groups the agents by the log that covers their work: their
// directory and branch, read since the first of them started.
func logKeys(as []*claude.Agent) map[gitinfo.LogKey][]*claude.Agent {
	type dr struct{ dir, ref string }
	by := map[dr][]*claude.Agent{}
	for _, a := range as {
		if a.Interactive || a.Cwd == "" {
			continue
		}
		ref := a.Branch
		if ref == "" {
			ref = "HEAD"
		}
		k := dr{a.Cwd, ref}
		by[k] = append(by[k], a)
	}
	out := map[gitinfo.LogKey][]*claude.Agent{}
	for k, ag := range by {
		since := ag[0].StartedAt // sorted by start
		for _, a := range ag {
			if a.StartedAt.Before(since) {
				since = a.StartedAt
			}
		}
		out[gitinfo.LogKey{Dir: k.dir, Ref: k.ref, Since: since.Truncate(time.Minute)}] = ag
	}
	return out
}

// timelineItems gathers container's feed, oldest first.
func (m *model) timelineItems(container string) []tlItem {
	as := m.timelineAgents(container)
	var items []tlItem
	for _, a := range as {
		if a.Interactive {
			continue
		}
		name := m.titleOf(a.ID)
		info := claude.ReadTimeline(a.ID)
		for i, e := range info.Events {
			it := tlItem{At: e.At, Who: name, Text: e.Detail}
			switch e.State {
			case "busy":
				it.Icon, it.Color = "▶", cBusy
				if i == 0 {
					it.Text = prefix("started", e.Detail)
				} else {
					it.Icon, it.Dim = "◆", true
					it.Text = prefix("back to work", e.Detail)
				}
			case "blocked":
				it.Icon, it.Color, it.Text = "●", cBlocked, prefix("waiting", e.Detail)
			case "done":
				it.Icon, it.Color, it.Text = "✓", cReady, prefix("done", e.Detail)
			case "failed":
				it.Icon, it.Color, it.Text = "✗", cFailed, prefix("failed", e.Detail)
			}
			items = append(items, it)
		}
		for _, p := range a.PRs {
			at, ok := info.Links[p.Href]
			if !ok {
				at = a.UpdatedAt
			}
			if at.IsZero() {
				continue
			}
			kind := p.Kind
			if kind == "" {
				kind = "PR"
			}
			items = append(items, tlItem{At: at, Who: name, Icon: "⇡", Color: cAccent,
				Text: strings.TrimSpace(fmt.Sprintf("opened %s #%s %s", kind, p.ID, p.Href))})
		}
	}
	for _, n := range group.ReadNotes(container) {
		items = append(items, tlItem{At: n.At, Who: n.Name, Icon: "✎", Color: cAccent, Text: n.Text})
	}
	seen := map[string]bool{}
	for k, ag := range logKeys(as) {
		cs, _ := gitinfo.CachedLog(k)
		for _, c := range cs {
			if seen[c.Hash] {
				continue
			}
			seen[c.Hash] = true
			items = append(items, tlItem{At: c.At, Who: m.titleOf(committer(ag, c.At).ID), Icon: "⎇", Color: cDone,
				Text: c.Hash + " " + c.Subject})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.Before(items[j].At) })
	for i := range items {
		if items[i].Who == "" {
			items[i].Who = "?"
		}
	}
	return items
}

// committer is the agent a commit is put down to: of those sharing the
// branch, the last one started before it.
func committer(ag []*claude.Agent, at time.Time) *claude.Agent {
	who := ag[0]
	for _, a := range ag {
		if !a.StartedAt.After(at) {
			who = a
		}
	}
	return who
}

func prefix(what, detail string) string {
	if detail == "" {
		return what
	}
	return what + ": " + detail
}

// timelineLines renders the feed for a panel w wide: a rule per day, then
// "15:04 name ● what" lines, newest last.
func timelineLines(items []tlItem, w int) []string {
	if len(items) == 0 {
		return []string{stDim.Render("nothing yet: no state changes, notes, commits or PRs")}
	}
	nameW := 4
	for _, it := range items {
		nameW = max(nameW, lipgloss.Width(it.Who))
	}
	nameW = min(nameW, max(8, w/4))
	var out []string
	lastDay := ""
	for _, it := range items {
		if d := dayLabel(it.At); d != lastDay {
			lastDay = d
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, stDim.Render(trunc("── "+d+" "+strings.Repeat("─", w), w)))
		}
		who := trunc(it.Who, nameW)
		who += strings.Repeat(" ", max(0, nameW-lipgloss.Width(who)))
		pre := stDim.Render(clock(it.At)+" ") + stBold.Render(who) + " " + lipgloss.NewStyle().Foreground(it.Color).Render(it.Icon) + " "
		text := trunc(stripNL(it.Text), max(4, w-lipgloss.Width(pre)))
		if it.Dim {
			text = stDim.Render(text)
		}
		out = append(out, pre+text)
	}
	return out
}

var tlCache struct {
	id     string
	w, gen int
	lines  []string
}

// timelinePanel is the timeline of container for the panel, cached per
// frame generation: the git logs are read only from the cache here.
func (m *model) timelinePanel(container string, w int) []string {
	if c := &tlCache; c.id == container && c.w == w && c.gen == m.gen {
		return c.lines
	}
	lines := timelineLines(m.timelineItems(container), w)
	tlCache.id, tlCache.w, tlCache.gen, tlCache.lines = container, w, m.gen, lines
	return lines
}

// timelineTarget is the container whose timeline the panel shows for r, ""
// when the timeline is off or r isn't a group or coordinator.
func (m *model) timelineTarget(r *layout.Row) string {
	if !m.timeline || r == nil || !m.isContainer(r) {
		return ""
	}
	return r.ID
}

type timelineMsg struct{}

// timelineCmd reads, in the background, the git logs the shown timeline needs
// that aren't cached or are stale.
func (m *model) timelineCmd() tea.Cmd {
	c := m.timelineTarget(m.selected())
	if c == "" {
		return nil
	}
	var fs []func()
	for k := range logKeys(m.timelineAgents(c)) {
		if f := gitinfo.LogRefresher(k); f != nil {
			fs = append(fs, f)
		}
	}
	if len(fs) == 0 {
		return nil
	}
	return func() tea.Msg {
		for _, f := range fs {
			f()
		}
		return timelineMsg{}
	}
}

// timelineKey (T) swaps the blackboard of a group or coordinator for its
// timeline and back. On an agent it selects the agent's group first.
func (m *model) timelineKey(r *layout.Row) tea.Cmd {
	target := ""
	if r != nil && m.isContainer(r) {
		target = r.ID
	} else {
		target = m.noteTarget(r)
		if target != "" && m.visRow[target] {
			m.cursorID = target
			m.rebuild()
		} else {
			target = ""
		}
	}
	if target == "" {
		m.flash("the timeline belongs to a hand-made group (n) or a coordinator's agents")
		return nil
	}
	if r.ID == target {
		m.timeline = !m.timeline
	} else { // from an agent: show its group's timeline
		m.timeline = true
	}
	m.panelFor = "" // open at the newest line
	if m.timeline {
		m.flash("timeline of %s · pgup/pgdn scroll · T: blackboard", m.titleOf(target))
	} else {
		m.flash("blackboard · T: timeline")
	}
	return m.timelineCmd()
}

// timelineView is the panel of a group or coordinator with the timeline on:
// a short card at the top (a group's title and counts, or the coordinator's
// own card) and the timeline below it, stuck to the bottom and scrolled with
// pgup/pgdn like an agent's history.
func (m *model) timelineView(r *layout.Row, w int) panel {
	head := stHead.Render("timeline  (T: blackboard · pgup/pgdn)")
	var card []string
	if r.Group {
		card = []string{stBold.Render(r.Title),
			stDim.Render(fmt.Sprintf("%d agents · %d your turn · %d busy", r.Count, r.Blocked, r.Busy)), "", head}
	} else {
		_, c, foot, _ := m.agentDetail(r, w)
		card = append(append(c, foot...), "", head)
	}
	return panel{hist: m.timelinePanel(r.ID, w), card: card, bottomUp: true}
}

// DumpTimeline prints the timeline of a group or coordinator as plain text
// (the hidden `regia --timeline <id>`), the git logs read in place.
func DumpTimeline(w io.Writer, id string) error {
	m := newModel()
	if b, err := claude.FetchAgents(); err == nil {
		m.setAgents(b)
	}
	m.w, m.h = 160, 60
	m.cursorID = id
	m.layout.Collapsed = map[string]bool{} // in memory only: rebuild never saves
	m.rebuild()
	if r := m.selected(); r == nil || r.ID != id || !m.isContainer(r) {
		return fmt.Errorf("not a visible group or coordinator: %s", id)
	}
	m.timeline = true
	if c := m.timelineCmd(); c != nil {
		c()
	}
	for _, l := range m.detailLines(110).lines() {
		fmt.Fprintln(w, l)
	}
	return nil
}
