package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/gitinfo"
	"github.com/farzeni/regia/internal/layout"
)

// agentDetail renders the right panel for one agent: a card pinned at the top
// (context, what it asks), below it the conversation (your messages and its
// replies, oldest to newest), which scrolls and opens at its end, and a foot
// pinned at the bottom: state and meta.
func (m *model) agentDetail(r *layout.Row, w int) (hist, card, foot []string) {
	a := r.Agent
	var h claude.History
	if m.histFor == r.ID {
		h = m.hist
	}
	g, c := m.agentGlyph(a)
	col := lipgloss.NewStyle().Foreground(c)

	// ── header: big state + how long, name
	label := map[string]string{
		"blocked": "WAITING ON YOU", "ready": "READY · YOUR TURN", "busy": "WORKING", "done": "DONE",
		"failed": "FAILED", "idle": "IDLE",
	}[a.State]
	if a.Interactive {
		label = "INTERACTIVE"
	}
	if a.Stale {
		label = "WAITING ON YOU · STALE"
	}
	if a.Snoozed {
		label = "WAITING ON YOU · SNOOZED"
	}
	if label == "" {
		label = strings.ToUpper(a.State)
	}
	since := waitedSince(a)
	if a.State == "blocked" && h.Status != nil {
		since = h.Status.At // waiting since it last reported, not since the file changed
	}
	state := g + " " + label + " · " + ago(since)
	head := col.Bold(true).Render(state) + "   " + stBold.Render(trunc(r.Title, w-lipgloss.Width(state)-3))
	meta := tildify(a.Cwd)
	if gi, ok := gitinfo.Cached(a.Cwd); a.Branch != "" && !(ok && gi.OK) {
		meta += " ⎇ " + a.Branch
	}
	if a.Tokens > 0 {
		meta += fmt.Sprintf(" · %dk tok", a.Tokens/1000)
	}
	if !a.Interactive {
		meta += " · " + a.ID
	}
	meta = stDim.Render(trunc(meta, w))

	// ── context: one compact line each, dim labels, in a box like the plugins' cards
	var out, ctx []string
	const lw = 8
	iw := boxInner(w)
	field := func(name, text string, st lipgloss.Style, maxLines int) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		for i, l := range clip(wrap(text, max(10, iw-lw)), maxLines, iw-lw) {
			lab := strings.Repeat(" ", lw)
			if i == 0 {
				lab = stDim.Render(fmt.Sprintf("%-*s", lw, name))
			}
			ctx = append(ctx, lab+st.Render(l))
		}
	}
	plain := lipgloss.NewStyle()
	field("task", taskTitle(h.Task), plain, 1)
	if h.Status != nil {
		label := "status"
		if a.State == "busy" {
			label = "last"
		}
		field(label, h.Status.Detail, stDim, 2)
	}
	if gi, ok := gitinfo.Cached(a.Cwd); ok && gi.OK {
		l1, l2 := gitLines(gi)
		st := plain
		if gi.Changed+gi.Untracked > 0 {
			st = lipgloss.NewStyle().Foreground(cBlocked)
		}
		field("git", l1, st, 1)
		field("", l2, stDim, 1)
	}
	pr := prFor(a)
	if pluginShowsPR(a, pr.URL) {
		// a plugin's card already shows this PR, with more detail
	} else if l, st := prLine(pr); l != "" {
		field("pr", l, st, 1)
	} else if len(a.PRs) > 0 {
		var ids []string
		for _, p := range a.PRs {
			ids = append(ids, p.Kind+" #"+p.ID)
		}
		field("links", strings.Join(ids, "  ")+"   "+a.PRs[len(a.PRs)-1].Href, stDim, 1)
	}

	if a.Unhealthy != "" {
		field("stuck?", stuckGlyph+" "+a.Unhealthy, lipgloss.NewStyle().Foreground(cDim), 1)
	}
	if g := m.grantFor(a.ID); g != "" {
		if why := m.layout.Claims[a.ID]; why != "" {
			field("claims", grantGlyph+" "+g+": "+why, stAccent, 2)
		} else {
			field("may", grantGlyph+" organize "+g+" (you granted it: O)", stAccent, 1)
		}
	}
	// the box, unless it would only say there's nothing yet
	tabs := append([]panelTab{{name: agentTab, body: ctx}, m.changesTab(a, w)}, pluginTabs(a, w)...)
	if len(ctx) > 0 || len(tabs) > 2 || tabs[1].badge != nil || m.panelTab == changesTab {
		out = append(out, m.tabbedBox(tabs, w)...)
	}

	// ── the ask: what it needs from you (or what it's doing), in a barred block
	bar := func(c lipgloss.TerminalColor, lines []string) {
		b := lipgloss.NewStyle().Foreground(c).Render("┃ ")
		for _, l := range lines {
			out = append(out, b+l)
		}
	}
	switch {
	case a.State == "blocked" && a.Needs != "":
		ls := needsLines(a.Needs, w-2)
		if a.Suggested != "" {
			ls = append(ls, "")
			for i, l := range clip(wrap("› "+a.Suggested, max(10, w-2-12)), 2, w-2-12) {
				l = lipgloss.NewStyle().Foreground(cAccent).Render(strings.TrimRight(l, " "))
				if i == 0 {
					l += stDim.Render("   y copies")
				}
				ls = append(ls, l)
			}
		}
		out = append(out, "")
		bar(cBlocked, ls)
	case a.State == "busy" && a.Detail != "":
		out = append(out, "")
		bar(cBusy, clip(wrap(a.Detail, max(10, w-2)), 3, w-2))
	}
	// the state and meta lines go in the foot, under the latest activity
	card = out
	foot = []string{head, meta}

	// ── the conversation: your messages and its replies, newest at the bottom
	return m.conversationLines(a, w), card, foot
}

func clock(t time.Time) string { return t.Local().Format("15:04") }

// humanTime is when, the way you'd say it: just now, 3 minutes ago, 2 hours
// ago (today), yesterday, Mon 5 Aug (this year), Wed 8 Jan 2025.
func humanTime(t time.Time) string { return humanTimeAt(t, time.Now()) }

func humanTimeAt(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	d := now.Sub(t)
	y, mo, dd := now.Date()
	today := time.Date(y, mo, dd, 0, 0, 0, 0, now.Location())
	ago := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit + " ago"
		}
		return fmt.Sprintf("%d %ss ago", n, unit)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return ago(int(d/time.Minute), "minute")
	case !t.Before(today):
		return ago(int(d/time.Hour), "hour")
	case !t.Before(today.AddDate(0, 0, -1)):
		return "yesterday"
	case t.Year() == now.Year():
		return t.Format("Mon 2 Jan")
	}
	return t.Format("Mon 2 Jan 2006")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func dayLabel(t time.Time) string {
	t = t.Local()
	now := time.Now()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today):
		return "Today"
	case !t.Before(today.AddDate(0, 0, -1)):
		return "Yesterday"
	case t.Year() == now.Year():
		return t.Format("Mon 2 Jan")
	}
	return t.Format("Mon 2 Jan 2006")
}
