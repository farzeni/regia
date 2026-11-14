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

// agentDetail renders the right panel for one agent, read bottom-up like a
// chat: the history (oldest to newest, grouped by your messages) scrolls above
// a card pinned at the bottom whose last line is the agent's state, right
// above the input. Reading up from there: state, what it asks, context, past.
func (m *model) agentDetail(r *layout.Row, w int) (hist, card []string) {
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

	// ── context: one compact line each, dim labels
	var out []string
	const lw = 8
	field := func(name, text string, st lipgloss.Style, maxLines int) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		for i, l := range clip(wrap(text, max(10, w-lw)), maxLines, w-lw) {
			lab := strings.Repeat(" ", lw)
			if i == 0 {
				lab = stDim.Render(fmt.Sprintf("%-*s", lw, name))
			}
			out = append(out, lab+st.Render(l))
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
	if len(a.PRs) > 0 {
		var ids []string
		for _, p := range a.PRs {
			ids = append(ids, p.Kind+" #"+p.ID)
		}
		field("links", strings.Join(ids, "  ")+"   "+a.PRs[len(a.PRs)-1].Href, stDim, 1)
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
	card = append(out, "", meta, head)

	// ── terminal output (o) replaces the history
	if m.logsFor == r.ID && m.logs != "" {
		hist = append(hist, stHead.Render("terminal  (o)"))
		for _, l := range strings.Split(strings.TrimRight(m.logs, "\n"), "\n") {
			hist = append(hist, trunc(l, w))
		}
		return hist, card
	}

	// ── history: oldest to newest (the newest right above the card), day separators
	out = nil
	lastDay := ""
	for i := max(0, len(h.Turns)-maxTurns); i < len(h.Turns); i++ {
		t := h.Turns[i]
		start := t.LastAt
		if t.User != nil {
			start = t.User.At
		} else if len(t.Results) > 0 {
			start = t.Results[0].At
		}
		if d := dayLabel(start); d != lastDay {
			lastDay = d
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, stDim.Render(trunc("── "+d+" "+strings.Repeat("─", w), w)))
		}
		if t.User != nil {
			pre := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render("▸ you ") + stDim.Render(clock(t.User.At)+"  ")
			rep := ""
			if t.Repeats > 0 {
				rep = stDim.Render(fmt.Sprintf("  ×%d", t.Repeats+1))
			}
			out = append(out, pre+stYou.Render(trunc(claude.Tidy(t.User.Detail), w-lipgloss.Width(pre)-lipgloss.Width(rep)))+rep)
		} else {
			out = append(out, stDim.Render("▸ earlier activity"))
		}
		for j, res := range t.Results {
			rg, rc := glyph(&claude.Agent{State: res.State})
			tail := ""
			if j == len(t.Results)-1 && t.Steps > 0 {
				tail = stDim.Render(fmt.Sprintf("  ·%d step%s", t.Steps, plural(t.Steps)))
			}
			pre := "    " + lipgloss.NewStyle().Foreground(rc).Render(rg) + " " + stDim.Render(clock(res.At)+"  ")
			out = append(out, pre+trunc(res.Detail, w-lipgloss.Width(pre)-lipgloss.Width(tail))+tail)
		}
		latest := i == len(h.Turns)-1
		if len(t.Results) == 0 && (t.Steps > 0 || latest) {
			msg := fmt.Sprintf("%d step%s, no result", t.Steps, plural(t.Steps))
			if latest && a.State == "busy" && a.Detail != "" {
				msg = "now: " + a.Detail
			}
			out = append(out, "    "+lipgloss.NewStyle().Foreground(cBusy).Render("◆ ")+stDim.Render(trunc(msg, w-6)))
		}
	}
	if len(h.Turns) == 0 && !a.Interactive {
		out = append(out, stDim.Render("no history yet"))
	}
	return out, card
}

// maxTurns caps how far back the panel scrolls (the transcript has them all).
const maxTurns = 300

func clock(t time.Time) string { return t.Local().Format("15:04") }

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
		return t.Format("Mon Jan 2")
	}
	return t.Format("Jan 2 2006")
}
