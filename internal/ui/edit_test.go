package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The input understands the usual readline shortcuts, and keys that are list
// commands on an empty input edit the text when there is text.
func TestInputReadlineKeys(t *testing.T) {
	m := testModel(t)
	k := func(t tea.KeyType, alt bool) { m.Update(tea.KeyMsg{Type: t, Alt: alt}) }
	want := func(step, s string) {
		t.Helper()
		if m.draft() != s {
			t.Fatalf("%s: draft %q, want %q", step, m.draft(), s)
		}
	}

	typeText(m, "fix the flaky test now")
	k(tea.KeyBackspace, true) // alt+backspace
	want("alt+backspace", "fix the flaky test ")
	k(tea.KeyCtrlW, false)
	want("ctrl+w", "fix the flaky ")

	k(tea.KeyCtrlA, false) // start of line, then type
	typeText(m, "please ")
	want("ctrl+a + type", "please fix the flaky ")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}, Alt: true}) // alt+d: delete next word
	want("alt+d", "please  the flaky ")
	k(tea.KeyCtrlE, false)
	typeText(m, "build")
	want("ctrl+e + type", "please  the flaky build")

	k(tea.KeyLeft, true) // alt+←: word back
	k(tea.KeyCtrlK, false)
	want("alt+← ctrl+k", "please  the flaky ")
	k(tea.KeyHome, false)
	k(tea.KeyDelete, false)
	want("home delete", "lease  the flaky ")
	sel := m.cursorID
	k(tea.KeyCtrlD, false) // text present: delete forward, not half page
	want("ctrl+d", "ease  the flaky ")
	if m.cursorID != sel {
		t.Fatal("ctrl+d moved the list while editing")
	}
	k(tea.KeyEnd, false)
	k(tea.KeyCtrlU, false)
	want("ctrl+u", "")

	// empty input: ←/→ home/end act on the list again
	k(tea.KeyEnd, false)
	if m.cursor != len(m.rows)-1 {
		t.Fatalf("end on an empty input should go to the last row (cursor %d of %d)", m.cursor, len(m.rows))
	}
}
