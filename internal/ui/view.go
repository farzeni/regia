package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
)

var (
	cBlocked = lipgloss.Color("214")
	cBusy    = lipgloss.Color("42")
	cIdle    = lipgloss.Color("245")
	cDone    = lipgloss.Color("65")
	cFailed  = lipgloss.Color("196")
	cInter   = lipgloss.Color("75")
	cDim     = lipgloss.Color("241")
	cAccent  = lipgloss.Color("141")

	stDim    = lipgloss.NewStyle().Foreground(cDim)
	stBold   = lipgloss.NewStyle().Bold(true)
	stAccent = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stSel    = lipgloss.NewStyle().Background(lipgloss.Color("237"))
	stHead   = lipgloss.NewStyle().Foreground(cDim).Bold(true)
)

func glyph(a *claude.Agent) (string, lipgloss.Color) {
	if a.Interactive {
		return "⌨", cInter
	}
	switch a.State {
	case "blocked":
		return "●", cBlocked
	case "busy":
		return "◆", cBusy
	case "done":
		return "✓", cDone
	case "failed":
		return "✗", cFailed
	}
	return "○", cIdle
}

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func tildify(p string) string {
	if h := claude.Home(); strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

func trunc(s string, w int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if w <= 0 {
		return ""
	}
	return runewidth.Truncate(s, w, "…")
}

func pad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func (m *model) bodyHeight() int { return max(1, m.h-4-m.inputRows()-m.compRows()) } // header, status, popup, input box

func (m *model) View() string {
	if m.w == 0 {
		return ""
	}
	m.sizeInput() // the input box grows with the draft; the list gets the rest
	bh := m.bodyHeight()
	lw := max(30, min(64, m.w*42/100))
	if m.w < 70 {
		lw = m.w
	}
	dw := m.w - lw - 3

	// scroll; when the list overflows, its last line says what's hidden
	vis := bh
	if len(m.rows) > bh && bh > 2 {
		vis = bh - 1
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
	m.offset = max(0, min(m.offset, max(0, len(m.rows)-vis)))

	left := make([]string, 0, bh)
	for i := m.offset; i < len(m.rows) && len(left) < vis; i++ {
		left = append(left, m.renderRow(m.rows[i], lw, i == m.cursor))
	}
	if vis < bh {
		left = append(left, hiddenLine(m.rows[:m.offset], m.rows[m.offset+len(left):]))
	}
	if len(m.rows) == 0 {
		msg := "no agents"
		if m.view == viewInbox {
			msg = "nobody is waiting for you"
		}
		if m.opts.Filter != "" {
			msg = "nothing matches /" + m.opts.Filter
		}
		left = append(left, stDim.Render("  "+msg))
	}
	for len(left) < bh {
		left = append(left, "")
	}

	var right []string
	if dw > 20 {
		right = m.detailLines(dw)
	}
	sep := stDim.Render("│")
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteByte('\n')
	for i := 0; i < bh; i++ {
		b.WriteString(pad(left[i], lw))
		if dw > 20 {
			b.WriteString(" " + sep + " ")
			if i < len(right) {
				b.WriteString(right[i])
			}
		}
		b.WriteByte('\n')
	}
	b.WriteString(m.footer())
	if m.help {
		return overlay(b.String(), helpModal(m.w, m.h), m.w, m.h)
	}
	return b.String()
}

func (m *model) header() string {
	var nb, nbusy, n int
	for _, a := range m.agents {
		if a.State == "done" {
			continue
		}
		n++
		switch a.State {
		case "blocked":
			nb++
		case "busy":
			nbusy++
		}
	}
	tabs := []string{"tree", "inbox"}
	for i, t := range tabs {
		if viewKind(i) == m.view {
			tabs[i] = stAccent.Render("[" + t + "]")
		} else {
			tabs[i] = stDim.Render(" " + t + " ")
		}
	}
	s := stBold.Render("regia") + "  " + strings.Join(tabs, "") + "   " +
		lipgloss.NewStyle().Foreground(cBlocked).Render(fmt.Sprintf("● %d need you", nb)) + "  " +
		lipgloss.NewStyle().Foreground(cBusy).Render(fmt.Sprintf("◆ %d busy", nbusy)) + "  " +
		stDim.Render(fmt.Sprintf("%d live", n))
	if m.opts.Filter != "" {
		s += "  " + stAccent.Render("/"+m.opts.Filter)
	}
	if m.fetchErr != nil {
		s += "  " + lipgloss.NewStyle().Foreground(cFailed).Render("listing failed: "+m.fetchErr.Error())
	}
	return s
}

func (m *model) footer() string {
	var line1 string
	switch m.mode {
	case modeInput:
		line1 = stAccent.Render(m.inputPrompt) + m.input + "█"
	case modeConfirm:
		line1 = lipgloss.NewStyle().Foreground(cBlocked).Render(m.confirmMsg)
	default:
		if time.Since(m.statusAt) < 6*time.Second {
			line1 = m.status
		}
	}
	if pop := m.compView(); pop != "" {
		line1 = pad(trunc(line1, m.w), m.w) + "\n" + pop
		return line1 + "\n" + m.inputBox()
	}
	return pad(trunc(line1, m.w), m.w) + "\n" + m.inputBox()
}

func (m *model) renderRow(r layout.Row, w int, sel bool) string {
	indent := strings.Repeat("  ", r.Depth)
	fold := "  "
	if r.HasKids || r.Group {
		fold = "▾ "
		if r.Collapsed {
			fold = "▸ "
		}
	}
	var line string
	if r.Group {
		meta := fmt.Sprintf("%d", r.Count)
		if r.Busy > 0 {
			meta = lipgloss.NewStyle().Foreground(cBusy).Render(fmt.Sprintf("◆%d ", r.Busy)) + meta
		}
		if r.Blocked > 0 {
			meta = lipgloss.NewStyle().Foreground(cBlocked).Render(fmt.Sprintf("●%d ", r.Blocked)) + meta
		}
		title := trunc(r.Title, w-len(indent)-lipgloss.Width(meta)-4)
		line = indent + stDim.Render(fold) + stHead.Render(title)
		line = pad(line, w-lipgloss.Width(meta)-1) + meta
	} else {
		a := r.Agent
		g, c := glyph(a)
		age := ago(waitedSince(a))
		cutMark := ""
		if r.ID == m.cut {
			cutMark = "✂ "
		}
		extra := ""
		if m.view == viewInbox {
			extra = "  " + a.Needs
		} else if r.Collapsed && r.Blocked > 0 {
			extra = fmt.Sprintf("  (+%d waiting)", r.Blocked)
		}
		titleW := w - len(indent) - 6 - len(age) - len(cutMark)
		title := trunc(r.Title+stripNL(extra), titleW)
		ts := lipgloss.NewStyle()
		if a.State == "blocked" {
			ts = ts.Bold(true)
		} else if a.State == "done" {
			ts = ts.Foreground(cDim)
		}
		line = indent + stDim.Render(fold) + lipgloss.NewStyle().Foreground(c).Render(g) + " " + cutMark + ts.Render(title)
		line = pad(line, w-len(age)-1) + stDim.Render(age)
	}
	if sel {
		return stSel.Render(pad(line, w))
	}
	return line
}

func stripNL(s string) string { return strings.ReplaceAll(s, "\n", " ") }

func wrap(s string, w int) []string {
	if s == "" {
		return nil
	}
	return strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
}

func (m *model) detailLines(w int) []string {
	r := m.selected()
	if r == nil {
		return nil
	}
	var out []string
	sec := func(t string) { out = append(out, "", stHead.Render(t)) }
	if r.Group {
		out = append(out, stBold.Render(r.Title), stDim.Render(fmt.Sprintf("%d agents · %d need you · %d busy", r.Count, r.Blocked, r.Busy)))
		sec("waiting for you")
		n := 0
		for _, rr := range m.layout.Flatten(m.agents, layout.ViewOpts{ShowDone: false}) {
			if rr.Agent != nil && rr.Agent.State == "blocked" && m.layout.IsDescendant(rr.ID, r.ID) {
				out = append(out, lipgloss.NewStyle().Foreground(cBlocked).Render("● ")+trunc(rr.Title+" — "+stripNL(rr.Agent.Needs), w-2))
				n++
			}
		}
		if n == 0 {
			out = append(out, stDim.Render("nothing"))
		}
		return out
	}
	return m.agentDetail(r, w)
}
