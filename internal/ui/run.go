package ui

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/claude"
)

// Run opens the full-screen control room.
func Run() error {
	_, err := tea.NewProgram(newModel(), tea.WithAltScreen()).Run()
	return err
}

// DumpTree prints the organized tree and the inbox as plain text (no TTY).
func DumpTree(w io.Writer) error {
	b, err := claude.FetchAgents()
	if err != nil {
		return fmt.Errorf("claude agents --json failed: %w", err)
	}
	m := newModel()
	m.setAgents(b)
	for _, r := range m.rows {
		ind := strings.Repeat("  ", r.Depth)
		if r.Group {
			fmt.Fprintf(w, "%s▾ %s  (%d, %d need you)\n", ind, r.Title, r.Count, r.Blocked)
			continue
		}
		g, _ := glyph(r.Agent)
		fmt.Fprintf(w, "%s%s %-8s %s  [%s]\n", ind, g, r.Agent.State, r.Title, r.ID)
	}
	m.view = viewInbox
	m.rebuild()
	fmt.Fprintf(w, "\ninbox (%d):\n", len(m.rows))
	for _, r := range m.rows {
		fmt.Fprintf(w, "  ● %s — %s\n", r.Title, trunc(r.Agent.Needs, 100))
	}
	return nil
}

// DumpPanel prints one agent's detail panel as plain text.
func DumpPanel(w io.Writer, id string) error {
	m := newModel()
	if b, err := claude.FetchAgents(); err == nil {
		m.setAgents(b)
	}
	m.w, m.h = 160, 60
	m.cursorID = id
	m.layout.Collapsed = map[string]bool{} // in memory only: rebuild never saves
	m.rebuild()
	if r := m.selected(); r != nil && r.Agent != nil {
		if c := gitCmd(r.Agent.Cwd); c != nil {
			c()
		}
	}
	if r := m.selected(); r == nil || r.ID != id {
		return fmt.Errorf("not visible: %s", id)
	}
	for _, l := range m.detailLines(90).lines() {
		fmt.Fprintln(w, l)
	}
	return nil
}
