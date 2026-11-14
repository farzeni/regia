// regia: a fast terminal control room for Claude Code background agents.
//
// Data comes from `claude agents --json --all` (supported) plus read-only job
// state files in ~/.claude/jobs. Attaching uses `claude attach`; the user's own
// organization lives in ~/.config/regia/layout.json.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--dump":
			dump()
			return
		case "-h", "--help":
			fmt.Println("regia — terminal control room for Claude Code background agents\n\n  regia          open the UI (? for keys)\n  regia --dump   print the tree and inbox as plain text")
			return
		}
	}
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// dump prints the organized tree without a TTY (scripting, testing).
func dump() {
	b, err := fetchAgents()
	if err != nil {
		fmt.Fprintln(os.Stderr, "claude agents --json failed:", err)
		os.Exit(1)
	}
	m := newModel()
	m.setAgents(b)
	for _, r := range m.rows {
		ind := strings.Repeat("  ", r.Depth)
		if r.Group {
			fmt.Printf("%s▾ %s  (%d, %d need you)\n", ind, r.Title, r.Count, r.Blocked)
			continue
		}
		g, _ := glyph(r.Agent)
		fmt.Printf("%s%s %-8s %s  [%s]\n", ind, g, r.Agent.State, r.Title, r.ID)
	}
	m.view = viewInbox
	m.rebuild()
	fmt.Printf("\ninbox (%d):\n", len(m.rows))
	for _, r := range m.rows {
		fmt.Printf("  ● %s — %s\n", r.Title, trunc(r.Agent.Needs, 100))
	}
}
