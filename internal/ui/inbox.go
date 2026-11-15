package ui

import (
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
)

// The inbox is about today. What has waited since before today (see
// dayStart) folds into one row at the bottom, "earlier days", that space
// opens; it starts folded every time regia opens. Snoozed questions (Z) are
// left out until their agent does something new.

const earlierID = "inbox:earlier" // the fold's row id: not a layout node

// inboxRows builds the inbox: today's items, then the earlier-days fold.
func (m *model) inboxRows(now time.Time) []layout.Row {
	var today, earlier []*claude.Agent
	for _, a := range m.agents {
		keep := a.ID == m.opts.Keep // the one you're reading stays until you move away
		if !(a.State == "blocked" || a.State == "ready" || keep) || !m.shown(a.ID) || (a.Snoozed && !keep) {
			continue
		}
		if fromToday(a, now) {
			today = append(today, a)
		} else {
			earlier = append(earlier, a)
		}
	}
	sortInbox(today)
	sortInbox(earlier)
	m.earlier = earlier

	rows := make([]layout.Row, 0, len(today)+len(earlier)+1)
	for _, a := range today {
		rows = append(rows, m.inboxRow(a, 0))
	}
	if len(earlier) == 0 {
		return rows
	}
	open := m.earlierOpen || m.opts.Filter != ""
	fold := layout.Row{ID: earlierID, Group: true, Title: "earlier days", HasKids: true, Collapsed: !open, Count: len(earlier)}
	for _, a := range earlier {
		if a.YourTurn() {
			fold.Blocked++
		}
	}
	rows = append(rows, fold)
	if open {
		for _, a := range earlier {
			rows = append(rows, m.inboxRow(a, 1))
		}
	}
	return rows
}

// sortInbox: questions, then results to read, then stale; newest first in each.
func sortInbox(as []*claude.Agent) {
	sort.SliceStable(as, func(i, j int) bool {
		if ri, rj := inboxRank(as[i]), inboxRank(as[j]); ri != rj {
			return ri < rj
		}
		if ti, tj := waitedSince(as[i]), waitedSince(as[j]); !ti.Equal(tj) {
			return ti.After(tj)
		}
		return as[i].ID < as[j].ID
	})
}

func (m *model) inboxRow(a *claude.Agent, depth int) layout.Row {
	t := a.Name
	if al := m.layout.Alias[a.ID]; al != "" {
		t = al
	}
	return layout.Row{ID: a.ID, Depth: depth, Agent: a, Title: t, Where: m.layout.Trail(a.ID, m.agents)}
}

// earlierSummary says what the fold holds, e.g. "9 questions, 1 result · oldest 3d".
func (m *model) earlierSummary() string {
	var q, res int
	var oldest time.Time
	for _, a := range m.earlier {
		if a.State == "ready" {
			res++
		} else {
			q++
		}
		if t := waitedSince(a); oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
	}
	var s string
	if q > 0 {
		s = count(q, "question")
	}
	if res > 0 {
		if s != "" {
			s += ", "
		}
		s += count(res, "result")
	}
	if !oldest.IsZero() {
		s += " · oldest " + ago(oldest)
	}
	return s
}

// earlierSubline is the fold row's second line.
func (m *model) earlierSubline(r layout.Row) string {
	hint := "space opens"
	if !r.Collapsed {
		hint = "space folds"
	}
	return "    " + stDim.Render(m.earlierSummary()+" · "+hint)
}

// earlierPanel is the right panel for the fold row: what's in it.
func (m *model) earlierPanel(w int) panel {
	out := []string{
		stBold.Render("earlier days"),
		stDim.Render(trunc(fmt.Sprintf("waiting since before today (%d:00) · not counted in \"need you\"", dayStarts), w)),
		"",
	}
	for _, a := range m.earlier {
		g, c := glyph(a)
		age := ago(waitedSince(a))
		what := stripNL(a.Needs)
		if a.State == "ready" {
			what = "finished: " + stripNL(a.Detail)
		}
		title := m.inboxRow(a, 0).Title
		line := lipgloss.NewStyle().Foreground(c).Render(g+" ") + trunc(title+" — "+what, w-3-len(age))
		out = append(out, pad(line, w-len(age))+stDim.Render(age))
	}
	return panel{card: out}
}

// earlierKey handles the keys that act on the fold row. It reports whether
// the key was used (or must do nothing here: the row is not a layout node).
func (m *model) earlierKey(k string) bool {
	switch k {
	case "enter", " ", "z":
		m.earlierOpen = !m.earlierOpen
	case "right", "l":
		m.earlierOpen = true
	case "left", "h":
		m.earlierOpen = false
	case "e", "f", "x", "s", "r", "d", "N", "M", "w", "m", "Z", "y", "Y", "o":
		return true // agent and group actions: nothing to act on
	default:
		return false
	}
	m.rebuild()
	return true
}
