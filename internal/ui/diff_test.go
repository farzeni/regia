package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCleanDropsControls(t *testing.T) {
	if got := clean("+\tx\x1b[31mred\r"); got != "+    x[31mred" {
		t.Fatalf("%q", got)
	}
}

// deliver runs cmd (a batch too) and feeds its messages back to m.
func deliver(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			deliver(m, c)
		}
	case nil:
	default:
		m.Update(msg)
	}
}
