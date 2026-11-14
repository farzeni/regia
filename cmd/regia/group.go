package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/group"
)

// regia context [--all] [id]: what an agent's group looks like and its notes.
// Inside a Claude Code session the agent is found from the environment.
func cmdContext(args []string, w io.Writer) error {
	all, self := false, group.SelfFromEnv()
	for _, a := range args {
		switch {
		case a == "--all":
			all = true
		case !strings.HasPrefix(a, "-"):
			self = a
		default:
			return fmt.Errorf("usage: regia context [--all] [agent-id]")
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
	pos := t.Resolve(self)
	fmt.Fprint(w, group.Context(t, pos, group.Agents(), all, time.Now()))
	if self == group.SelfFromEnv() {
		group.MarkRead(t, pos)
	}
	return nil
}

// regia note "…": share one line with the agent's nearest group.
func cmdNote(args []string, w io.Writer) error {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		return fmt.Errorf(`usage: regia note "a decision, fact or warning for the group"`)
	}
	if !group.Enabled() {
		fmt.Fprintln(w, "group awareness is off (regia group on): note not shared")
		return nil
	}
	self := group.SelfFromEnv()
	if self == "" {
		return fmt.Errorf("regia note works inside a Claude Code session (in regia, N adds a note to the selected group)")
	}
	t := group.LoadTree()
	pos := t.Resolve(self)
	c := pos.Nearest()
	if c == "" {
		fmt.Fprintln(w, "not in a group (a hand-made group in regia, or a coordinator's agents): nothing to share with, note not saved")
		return nil
	}
	agents := group.Agents()
	name := func(id string) string {
		if a := agents[id]; a != nil {
			return a.Name
		}
		return ""
	}
	if err := group.AddNote(c, group.Note{By: self, Name: t.Title(self, name), Text: text}); err != nil {
		return err
	}
	fmt.Fprintf(w, "shared with %s (%d agents)\n", t.Title(c, name), t.Members(c))
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
	state := "on: grouped agents learn about their group (regia context, regia note)"
	if !group.Enabled() {
		state = "off: agents get nothing from regia about their group"
	}
	fmt.Fprintln(w, "group awareness "+state)
	if os.Getenv("REGIA_GROUP") != "" {
		fmt.Fprintln(w, "(REGIA_GROUP="+os.Getenv("REGIA_GROUP")+" is set in this shell)")
	}
	return nil
}
