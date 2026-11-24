package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/farzeni/regia/internal/group"
)

const swapUsage = `usage: regia swap <old-id> <new-id> --reason "…" [--note <path or URL>] [--answer]

The new session (started beforehand, e.g. with claude --bg) takes the old one's
name, place in the tree, pin and children; the old one is removed (claude rm).
The row keeps "swapped from <old>" with the note. Plugins with "gates": ["swap"]
may refuse. It runs as you: logged under U, it can't be undone.

The old session must be done, idle or failed. --answer also lets one that is
waiting on you go: the new session carries your answer (logged under U as
answered by swap).`

// regia swap <old> <new> --reason … [--note …]: a session swap from scripts
// (agentd, swarm.sh), run as you.
func cmdSwap(args []string, w io.Writer) error {
	var ids []string
	var reason, note string
	answer := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		key, val, inline := strings.Cut(a, "=")
		switch key {
		case "--reason", "--note":
			if !inline {
				if i+1 >= len(args) {
					return fmt.Errorf("%s needs a value\n\n%s", key, swapUsage)
				}
				i++
				val = args[i]
			}
			if key == "--reason" {
				reason = val
			} else {
				note = val
			}
		case "--answer":
			answer = true
		case "-h", "--help":
			fmt.Fprintln(w, swapUsage)
			return nil
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag %q\n\n%s", a, swapUsage)
			}
			ids = append(ids, a)
		}
	}
	if len(ids) != 2 {
		return fmt.Errorf("%s", swapUsage)
	}
	swap := group.SwapAsYou
	if answer {
		swap = group.SwapAnswer
	}
	out, err := swap(ids[0], ids[1], reason, note)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, out)
	return nil
}
