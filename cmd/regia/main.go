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
	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/spawns"
	"github.com/farzeni/regia/internal/ui"
)

const usage = `regia — terminal control room for Claude Code background agents

  regia            open the UI (? for keys)
  regia setup      install the Claude Code plugin (spawn tracking) and backfill links
  regia update     install the latest version and update the plugin (also daily, automatically)
  regia version    print this build's version
  regia uninstall  remove the plugin and the binary (--purge: also your layout)
  regia --dump     print the tree and inbox as plain text
  regia plugin     list | new | input | run: your own scripts on the agent panel (docs/plugins.md)
  regia openers    list | show | test: what opens files, images, URLs and diffs (README "Openers")

  group awareness (agents in a hand-made group or under a coordinator):
  regia context    run by an agent: its group, siblings' state and shared notes (--all: every note)
  regia note "…"   run by an agent: share one line with its group
  regia group      on | off | status: turn it on or off for every session (REGIA_GROUP=off: one session)`

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
	case "update":
		return cmdUpdate(args[1:])
	case "version", "--version", "-v":
		cmdVersion()
	case "uninstall":
		return cmdUninstall(args[1:])
	case "hook": // called by the plugin's hooks in every session; must stay quiet and fast
		if len(args) > 1 {
			switch args[1] {
			case "spawn":
				spawns.HandleHook(os.Stdin, os.Getenv("CLAUDE_JOB_DIR"))
			case "group":
				group.HandleHook(os.Stdin, os.Stdout, os.Getenv("CLAUDE_JOB_DIR"), time.Now())
			}
		}
	case "context":
		return cmdContext(args[1:], os.Stdout)
	case "note":
		return cmdNote(args[1:], os.Stdout)
	case "group":
		return cmdGroup(args[1:], os.Stdout)
	case "plugin", "plugins":
		return cmdPlugin(args[1:], os.Stdout)
	case "openers", "opener":
		return ui.Openers(args[1:], os.Stdout)
	case "--panel": // hidden: print one agent's detail panel as plain text
		if len(args) < 2 {
			return fmt.Errorf("usage: regia --panel <id>")
		}
		return ui.DumpPanel(os.Stdout, args[1])
	case "--timeline": // hidden: print a group's or coordinator's timeline as plain text
		if len(args) < 2 {
			return fmt.Errorf("usage: regia --timeline <id>")
		}
		return ui.DumpTimeline(os.Stdout, args[1])
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
