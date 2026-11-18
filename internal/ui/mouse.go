package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// listTop is the screen row of the list's first line (header + blank).
const listTop = 2

// updateMouse: the wheel scrolls the list or the panel (whichever is under
// the pointer), a click selects a row, a tab of the agent box, a file of the
// changes tab or a block of the conversation (a click on the selected one
// opens it, or folds / unfolds it), and dragging the divider resizes the
// list. (Holding shift lets the terminal select text as usual.)
func (m *model) updateMouse(e tea.MouseMsg) tea.Cmd {
	if m.help || m.mode != modeNormal {
		return nil
	}
	divider := padX + m.lw + gap/2
	onList := e.X < divider-1
	switch {
	case m.dragging:
		switch e.Action {
		case tea.MouseActionMotion:
			m.setSidebar(e.X - padX - gap/2)
		case tea.MouseActionRelease:
			m.dragging = false
			m.resizeSidebar(m.layout.Sidebar)
		}
		return nil
	case e.Button == tea.MouseButtonWheelUp:
		if onList {
			m.moveCursor(-3)
		} else {
			m.scrollConv(3)
		}
	case e.Button == tea.MouseButtonWheelDown:
		if onList {
			m.moveCursor(3)
		} else {
			m.scrollConv(-3)
		}
	case e.Action == tea.MouseActionRelease:
		// A click in the panel acts on the release, where it went down (not
		// after a drag): a terminal opening a link on cmd/ctrl+click swallows
		// that release (Ghostty does), while it still sends the press. Acting on
		// the press would select, unfold or scroll, moving the link away from
		// the pointer before the terminal opened it.
		if m.pressed {
			m.pressed = false
			if e.X == m.pressX && e.Y == m.pressY {
				return m.clickPanel(e.X-(padX+m.lw+gap), e.Y-listTop)
			}
		}
	case e.Button == tea.MouseButtonLeft && e.Action == tea.MouseActionPress:
		m.pressed = false
		if e.X >= divider-1 && e.X <= divider+1 && e.Y >= listTop {
			m.dragging = true
			return nil
		}
		if !onList { // the click happens on the release
			m.pressed, m.pressX, m.pressY = true, e.X, e.Y
			return nil
		}
		if rh := m.rowHeight(); onList && e.Y >= listTop && e.Y < listTop+m.vis*rh {
			if i := m.offset + (e.Y-listTop)/rh; i < len(m.rows) {
				m.moveCursor(i - m.cursor)
			}
		}
	}
	return nil
}

// clickPanel handles a click at column x, row y of the panel.
func (m *model) clickPanel(x, y int) tea.Cmd {
	if m.view == viewSearch { // its panel is the results: what the last agent frame drew is gone
		return nil
	}
	if y == m.tabRow { // a tab on the agent box's top line
		for _, t := range m.tabHits {
			if x >= t.x0 && x < t.x1 {
				if t.name != m.panelTab {
					m.chg.focus = false
				}
				m.panelTab = t.name
				return nil
			}
		}
	}
	if c, ok := m.clickChanges(y - m.tabRow - 1); ok {
		return c
	}
	i := m.convBlockAtRow(y)
	if i < 0 {
		return nil
	}
	b := m.conv.blocks[i]
	if b.kind == blockStep && m.convFocus && i == m.convSel() { // the selected step: its action
		return m.activateConv(b)
	}
	if !m.convFocus || i != m.convSel() {
		m.flash(convHint)
	}
	m.selectConvBlock(i)
	m.convFocus, m.chg.focus = true, false
	runLine := b.run != nil && b.kind != blockStep && m.conv.histStart+y-m.conv.histRow == b.top
	if b.kind == blockStep || runLine { // the steps fold / unfold on a click; a message's text only selects
		m.toggleConv(b)
	}
	return nil
}

// scrollConv scrolls the panel's conversation d lines back (d < 0: forward).
// In focus, the cursor stays on screen: when its block scrolls out, the
// nearest one still shown is selected.
func (m *model) scrollConv(d int) {
	m.panelScroll = max(0, m.panelScroll+d)
	c := &m.conv
	if !m.convFocus || c.id != m.cursorID || len(c.blocks) == 0 || c.histEnd <= c.histStart {
		return
	}
	// what the next frame shows, from what the last one did
	room := c.histEnd - c.histStart
	n := c.histEnd + (m.panelScroll - d) // all the lines: the end + how far back it was
	m.panelScroll = max(0, min(m.panelScroll, n-room))
	end := n - m.panelScroll
	start := max(0, end-room)
	i := m.convSel()
	b := c.blocks[i]
	if b.top < end && b.top+b.n > start {
		return
	}
	pick := -1
	for j, x := range c.blocks {
		if x.top < end && x.top+x.n > start {
			if pick < 0 || b.top >= end { // below the view: the last shown; above: the first
				pick = j
			}
		}
	}
	if pick >= 0 {
		m.convOf(c.id).sel = c.blocks[pick].key
	}
}

// setSidebar sets the list width for this session (while dragging).
func (m *model) setSidebar(w int) {
	m.layout.Sidebar = max(minSidebar, min(w, m.w-2*padX-gap-30))
}

// resizeSidebar sets the list width and remembers it (layout.json).
func (m *model) resizeSidebar(w int) {
	m.setSidebar(w)
	m.flash("list width %d · - / + or drag the divider", m.layout.Sidebar)
	m.saveLayout()
}
