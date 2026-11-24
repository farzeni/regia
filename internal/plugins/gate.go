package plugins

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// GateRemove is the gate asked before a session is removed (claude rm), from
// regia's x, an agent's remove tool or a plugin action: a plugin that lists it
// in "gates" may refuse, e.g. until the session has written its handoff.
const GateRemove = "remove"

// GateSwap is the gate asked before a session swap (regia swap, an agent's
// swap tool): the agent is the session being replaced, "to" the one taking
// its place. The remove gate is not asked for it as well.
const GateSwap = "swap"

// GateAttach is the gate asked before enter attaches to a session: a plugin
// may refuse, or answer {"run": "<action>"} to run one of its actions first
// (e.g. one that starts a fresh session from a handoff, attached instead).
// Unlike the others it fails open: attaching destroys nothing.
const GateAttach = "attach"

// attachWait caps each attach gate: enter shouldn't hang on a script.
var attachWait = 2 * time.Second

// AttachAnswer is what the attach gates said: Refuse (why not), or Run (an
// action of Plugin to run first); Warn lists the gates that failed and were
// let through.
type AttachAnswer struct {
	Refuse string
	Run    string
	Plugin *Manifest
	Warn   string
}

// AttachGating: plugins gate attaching to an agent working in cwd (so enter
// has to ask them first).
func AttachGating(cwd string) bool {
	for _, p := range Plugins() {
		if slices.Contains(p.Gates, GateAttach) && p.covers(cwd) {
			return true
		}
	}
	return false
}

// Attach asks the attach gates about the agent in in, one after the other,
// each for at most attachWait. The first refusal or run wins. It blocks: run
// it in a tea.Cmd.
func Attach(in Input, dir string) AttachAnswer {
	in.Version, in.Event, in.Last = ProtocolVersion, GateAttach, nil
	var ans AttachAnswer
	var warns []string
	ctx, cancel := context.WithTimeout(context.Background(), attachWait) // for all the gates together
	defer cancel()
	for _, p := range Plugins() {
		if !slices.Contains(p.Gates, GateAttach) || !p.covers(in.Agent.Cwd) {
			continue
		}
		in.Plugin = PluginInfo{Name: p.Name, Dir: p.Dir}
		out, _, err := Run(ctx, p, in, dir)
		if _, warning := err.(*Warning); err != nil && !warning {
			warns = append(warns, fmt.Sprintf("%s couldn't check it (%v)", p.Name, err))
			continue
		}
		if out.Refuse != "" {
			ans.Refuse = p.Name + ": " + out.Refuse
			break
		}
		if out.Run != "" {
			ans.Run, ans.Plugin = out.Run, p
			break
		}
	}
	ans.Warn = strings.Join(warns, "; ")
	return ans
}

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
