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
	m := newModel()
	if e := recoverDrafts(); e != nil { // a regia that died while you typed
		m.showEntry(*e)
		m.typing = true
		m.flash("brought back what you were typing when regia closed · esc clears it (pgup brings it back)")
	}
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if fm, ok := final.(*model); ok && fm != nil {
		fm.finishDraft() // what's in the input goes into the history
	} else {
		m.finishDraft()
	}
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
	m.earlierOpen = true // plain text: list everything
	m.rebuild()
	fmt.Fprintf(w, "\ninbox (%d):\n", len(m.rows)-btoi(len(m.earlier) > 0))
	for _, r := range m.rows {
		if r.ID == earlierID {
			fmt.Fprintf(w, "  ▾ earlier days — %s\n", m.earlierSummary())
			continue
		}
		fmt.Fprintf(w, "%s  ● %s — %s\n", strings.Repeat("  ", r.Depth), r.Title, trunc(r.Agent.Needs, 100))
	}
	return nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
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
		if c := prCmd(r.Agent); c != nil {
			c()
		}
		for _, f := range m.pluginRefreshers(r.Agent) {
			f()
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
