package plugins

import (
	"context"
	"fmt"
	"slices"
)

// GateRemove is the gate asked before a session is removed (claude rm), from
// regia's x, an agent's remove tool or a plugin action: a plugin that lists it
// in "gates" may refuse, e.g. until the session has written its handoff.
const GateRemove = "remove"

// GateSwap is the gate asked before a session swap (regia swap, an agent's
// swap tool): the agent is the session being replaced, "to" the one taking
// its place. The remove gate is not asked for it as well.
const GateSwap = "swap"

// Gate asks every plugin that gates event about the agent in in, one after
// the other, and returns why the first one refuses. A plugin that fails
// (exits non-zero, times out, prints no JSON) refuses too: a rule you set up
// is never skipped silently. The plugins' own "states" don't matter here,
// their "dirs" do. It blocks: run it in a tea.Cmd or a tool call.
func Gate(event string, in Input, dir string) error {
	in.Version, in.Event, in.Last = ProtocolVersion, event, nil
	for _, p := range Plugins() {
		if !slices.Contains(p.Gates, event) || !p.covers(in.Agent.Cwd) {
			continue
		}
		in.Plugin = PluginInfo{Name: p.Name, Dir: p.Dir}
		out, _, err := Run(context.Background(), p, in, dir)
		if _, warning := err.(*Warning); err != nil && !warning {
			return fmt.Errorf("%s couldn't check it (%v)", p.Name, err)
		}
		if out.Refuse != "" {
			return fmt.Errorf("%s: %s", p.Name, out.Refuse)
		}
	}
	return nil
}
