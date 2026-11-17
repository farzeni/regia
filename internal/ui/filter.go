package ui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// The quick filter (`/`, ctrl+f in the input) narrows the list as you type.
// While it is open the arrows move among the matches and enter opens the
// highlighted one, so finding a session is: type, move, enter.

// filterKey handles the filter prompt's own keys; ok is false for the keys
// the prompt line handles as usual.
func (m *model) filterKey(k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "up", "ctrl+p", "ctrl+k":
		m.moveMatch(-1)
	case "down", "ctrl+n", "ctrl+j":
		m.moveMatch(1)
	case "enter": // keep the filter and open the highlighted match
		m.mode = modeNormal
		m.opts.Filter = m.input
		m.rebuild()
		return m.updateNormal(tea.KeyMsg{Type: tea.KeyEnter}), true
	case "ctrl+w", "alt+backspace":
		q := strings.TrimRightFunc(m.input, unicode.IsSpace)
		m.input = q[:strings.LastIndexFunc(q, unicode.IsSpace)+1]
		m.filterEdited()
	case "backspace":
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
		m.filterEdited()
	case "ctrl+u":
		m.input = ""
		m.filterEdited()
	default:
		switch k.Type {
		case tea.KeySpace:
			m.input += " "
		case tea.KeyRunes:
			m.input += string(k.Runes)
		default:
			return nil, false
		}
		m.filterEdited()
	}
	return nil, true
}

// filterEdited applies the filter and puts the selection on the first match.
func (m *model) filterEdited() {
	m.opts.Filter = m.input
	m.rebuild()
	m.cursor = -1
	m.moveMatch(1)
}

// isMatch: the row is an agent the filter itself matches, not an ancestor
// shown only to place one.
func (m *model) isMatch(i int) bool {
	a := m.rows[i].Agent
	return a != nil && m.opts.Visible(a, m.layout.Alias[a.ID], m.layout.Pins[a.ID])
}

// moveMatch moves the selection to the next (d=1) or previous (d=-1) match.
func (m *model) moveMatch(d int) {
	for i := m.cursor + d; i >= 0 && i < len(m.rows); i += d {
		if m.isMatch(i) {
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.moveCursor(i - m.cursor)
			return
		}
	}
	if m.cursor < 0 { // no match: back to the top
		m.cursor = 0
		m.moveCursor(0)
	}
}
