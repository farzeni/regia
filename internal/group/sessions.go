package group

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/gitinfo"
	"github.com/farzeni/regia/internal/plugins"
)

// Removing and stopping sessions: the two tools that reach into Claude Code.
// They follow the tree tools' scope (a coordinator acts on the agents under
// it, anything else needs a claim, muted parts are out of reach, never the
// caller itself) and are logged in U with the reason, but they can't be
// undone there: a removed session can only be resumed (claude --resume), a
// stopped one starts again when it gets a message.

// RunRm runs `claude rm <id>`: the session leaves the list and its worktree is
// deleted when that's safe; the conversation stays resumable. It refuses
// (non-zero exit, with a reason) e.g. over unpushed commits, and regia never
// passes --discard or --force. A variable for tests.
var RunRm = func(id string) (string, error) {
	out, err := exec.Command("claude", "rm", id).CombinedOutput()
	return string(out), err
}

// RunStop runs `claude stop <id>`: the session stays listed and resumable.
// A variable for tests.
var RunStop = func(id string) (string, error) {
	out, err := exec.Command("claude", "stop", id).CombinedOutput()
	return string(out), err
}

// RemoveSession is how every removal ends, from x, a plugin action or an
// agent's remove tool: the plugins' remove gates first (one may refuse, e.g.
// until the session wrote its handoff), then claude rm. in describes the
// agent as the caller knows it; by is "you" or the agent asking.
func RemoveSession(in plugins.Input, by, reason string) (string, error) {
	in.By, in.Reason = by, reason
	if err := plugins.Gate(plugins.GateRemove, in, in.Agent.Cwd); err != nil {
		return "", err
	}
	out, err := RunRm(in.Agent.ID)
	if err != nil {
		return out, errors.New(oneLine(out, err))
	}
	return out, nil
}

// PluginAgent is what a plugin reads about a in "agent".
func PluginAgent(a *claude.Agent) plugins.AgentInfo {
	ai := plugins.AgentInfo{
		ID: a.ID, SessionID: a.SessionID, Name: a.Name, Cwd: a.Cwd, Kind: a.Kind, State: a.State,
		Interactive: a.Interactive, Branch: a.Branch, Intent: a.Intent, Detail: a.Detail, Needs: a.Needs,
		Transcript: a.Transcript, StartedAt: a.StartedAt, UpdatedAt: a.UpdatedAt, Links: []plugins.Link{},
	}
	for _, l := range a.PRs {
		ai.Links = append(ai.Links, plugins.Link{Kind: l.Kind, ID: l.ID, URL: l.Href})
	}
	return ai
}

// gateInput is what a remove gate reads about agent id when an agent asks:
// what the panel would give, read now (no PR: gh is too slow for a tool call).
func (w *world) gateInput(id string) plugins.Input {
	a := w.agents[id]
	in := plugins.Input{Agent: PluginAgent(a)}
	in.Agent.Title = w.title(id)
	in.Agent.Group = w.l.Trail(id, w.agents)
	if gi := gitinfo.Read(a.Cwd); gi.OK {
		in.Git = &plugins.GitInfo{Branch: gi.Branch, Upstream: gi.Upstream, Ahead: gi.Ahead, Behind: gi.Behind,
			Changed: gi.Changed + gi.Untracked, Head: gi.Head, Subject: gi.Subject, Worktree: gi.Worktree}
		if gi.Branch != "" {
			in.Agent.Branch = gi.Branch
		}
	}
	return in
}

// oneLine condenses a command's output into one line, for a refusal.
func oneLine(out string, err error) string {
	s := strings.Join(strings.Fields(out), " ")
	if s == "" && err != nil {
		s = err.Error()
	}
	return s
}

// canSession says why c may not stop or remove id ("" if it may).
func (c Caller) canSession(w *world, id string) string {
	if why := c.canChange(w, id); why != "" {
		return why
	}
	a := w.agents[id]
	switch {
	case a == nil:
		return fmt.Sprintf("%s is a group: name its agents", id)
	case a.Interactive:
		return fmt.Sprintf("%s (%s) is an interactive session: only its terminal can end it", w.title(id), id)
	}
	return ""
}

// liveUnder is the agents still listed under id, gone excepted.
func (w *world) liveUnder(id string, gone map[string]bool) []string {
	var out []string
	for _, k := range w.kids(id) {
		if w.isAgent(k) && !gone[k] {
			out = append(out, k)
		}
		out = append(out, w.liveUnder(k, gone)...)
	}
	return out
}

