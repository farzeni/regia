package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// agentDetail renders the right panel for one agent: a status card that says
// where it is, then the history grouped by your messages (newest first).
func (m *model) agentDetail(r *Row, w int) []string {
	a := r.Agent
	var h History
	if m.histFor == r.ID {
		h = m.hist
	}
	var out []string
	g, c := glyph(a)
	col := lipgloss.NewStyle().Foreground(c)

	// ── header: big state + how long, name
	label := map[string]string{
		"blocked": "WAITING ON YOU", "busy": "WORKING", "done": "DONE",
		"failed": "FAILED", "idle": "IDLE",
	}[a.State]
	if a.Interactive {
		label = "INTERACTIVE"
	}
	if label == "" {
		label = strings.ToUpper(a.State)
	}
	since := waitedSince(a)
	if a.State == "blocked" && h.Status != nil {
		since = h.Status.At // waiting since it last reported, not since the file changed
	}
	state := g + " " + label + " · " + ago(since)
	out = append(out, col.Bold(true).Render(state)+"   "+stBold.Render(trunc(r.Title, w-lipgloss.Width(state)-3)))
	meta := tildify(a.Cwd)
	if gi, ok := cachedGit(a.Cwd); a.Branch != "" && !(ok && gi.OK) {
		meta += " ⎇ " + a.Branch
	}
	if a.Tokens > 0 {
		meta += fmt.Sprintf(" · %dk tok", a.Tokens/1000)
	}
	if !a.Interactive {
		meta += " · " + a.ID
	}
	out = append(out, stDim.Render(trunc(meta, w)))
	out = append(out, "")

	// ── card
	const lw = 8
	field := func(name, text string, st lipgloss.Style, maxLines int) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		lines := wrap(text, max(10, w-lw))
		if maxLines > 0 && len(lines) > maxLines {
			lines = lines[:maxLines]
			lines[maxLines-1] = trunc(strings.TrimRight(lines[maxLines-1], " ")+" …", w-lw)
		}
		for i, l := range lines {
			lab := strings.Repeat(" ", lw)
			if i == 0 {
				lab = stHead.Render(fmt.Sprintf("%-*s", lw, name))
			}
			out = append(out, lab+st.Render(l))
		}
	}
	plain := lipgloss.NewStyle()
	field("TASK", tidy(h.Task), plain, 2)
	switch {
	case a.State == "busy" && a.Detail != "":
		field("NOW", a.Detail, lipgloss.NewStyle().Foreground(cBusy), 2)
		if h.Status != nil {
			field("LAST", h.Status.Detail, stDim, 1)
		}
	case h.Status != nil:
		_, sc := glyph(&Agent{State: h.Status.State})
		field("STATUS", h.Status.Detail, lipgloss.NewStyle().Foreground(sc), 2)
	}
	if a.State == "blocked" {
		field("NEEDS", a.Needs, lipgloss.NewStyle().Foreground(cBlocked).Bold(true), 6)
		if a.Suggested != "" {
			field("REPLY", "› "+a.Suggested+"   (y copies)", plain, 2)
		}
	}
	if gi, ok := cachedGit(a.Cwd); ok && gi.OK {
		l1, l2 := gi.lines()
		st := plain
		if gi.Changed+gi.Untracked > 0 {
			st = lipgloss.NewStyle().Foreground(cBlocked)
		}
		field("GIT", l1, st, 1)
		field("", l2, stDim, 1)
	}
	if len(a.PRs) > 0 {
		var ids []string
		for _, p := range a.PRs {
			ids = append(ids, p.Kind+" #"+p.ID)
		}
		field("LINKS", strings.Join(ids, "  ")+"   "+a.PRs[len(a.PRs)-1].Href, stDim, 2)
	}

	// ── terminal output (o) replaces the history
	if m.logsFor == r.ID && m.logs != "" {
		out = append(out, "", stHead.Render("terminal  (o)"))
		ls := strings.Split(strings.TrimRight(m.logs, "\n"), "\n")
		for _, l := range ls[max(0, len(ls)-25):] {
			out = append(out, trunc(l, w))
		}
		return out
	}

	// ── history: turns newest first, day separators
	limit := m.bodyHeight()
	lastDay := ""
	for i := len(h.Turns) - 1; i >= 0 && len(out) < limit; i-- {
		t := h.Turns[i]
		start := t.LastAt
		if t.User != nil {
			start = t.User.At
		} else if len(t.Results) > 0 {
			start = t.Results[0].At
		}
		if d := dayLabel(start); d != lastDay {
			lastDay = d
			out = append(out, "", stDim.Render(trunc("── "+d+" "+strings.Repeat("─", w), w)))
		}
		if t.User != nil {
			pre := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render("▸ you ") + stDim.Render(clock(t.User.At)+"  ")
			rep := ""
			if t.Repeats > 0 {
				rep = stDim.Render(fmt.Sprintf("  ×%d", t.Repeats+1))
			}
			out = append(out, pre+trunc(tidy(t.User.Detail), w-lipgloss.Width(pre)-lipgloss.Width(rep))+rep)
		} else {
			out = append(out, stDim.Render("▸ earlier activity"))
		}
		for j, res := range t.Results {
			if len(out) >= limit {
				break
			}
			rg, rc := glyph(&Agent{State: res.State})
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
		out = append(out, "", stDim.Render("no history yet"))
	}
	return out
}

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
