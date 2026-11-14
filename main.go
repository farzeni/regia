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

	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--dump":
			dump()
			return
		case "--panel": // hidden: print one agent's detail panel as plain text
			if len(os.Args) < 3 {
				return
			}
			m := newModel()
			if b, err := fetchAgents(); err == nil {
				m.setAgents(b)
			}
			m.w, m.h = 160, 60
			m.cursorID = os.Args[2]
			m.layout.Collapsed = map[string]bool{} // in memory only: rebuild never saves
			m.rebuild()
			if r := m.selected(); r != nil && r.Agent != nil {
				if c := gitCmd(r.Agent.Cwd); c != nil {
					c()
				}
			}
			if r := m.selected(); r == nil || r.ID != os.Args[2] {
				fmt.Fprintln(os.Stderr, "not visible:", os.Args[2])
				os.Exit(1)
			}
			for _, l := range m.detailLines(90) {
				fmt.Println(l)
			}
			return
		case "--attach-test": // hidden: run any command through the attach logic
			t0 := time.Now()
			err := (&attachCmd{argv: os.Args[2:]}).Run()
			_, _ = os.Stdout.WriteString("\x1b[?1049l")
			fmt.Printf("ATTACH-RETURNED err=%v after=%dms\n", err, time.Since(t0).Milliseconds())
			return
		case "setup":
			os.Exit(setup(os.Args[2:]))
		case "hook": // called by the plugin's hook in every session; must stay quiet and fast
			if len(os.Args) > 2 && os.Args[2] == "spawn" {
				hookSpawn(os.Stdin, os.Getenv("CLAUDE_JOB_DIR"))
			}
			return
		case "-h", "--help":
			fmt.Println("regia — terminal control room for Claude Code background agents\n\n  regia          open the UI (? for keys)\n  regia setup    install the Claude Code plugin (spawn tracking) and backfill links\n  regia --dump   print the tree and inbox as plain text")
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