// depth is how far below the top id sits, to remove agents before their
// coordinator.
func (w *world) depth(id string) int {
	n := 0
	for p := w.parent(id); p != "" && n < 64; p = w.parent(p) {
		n++
	}
	return n
}

// sessionsRun is what remove or stop did to each agent asked.
type sessionsRun struct {
	done    []string // titles
	refused []string // "title (id): why"
}

func (r sessionsRun) summary(verb string) (string, error) {
	s := verb + " " + plural2(len(r.done), "session", "sessions")
	if len(r.done) > 0 {
		s += ": " + strings.Join(r.done, ", ")
	}
	if len(r.refused) > 0 {
		s += "; refused " + strings.Join(r.refused, "; ")
	}
	if len(r.done) == 0 {
		return "", errors.New(s)
	}
	return s, nil
}

// sessions checks the caller, then runs do on each agent (in order), logging
// each one done in U with the reason.
func sessions(self, tool string, ids []string, reason string, order func(w *world, ids []string), do func(w *world, c Caller, id string, gone map[string]bool) error) (sessionsRun, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return sessionsRun{}, errors.New("give the reason (the user sees it in regia, under U)")
	}
	if len(ids) == 0 {
		return sessionsRun{}, errors.New("no agents given")
	}
	w, err := read(self)
	if err != nil {
		return sessionsRun{}, err
	}
	c := w.who(self)
	ids = unique(ids)
	if order != nil {
		order(w, ids)
	}
	var r sessionsRun
	gone := map[string]bool{}
	for _, id := range ids {
		name := w.title(id)
		if err := do(w, c, id, gone); err != nil {
			r.refused = append(r.refused, fmt.Sprintf("%s (%s): %v", name, id, err))
			continue
		}
		gone[id] = true
		r.done = append(r.done, name)
		_ = Record(Action{At: time.Now(), By: self, Name: c.Name, Tool: tool, Final: true,
			Summary: fmt.Sprintf("%s %s (%s): %s", map[string]string{"remove": "removed", "stop": "stopped"}[tool], name, id, reason)})
	}
	return r, nil
}

// unique is ids without repeats, in their order.
func unique(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// Remove runs claude rm on agents inside the caller's scope that are done,
// idle or failed: never one that is working or waiting on the user, and never
// a coordinator with agents still listed under it (they go first; given in
// the same call, they are removed first). Each removal passes the plugins'
// remove gates and is logged in U with the reason.
func Remove(self string, ids []string, reason string) (string, error) {
	r, err := sessions(self, "remove", ids, reason,
		func(w *world, ids []string) { // deepest first: agents before their coordinator
			slices.SortStableFunc(ids, func(a, b string) int { return w.depth(b) - w.depth(a) })
		},
		func(w *world, c Caller, id string, gone map[string]bool) error {
			if why := c.canSession(w, id); why != "" {
				return errors.New(why)
			}
			a := w.agents[id]
			claude.Enrich(a) // the state as of now, not the cached listing's
			switch a.State {
			case "busy":
				return errors.New("it is working: wait until it is done, or stop it first")
			case "blocked":
				return errors.New("it is waiting on the user")
			}
			if live := w.liveUnder(id, gone); len(live) > 0 {
				return fmt.Errorf("agents are still listed under it (%s): remove them first", strings.Join(live, ", "))
			}
			_, err := RemoveSession(w.gateInput(id), c.Name, reason)
			return err
		})
	if err != nil {
		return "", err
	}
	return r.summary("removed")
}

// Stop runs claude stop on agents inside the caller's scope, working ones
// included: to quiet an agent that should not go on. The session stays
// listed and resumable. Logged in U with the reason.
func Stop(self string, ids []string, reason string) (string, error) {
	r, err := sessions(self, "stop", ids, reason, nil,
		func(w *world, c Caller, id string, _ map[string]bool) error {
			if why := c.canSession(w, id); why != "" {
				return errors.New(why)
			}
			a := w.agents[id]
			claude.Enrich(a)
			if a.State == "done" || a.State == "failed" {
				return fmt.Errorf("it is already %s", a.State)
			}
			if out, err := RunStop(id); err != nil {
				return errors.New(oneLine(out, err))
			}
			return nil
		})
	if err != nil {
		return "", err
	}
	return r.summary("stopped")
}
