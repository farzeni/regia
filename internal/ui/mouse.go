package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// listTop is the screen row of the list's first line (header + blank).
const listTop = 2

// updateMouse: the wheel scrolls the list or the panel (whichever is under
// the pointer), a click selects a row, and dragging the divider resizes the
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
			m.panelScroll += 3
		}
	case e.Button == tea.MouseButtonWheelDown:
		if onList {
			m.moveCursor(3)
		} else {
			m.panelScroll = max(0, m.panelScroll-3)
		}
	case e.Button == tea.MouseButtonLeft && e.Action == tea.MouseActionPress:
		if e.X >= divider-1 && e.X <= divider+1 && e.Y >= listTop {
			m.dragging = true
			return nil
		}
		if onList && e.Y >= listTop && e.Y < listTop+m.vis {
			if i := m.offset + e.Y - listTop; i < len(m.rows) {
				m.moveCursor(i - m.cursor)
			}
		}
	}
	return nil
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
