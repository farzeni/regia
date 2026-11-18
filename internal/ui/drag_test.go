package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Switching agents (here with the wheel over the list, which goes through
// neither a key of the conversation nor a click) drops the dragged selection.
func TestDragClearedOnSwitch(t *testing.T) {
	m := convModel(t)
	prev := copyText
	copyText = func(string) error { return nil }
	t.Cleanup(func() { copyText = prev })
	y := -1
	for row, l := range strings.Split(convScreen(m), "\n") {
		if strings.Contains(l, "Done: tests fixed.") {
			y = row - listTop
		}
	}
	x0 := padX + m.lw + gap
	ev := func(a tea.MouseAction, x int) {
		m.Update(tea.MouseMsg{X: x0 + x, Y: listTop + y, Button: tea.MouseButtonLeft, Action: a})
	}
	ev(tea.MouseActionPress, 7)
	ev(tea.MouseActionMotion, 17)
	ev(tea.MouseActionRelease, 17)
	if !m.conv.drag.on {
		t.Fatal("no selection")
	}
	id := m.cursorID
	m.Update(tea.MouseMsg{X: 3, Y: listTop + 1, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m.View()
	if m.cursorID == id {
		t.Fatal("the wheel should move to another row")
	}
	if m.conv.drag.on {
		t.Fatal("another agent: the selection is dropped")
	}
}
