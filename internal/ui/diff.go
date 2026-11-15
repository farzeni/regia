package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/gitinfo"
	"github.com/farzeni/regia/internal/layout"
)

// diffView is the panel's diff mode (D): the selected agent's changes against
// the merge-base with the repo's default branch, uncommitted ones included.
// git runs in a tea.Cmd; gitinfo caches by directory + HEAD + dirty state.
type diffView struct {
	on      bool
	id, dir string        // the row and directory it shows
	d       *gitinfo.Diff // nil while loading
	// rendered lines, kept while the diff and the width stay the same
	linesFor *gitinfo.Diff
	linesW   int
	lines    []string
}

type diffMsg struct {
	id, dir string
	d       gitinfo.Diff
}

func diffCmd(id, dir string) tea.Cmd {
	return func() tea.Msg { return diffMsg{id, dir, gitinfo.LoadDiff(dir)} }
}

// diffKey toggles the diff mode on the selected agent.
func (m *model) diffKey(r *layout.Row) tea.Cmd {
	if m.dv.on {
		m.closeDiff()
		return nil
	}
	if r == nil || r.Agent == nil {
		m.flash("diff: select an agent")
		return nil
	}
	m.dv = diffView{on: true, id: r.ID, dir: r.Agent.Cwd}
	m.flash("changes vs the default branch · pgup/pgdn scroll · D or esc: back")
	return diffCmd(r.ID, r.Agent.Cwd)
}

func (m *model) closeDiff() {
	m.dv = diffView{}
	m.panelFor = "" // the history opens where it usually does
}

// gotDiff takes a finished diff if it's still the one on screen.
func (m *model) gotDiff(msg diffMsg) {
	if !m.dv.on || msg.id != m.dv.id || msg.dir != m.dv.dir {
		return
	}
	d := msg.d
	m.dv.d = &d
	// open at the top: placePanel clamps the scroll to the first line
	m.panelFor, m.panelScroll = m.cursorID, 1<<30
}

// diffHist is the panel's history replaced by the diff, when diff mode is on
// for this agent.
func (m *model) diffHist(a *claude.Agent, id string, w int) ([]string, bool) {
	if !m.dv.on || m.dv.id != id {
		return nil, false
	}
	if m.dv.d == nil {
		return []string{diffTitle("", w), stDim.Render("computing the diff…")}, true
	}
	if m.dv.linesFor != m.dv.d || m.dv.linesW != w {
		m.dv.lines, m.dv.linesFor, m.dv.linesW = diffLines(*m.dv.d, w), m.dv.d, w
	}
	return m.dv.lines, true
}

var (
	stAdd  = lipgloss.NewStyle().Foreground(cBusy)
	stDel  = lipgloss.NewStyle().Foreground(cFailed)
	stHunk = lipgloss.NewStyle().Foreground(cAccent)
)

func diffTitle(base string, w int) string {
	t := "changes"
	if base != "" {
		t += " vs " + base
	}
	return trunc(stHead.Render(t+"  (D)"), w)
}

// diffLines renders a Diff: title, the --stat block, then the colored diff.
func diffLines(d gitinfo.Diff, w int) []string {
	out := []string{diffTitle(d.Base, w)}
	switch {
	case d.Err != "":
		return append(out, stDim.Render("no diff: "+d.Err))
	case d.Empty:
		msg := "no changes"
		if d.Base != "" {
			msg += " against " + d.Base
		}
		return append(out, stDim.Render(msg))
	}
	if d.Base == "" {
		out = append(out, stDim.Render("no main/master branch found: showing uncommitted changes only"))
	}
	for _, l := range strings.Split(d.Stat, "\n") {
		out = append(out, statLine(clean(l), w))
	}
	out = append(out, "")
	for _, l := range strings.Split(d.Body, "\n") {
		out = append(out, bodyLine(clean(l), w))
	}
	if d.Cut > 0 {
		out = append(out, "", stDim.Render(fmt.Sprintf("… %d more lines not shown (the diff is capped at %d lines)", d.Cut, gitinfo.MaxDiffLines)))
	}
	return out
}

// statLine fits " path | 12 ++--" into w, shortening the path from the left
// so the counts stay visible, and colors the +/- bar.
func statLine(l string, w int) string {
	i := strings.LastIndex(l, " | ")
	if i < 0 { // the summary line, "N files changed, …"
		return stDim.Render(trunc(l, w))
	}
	path, rest := strings.TrimSpace(l[:i]), l[i:]
	if room := w - runewidth.StringWidth(rest) - 1; runewidth.StringWidth(path) > room {
		if room < 2 {
			return trunc(l, w)
		}
		r := []rune(path)
		for runewidth.StringWidth(string(r)) > room-1 {
			r = r[1:]
		}
		path = "…" + string(r)
	}
	bar := strings.TrimRight(rest, "+-")
	signs := rest[len(bar):]
	plus := strings.Count(signs, "+")
	return " " + path + bar + stAdd.Render(strings.Repeat("+", plus)) + stDel.Render(signs[plus:])
}

func bodyLine(l string, w int) string {
	t := trunc(l, w)
	switch {
	case strings.HasPrefix(l, "diff --git "):
		return stBold.Render(t)
	case strings.HasPrefix(l, "+++ "), strings.HasPrefix(l, "--- "),
		strings.HasPrefix(l, "index "), strings.HasPrefix(l, "new file"), strings.HasPrefix(l, "deleted file"),
		strings.HasPrefix(l, "similarity "), strings.HasPrefix(l, "rename "), strings.HasPrefix(l, "old mode"),
		strings.HasPrefix(l, "new mode"), strings.HasPrefix(l, "Binary files"), strings.HasPrefix(l, `\ No newline`):
		return stDim.Render(t)
	case strings.HasPrefix(l, "@@"):
		return stHunk.Render(t)
	case strings.HasPrefix(l, "+"):
		return stAdd.Render(t)
	case strings.HasPrefix(l, "-"):
		return stDel.Render(t)
	}
	return t
}

// clean makes a line of someone's file safe to draw: tabs become spaces and
// control characters (escape sequences, \r) are dropped.
func clean(l string) string {
	if !strings.ContainsFunc(l, func(r rune) bool { return r == '\t' || unicode.IsControl(r) }) {
		return l
	}
	var b strings.Builder
	for _, r := range l {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
