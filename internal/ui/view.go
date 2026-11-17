package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

var (
	cBlocked = lipgloss.Color("214")
	cBusy    = lipgloss.Color("42")
	cIdle    = lipgloss.Color("245")
	cDone    = lipgloss.Color("65")
	cReady   = lipgloss.Color("78") // done, not seen yet: a brighter green
	cFailed  = lipgloss.Color("196")
	cInter   = lipgloss.Color("75")
	cDim     = lipgloss.Color("241")
	cAccent  = lipgloss.Color("141")

	stDim    = lipgloss.NewStyle().Foreground(cDim)
	stBold   = lipgloss.NewStyle().Bold(true)
	stAccent = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stSel    = lipgloss.NewStyle().Background(lipgloss.Color("237"))
	stYou    = lipgloss.NewStyle().Foreground(lipgloss.Color("183")) // your messages in the history: a light tint of the accent
	stHead   = lipgloss.NewStyle().Foreground(cDim).Bold(true)
	stErr    = lipgloss.NewStyle().Foreground(cFailed) // a failed step
	stBubble = stYou.Background(lipgloss.Color("236")) // your message in the conversation: your tint on a subtle background
)

func glyph(a *claude.Agent) (string, lipgloss.Color) {
	if a.Interactive {
		return "⌨", cInter
	}
	switch a.State {
	case "blocked":
		if a.Stale || a.Snoozed {
			return "●", cIdle
		}
		return "●", cBlocked
	case "ready":
		return "✓", cReady
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

func (m *model) bodyHeight() int {
	return max(1, m.h-5-m.inputRows()-m.compRows()-m.dirPickRows()-m.modelPickRows())
} // header + gap, status, popup, input box

// padX is the margin left and right of the tree and the panel; gap separates
// them (the divider sits in its middle).
const (
	padX = 1
	gap  = 5
)

func (m *model) View() string {
	if m.w == 0 {
		return ""
	}
	m.sizeInput() // the input box grows with the draft; the list gets the rest
	bh := m.bodyHeight()
	iw := m.w - 2*padX
	lw := m.sidebarWidth(iw)
	dw := iw - lw - gap
	m.lw = lw

	// scroll, keeping a few rows of context around the selection; when the
	// list overflows, its last line says what's hidden
	rh := m.rowHeight()
	vis := bh / rh // in rows
	if len(m.rows) > vis && bh > 2 {
		vis = (bh - 1) / rh
	}
	vis = max(1, vis)
	so := min(scrollOff, max(0, (vis-1)/2))
	if m.cursor < m.offset+so {
		m.offset = m.cursor - so
	}
	if m.cursor >= m.offset+vis-so {
		m.offset = m.cursor - vis + 1 + so
	}
	m.offset = max(0, min(m.offset, max(0, len(m.rows)-vis)))
	m.vis = vis

	left := make([]string, 0, bh)
	sel := -1 // the selected row's first line
	end := m.offset
	for ; end < len(m.rows) && end < m.offset+vis; end++ {
		if end == m.cursor {
			sel = len(left)
		}
		left = append(left, m.renderRow(m.rows[end], lw)...)
	}
	if len(m.rows) > vis {
		left = append(left, hiddenLine(m.rows[:m.offset], m.rows[end:]))
	}
	if len(m.rows) == 0 {
		msg := "no agents"
		if m.view == viewInbox {
			msg = "nobody is waiting for you"
		}
		if m.opts.Filter != "" {
			msg = "nothing matches /" + m.opts.Filter
		}
		if m.view == viewSearch {
			msg = m.searchEmpty()
		}
		left = append(left, stDim.Render("  "+msg))
	}
	for len(left) < bh {
		left = append(left, "")
	}

	var right []string
	if dw > 20 {
		if m.view == viewSearch {
			right = m.placePanel(m.searchPanel(dw, bh), bh, dw)
		} else {
			right = m.placePanel(m.detailLines(dw), bh, dw)
		}
	}
	margin := strings.Repeat(" ", padX)
	divider := stDim.Render("│")
	if m.convFocus || m.chg.focus { // the conversation or the changes tab has the keys
		divider = stAccent.Render("┃")
	}
	sep := strings.Repeat(" ", gap/2) + divider + strings.Repeat(" ", gap-gap/2-1)
	var b strings.Builder
	b.WriteString(margin + ansi.Truncate(m.header(), iw, "…"))
	b.WriteString("\n\n")
	for i := 0; i < bh; i++ {
		row := margin + pad(left[i], lw)
		if sel >= 0 && i >= sel && i < sel+rh {
			row = selBar(row+" ", padX+lw+1) // the whole row, margins too
			if dw > 20 {
				b.WriteString(row + sep[1:])
			} else {
				b.WriteString(row)
			}
		} else if dw > 20 {
			b.WriteString(row + sep)
		} else {
			b.WriteString(row)
		}
		if dw > 20 {
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
	if m.mode == modeMenu {
		return overlay(b.String(), m.menuModal(), m.w, m.h)
	}
	return b.String()
}

func (m *model) header() string {
	var nb, nready, nbusy, n, narch int
	for _, a := range m.agents {
		if m.layout.IsArchived(a.ID) {
			narch++
			continue
		}
		if a.State == "done" {
			continue
		}
		n++
		switch {
		case a.State == "blocked" && a.YourTurn():
			nb++
		case a.State == "ready":
			nready++
		case a.State == "busy":
			nbusy++
		}
	}
	tabs := []string{"tree", "inbox", "search"}
	for i, t := range tabs {
		if viewKind(i) == m.view {
			tabs[i] = stAccent.Render("[" + t + "]")
		} else {
			tabs[i] = stDim.Render(" " + t + " ")
		}
	}
	s := stBold.Render("regia") + "  " + strings.Join(tabs, "") + "   " +
		lipgloss.NewStyle().Foreground(cBlocked).Render(fmt.Sprintf("● %d need you", nb)) + "  " +
		lipgloss.NewStyle().Foreground(cReady).Render(fmt.Sprintf("✓ %d ready", nready)) + "  " +
		lipgloss.NewStyle().Foreground(cBusy).Render(fmt.Sprintf("◆ %d busy", nbusy)) + "  " +
		stDim.Render(fmt.Sprintf("%d live", n))
	if narch > 0 {
		arch := fmt.Sprintf("%d archived", narch)
		if m.opts.ShowArchived {
			s += "  " + stAccent.Render("["+arch+"]")
		} else {
			s += "  " + stDim.Render(arch)
		}
	}
	if m.opts.Filter != "" && m.view != viewSearch {
		s += "  " + stAccent.Render("/"+m.opts.Filter)
	}
	if m.updateNote != "" {
		s += "  " + stAccent.Render("⬆ "+m.updateNote)
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
	case modeDir:
		line1 = m.dirPickPrompt()
	case modeModel:
		line1 = m.modelPickPrompt()
	case modeConfirm:
		line1 = lipgloss.NewStyle().Foreground(cBlocked).Render(m.confirmMsg)
	default:
		if time.Since(m.statusAt) < 6*time.Second {
			line1 = m.status
		}
	}
	status := pad(strings.Repeat(" ", padX)+trunc(line1, m.w-2*padX), m.w)
	if m.mode == modeNormal && m.view == viewSearch {
		status = pad(strings.Repeat(" ", padX)+m.searchLine(m.w-2*padX), m.w)
	}
	if m.dirPickRows() > 0 {
		status += "\n" + m.dirPickView()
	} else if m.modelPickRows() > 0 {
		status += "\n" + m.modelPickView()
	} else if pop := m.compView(); pop != "" {
		status += "\n" + pop
	}
	return status + "\n" + m.inputBox()
}

// rowHeight is how many lines a list row takes: two in the inbox (the agent,
// then where it sits and what it asks), one in the tree.
func (m *model) rowHeight() int {
	if m.view == viewInbox {
		return 2
	}
	return 1
}

// renderRow draws a row as rowHeight lines.
func (m *model) renderRow(r layout.Row, w int) []string {
	if m.view == viewSearch {
		return []string{m.searchRowLine(r, w)}
	}
	line := m.rowLine(r, w)
	if m.rowHeight() == 1 {
		return []string{line}
	}
	return []string{line, m.inboxSubline(r, w)}
}

// inboxSubline is an inbox item's second line, under its title: the group
// and coordinator it sits under, then what it is waiting on.
func (m *model) inboxSubline(r layout.Row, w int) string {
	a := r.Agent
	if r.ID == earlierID {
		return m.earlierSubline(r)
	}
	if a == nil {
		return ""
	}
	indent := "    " // under the title, past fold and glyph
	if r.Depth > 0 {
		indent += strings.Repeat("  ", r.Depth)
	}
	what := stripNL(a.Needs)
	if a.State == "busy" && a.Unhealthy != "" {
		what = stuckGlyph + " " + a.Unhealthy
	}
	if what == "" {
		what = stripNL(a.Detail)
	}
	room := w - len(indent) - 1
	place := ""
	if n := len(r.Where); n > 0 { // the outermost group, and the nearest coordinator if it fits
		limit := max(8, room*2/5)
		place = r.Where[0]
		if n > 1 {
			if both := place + " › " + r.Where[n-1]; runewidth.StringWidth(both) <= limit {
				place = both
			}
		}
		place = trunc(place, limit)
		if what != "" {
			place += " · "
		}
	}
	prWhy := ""
	if a.State != "blocked" && prNeedsLook(a) {
		prWhy = "⚠ " + prFor(a).Reason()
		what = prWhy
	} else if why, label := pluginAttention(a); a.State != "blocked" && why != "" {
		prWhy = "⚠ " + label + ": " + stripNL(why)
		what = prWhy
	}
	what = trunc(what, room-runewidth.StringWidth(place))
	st, wst := stDim, lipgloss.NewStyle().Foreground(cDim)
	if (a.YourTurn() || prWhy != "") && !r.Archived {
		st = lipgloss.NewStyle().Foreground(cAccent)
		wst = lipgloss.NewStyle()
	}
	if prWhy != "" && !r.Archived {
		wst = lipgloss.NewStyle().Foreground(cFailed)
	}
	return indent + st.Render(place) + wst.Render(what)
}

// faded reports whether a row's title goes gray: a question from before today
// or snoozed, or finished (done or failed) before today. Today's finished rows
// stay white so today's chats are easy to spot.
func faded(a *claude.Agent, now time.Time) bool {
	if a.Stale || a.Snoozed {
		return true
	}
	return (a.State == "done" || a.State == "failed") && !fromToday(a, now)
}

// rowKey is everything a list row's line depends on (see drawRowLine):
// drawing it with lipgloss is most of a frame, and between frames rows rarely
// change but for the pulsing icon's color, which is part of the key.
type rowKey struct {
	id, title, mark, age               string
	depth, count, blocked, busy, w     int
	group, kids, collapsed, archived   bool
	muted, inbox                       bool
	glyph                              string
	color                              lipgloss.Color
	cut, pin, yourTurn, faded, snoozed bool
	unhealthy                          bool
	badges                             string
}

var rowCache = map[rowKey]string{}

const rowCacheMax = 4096

func (m *model) rowLine(r layout.Row, w int) string {
	if m.editingRow(r.ID) != nil { // the cursor moves with each key: not cached
		return m.drawRowLine(r, w)
	}
	k := rowKey{id: r.ID, title: r.Title, mark: m.markFor(r.ID), depth: r.Depth, count: r.Count, blocked: r.Blocked,
		busy: r.Busy, w: w, group: r.Group, kids: r.HasKids, collapsed: r.Collapsed, archived: r.Archived,
		muted: m.layout.Muted[r.ID], inbox: m.view == viewInbox}
	if a := r.Agent; !r.Group && a != nil {
		k.glyph, k.color = m.agentGlyph(a)
		k.age = ago(waitedSince(a))
		k.cut = slices.Contains(m.cut, r.ID)
		k.pin = m.layout.Pins[r.ID]
		k.yourTurn, k.faded, k.snoozed = a.YourTurn(), faded(a, time.Now()), a.Snoozed
		k.unhealthy = a.Unhealthy != ""
		k.badges = pluginBadges(a.ID)
	}
	if l, ok := rowCache[k]; ok {
		return l
	}
	if len(rowCache) >= rowCacheMax {
		clear(rowCache)
	}
	l := m.drawRowLine(r, w)
	rowCache[k] = l
	return l
}

func (m *model) drawRowLine(r layout.Row, w int) string {
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
		mark := m.markFor(r.ID)
		muted := ""
		if m.layout.Muted[r.ID] {
			muted = " (muted)"
		}
		titleW := w - len(indent) - lipgloss.Width(meta) - 4 - lipgloss.Width(mark)
		title := stHead.Render(trunc(r.Title+muted, titleW))
		if e := m.editingRow(r.ID); e != nil {
			title = e.view(titleW)
		}
		line = indent + stDim.Render(fold) + mark + title
		line = pad(line, w-lipgloss.Width(meta)-1) + meta
	} else {
		a := r.Agent
		g, c := m.agentGlyph(a)
		age := ago(waitedSince(a))
		cutMark := ""
		if slices.Contains(m.cut, r.ID) {
			cutMark = "✂ "
		}
		extra := ""
		if r.Collapsed && r.Blocked > 0 && m.view != viewInbox {
			extra = fmt.Sprintf("  (+%d your turn)", r.Blocked)
		}
		if m.layout.Muted[r.ID] {
			extra += "  (muted)"
		}
		if a.Snoozed {
			extra += "  (snoozed)"
		}
		mark := m.markFor(r.ID)
		pin := ""
		if m.layout.Pins[r.ID] {
			pin = stAccent.Render(pinGlyph) + " "
		}
		warn := ""
		if a.Unhealthy != "" && !r.Archived {
			warn = " " + stDim.Render(stuckGlyph)
		}
		if b := pluginBadges(a.ID); b != "" && !r.Archived {
			warn += " " + b
		}
		titleW := w - len(indent) - 6 - len(age) - len(cutMark) - lipgloss.Width(mark) - lipgloss.Width(pin) - lipgloss.Width(warn)
		title := trunc(r.Title+stripNL(extra), titleW)
		ts := lipgloss.NewStyle()
		if a.YourTurn() {
			ts = ts.Bold(true)
		} else if faded(a, time.Now()) {
			ts = ts.Foreground(cDim)
		}
		gs := lipgloss.NewStyle().Foreground(c)
		if r.Archived {
			ts, gs = lipgloss.NewStyle().Foreground(cDim).Italic(true), stDim
		}
		title = ts.Render(title)
		if e := m.editingRow(r.ID); e != nil {
			title = e.view(titleW)
		}
		line = indent + stDim.Render(fold) + mark + gs.Render(g) + " " + cutMark + pin + title + warn
		line = pad(line, w-len(age)-1) + stDim.Render(age)
	}
	return line // the selection bar is drawn by View, across the margins
}

// selBar paints the selection background across a styled line.
func selBar(line string, w int) string { return bgBar(stSel, line, w) }

// bgBar paints st's background across a styled line, w wide: every style
// reset inside it would otherwise end the background mid-row.
func bgBar(st lipgloss.Style, line string, w int) string {
	line = pad(line, w)
	r := st.Render("x")
	bg := r[:strings.Index(r, "x")]
	if bg == "" { // no colors (not a terminal)
		return line
	}
	return bg + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bg) + "\x1b[0m"
}

// scrollOff is how many rows of context stay visible above and below the
// selection while scrolling the list.
const scrollOff = 3

// sidebarWidth is the list's width: what you set with -/+ or by dragging the
// divider, else about a third of the screen.
func (m *model) sidebarWidth(iw int) int {
	if m.w < 70 {
		return iw
	}
	w := m.layout.Sidebar
	if w == 0 {
		w = min(48, max(28, iw*30/100))
	}
	return max(minSidebar, min(w, iw-gap-30)) // leave the panel 30 columns
}

const minSidebar = 20

func stripNL(s string) string { return strings.ReplaceAll(s, "\n", " ") }

func wrap(s string, w int) []string {
	if s == "" {
		return nil
	}
	return strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
}

// panel is what the right side shows: for an agent, a card pinned at the top,
// a foot pinned at the bottom, and between them the conversation, which
// scrolls, stuck to the foot (bottomUp); for a group, plain lines.
type panel struct {
	hist, card, foot []string
	bottomUp         bool
}

func (p panel) lines() []string {
	return append(append(append([]string{}, p.card...), p.hist...), p.foot...)
}

// placePanel fits the panel into h rows. Bottom-up panels pin the card at the
// top and the foot at the bottom, with the history stuck right above the foot;
// the history scrolls by panelScroll
// lines (pgup/pgdn), which goes back to 0 when another row is selected.
func (m *model) placePanel(p panel, h, w int) []string {
	if !p.bottomUp {
		return p.card[:min(h, len(p.card))]
	}
	fresh := m.panelFor != m.cursorID
	if fresh {
		m.panelFor, m.panelScroll = m.cursorID, 0
	}
	rule := stDim.Render(strings.Repeat("─", w))
	var foot []string
	if len(p.foot) > 0 { // a rule between the history and the foot
		foot = append([]string{rule}, p.foot...)
	}
	if len(foot) > h { // tiny panel: keep the state line, at the foot's top
		foot = foot[1:min(len(foot), h+1)]
	}
	card := append([]string{}, p.card...)
	if len(card) > 0 && len(p.hist) > 0 { // a rule between the card and the history
		card = append(card, rule)
	}
	card = card[:min(len(card), h-len(foot))]
	room := h - len(card) - len(foot)
	m.revealConv(len(p.hist), room)
	m.panelScroll = max(0, min(m.panelScroll, len(p.hist)-room))
	end := len(p.hist) - m.panelScroll
	start := max(0, end-room)
	view := append([]string{}, p.hist[start:end]...)
	if room > 2 {
		if start > 0 {
			view[0] = stDim.Render(fmt.Sprintf("↑ %d earlier · pgup", start))
		}
		if m.panelScroll > 0 {
			view[len(view)-1] = stDim.Render(fmt.Sprintf("↓ %d newer · pgdn", m.panelScroll))
		}
	}
	out := append(make([]string, 0, h), card...)
	for len(out)+len(view)+len(foot) < h {
		out = append(out, "")
	}
	m.conv.histRow, m.conv.histStart, m.conv.histEnd = len(out), start, end // for clicks
	return append(append(out, view...), foot...)
}

func (m *model) detailLines(w int) panel {
	r := m.selected()
	if r == nil {
		return panel{}
	}
	if r.ID == earlierID {
		return m.earlierPanel(w)
	}
	var out []string
	sec := func(t string) { out = append(out, "", stHead.Render(t)) }
	if c := m.timelineTarget(r); c != "" {
		return m.timelineView(r, w)
	}
	if r.Group {
		// nothing in it pulses: drawn again only when something else happened
		if c := &m.groupPanel; c.id == r.ID && c.w == w && c.gen == m.gen {
			return panel{card: c.lines}
		}
		defer func() { m.groupPanel.id, m.groupPanel.w, m.groupPanel.gen, m.groupPanel.lines = r.ID, w, m.gen, out }()
		out = append(out, stBold.Render(r.Title), stDim.Render(fmt.Sprintf("%d agents · %d your turn · %d busy", r.Count, r.Blocked, r.Busy)),
			trunc(m.groupDirLine(r.ID), w))
		sec("your turn")
		n := 0
		for _, rr := range m.layout.Flatten(m.agents, layout.ViewOpts{ShowDone: false}) {
			if a := rr.Agent; a != nil && a.YourTurn() && m.layout.IsDescendant(rr.ID, r.ID) {
				g, c := glyph(a)
				what := a.Needs
				if a.State == "ready" {
					what = "finished: " + a.Detail
				}
				out = append(out, lipgloss.NewStyle().Foreground(c).Render(g+" ")+trunc(rr.Title+" — "+stripNL(what), w-2))
				n++
			}
		}
		if n == 0 {
			out = append(out, stDim.Render("nothing"))
		}
		if group.IsGroup(r.ID) {
			out = append(out, m.notesSection(r.ID, w, 10)...)
		}
		return panel{card: out}
	}
	m.tabHits, m.tabRow = m.tabHits[:0], 0 // agentDetail draws them again if it has tabs
	m.conv.id = ""                         // agentDetail sets it when it shows the conversation
	hist, card, foot := m.agentDetail(r, w)
	// a coordinator's panel is like any agent's; its agents' blackboard shows
	// atop the card only while open (N)
	if r.HasKids && m.notesFor == r.ID {
		if ns := m.notesSection(r.ID, w, 5); len(ns) > 0 {
			card = append(append(ns[1:], ""), card...)
			m.tabRow = len(ns) // the tab bar moved down by the notes
		}
	}
	return panel{hist: hist, card: card, foot: foot, bottomUp: true}
}
