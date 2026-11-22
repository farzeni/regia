package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/group"
)

// regia context [id]: what an agent's group looks like. Inside a Claude Code
// session the agent is found from the environment.
func cmdContext(args []string, w io.Writer) error {
	self := group.SelfFromEnv()
	for _, a := range args {
		switch {
		case a == "--all": // showed every note; sessions started before may still pass it
		case !strings.HasPrefix(a, "-"):
			self = a
		default:
			return fmt.Errorf("usage: regia context [agent-id]")
		}
	}
	if !group.Enabled() {
		fmt.Fprintln(w, "group awareness is off (regia group on)")
		return nil
	}
	if self == "" {
		return fmt.Errorf("not inside a Claude Code session: pass an agent id (regia context <id>)")
	}
	t := group.LoadTree()
	agents := group.Agents()
	t.SetLive(agents)
	pos := t.Resolve(self)
	fmt.Fprint(w, group.Context(t, pos, agents, time.Now()))
	if self == group.SelfFromEnv() {
		group.MarkRead(pos)
	}
	return nil
}

// regia group on|off|status
func cmdGroup(args []string, w io.Writer) error {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "on", "off":
		if err := group.SetEnabled(sub == "on"); err != nil {
			return err
		}
	case "status":
	default:
		return fmt.Errorf("usage: regia group on|off|status")
	}
	state := "on: grouped agents learn about their group (regia context)"
	if !group.Enabled() {
		state = "off: agents get nothing from regia about their group"
	}
	fmt.Fprintln(w, "group awareness "+state)
	if os.Getenv("REGIA_GROUP") != "" {
		fmt.Fprintln(w, "(REGIA_GROUP="+os.Getenv("REGIA_GROUP")+" is set in this shell)")
	}
	return nil
}
