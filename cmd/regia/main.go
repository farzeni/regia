// Command regia is a fast terminal control room for Claude Code background agents.
//
// Data comes from `claude agents --json --all` (supported) plus read-only job
// state files in ~/.claude/jobs. Attaching uses `claude attach`; the user's own
// organization lives in ~/.config/regia/layout.json.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/farzeni/regia/internal/attach"
	"github.com/farzeni/regia/internal/spawns"
	"github.com/farzeni/regia/internal/ui"
)

const usage = `regia — terminal control room for Claude Code background agents

  regia          open the UI (? for keys)
  regia setup    install the Claude Code plugin (spawn tracking) and backfill links
  regia --dump   print the tree and inbox as plain text`

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(args []string) error {
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "":
		return ui.Run()
	case "--dump":
		return ui.DumpTree(os.Stdout)
	case "setup":
		os.Exit(setup(args[1:]))
	case "hook": // called by the plugin's hook in every session; must stay quiet and fast
		if len(args) > 1 && args[1] == "spawn" {
			spawns.HandleHook(os.Stdin, os.Getenv("CLAUDE_JOB_DIR"))
		}
	case "--panel": // hidden: print one agent's detail panel as plain text
		if len(args) < 2 {
			return fmt.Errorf("usage: regia --panel <id>")
		}
		return ui.DumpPanel(os.Stdout, args[1])
	case "--attach-test": // hidden: run any command through the attach logic
		t0 := time.Now()
		err := attach.Command(args[1:]...).Run()
		_, _ = os.Stdout.WriteString("\x1b[?1049l")
		fmt.Printf("ATTACH-RETURNED err=%v after=%dms\n", err, time.Since(t0).Milliseconds())
	case "-h", "--help", "help":
		fmt.Println(usage)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
	return nil
}
