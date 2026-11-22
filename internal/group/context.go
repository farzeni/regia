package group

import (
	"fmt"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/claude"
)

// Agents lists the agents, from regia's cache when it is fresh (regia is
// running and refreshing it) or from `claude agents` otherwise.
var Agents = func() map[string]*claude.Agent {
	b := claude.CachedAgents()
	if claude.CachedAge() > 20*time.Second {
		if fresh, err := claude.FetchAgents(); err == nil {
			b = fresh
		}
	}
	out := map[string]*claude.Agent{}
	for _, a := range claude.ParseAgents(b) {
		out[a.ID] = a
	}
	return out
}

// cachedNames looks agent names up in regia's cached listing (no `claude`
// call: the hook must stay fast).
func cachedNames() func(string) string {
	var names map[string]string
	return func(id string) string {
		if names == nil {
			names = map[string]string{}
			for _, a := range claude.ParseAgents(claude.CachedAgents()) {
				names[a.ID] = a.Name
			}
		}
		return names[id]
	}
}

const maxListed = 10 // siblings / agents listed before "+N more"

// Context renders what `regia context` prints: where self is, and the others
// with their state and last line.
func Context(t *Tree, pos Position, agents map[string]*claude.Agent, now time.Time) string {
	name := func(id string) string {
		if a := agents[id]; a != nil {
			return a.Name
		}
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "regia · you are %q (%s)\n", t.Title(pos.Self, name), pos.Self)
	if !pos.Grouped() {
		b.WriteString("You are not in a group (a hand-made group in regia, or a coordinator's agents): no coordinator, no siblings.\n")
		return b.String()
	}
	line := func(id string) string {
		if IsGroup(id) {
			return fmt.Sprintf("▸ group %q (%d agents)", t.Title(id, name), t.Members(id))
		}
		a := agents[id]
		if a == nil {
			return fmt.Sprintf("· %s — not listed any more", t.Title(id, name))
		}
		claude.Enrich(a)
		s := fmt.Sprintf("%s %s — %s %s", glyph(a), t.Title(id, name), stateWord(a), ago(a, now))
		if d := claude.Tidy(a.Detail); d != "" {
			s += " — " + trunc(d, 90)
		}
		return s
	}
	list := func(title string, ids []string) {
		if len(ids) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s (%d):\n", title, len(ids))
		for i, id := range ids {
			if i == maxListed {
				fmt.Fprintf(&b, "  +%d more\n", len(ids)-maxListed)
				break
			}
			b.WriteString("  " + line(id) + "\n")
		}
	}
	if len(pos.Chain) > 0 {
		c := pos.Chain[0]
		if IsGroup(c) {
			fmt.Fprintf(&b, "group %q", t.Title(c, name))
			if len(pos.Chain) > 1 {
				fmt.Fprintf(&b, " (inside %s)", t.Title(pos.Chain[1], name))
			}
			b.WriteString("\n")
		} else {
			b.WriteString("coordinator: " + line(c) + "\n")
		}
		list("siblings", t.Siblings(pos))
	}
	list("your agents", t.Children(pos))

	// Self-describing, for an agent told only "use regia context".
	b.WriteString("(siblings: the other agents in your group or under your coordinator)\n")
	return b.String()
}

func glyph(a *claude.Agent) string {
	switch a.State {
	case "blocked":
		return "●"
	case "busy":
		return "◆"
	case "done":
		return "✓"
	case "failed":
		return "✗"
	}
	return "○"
}

func stateWord(a *claude.Agent) string {
	switch a.State {
	case "blocked":
		return "waiting on the user"
	case "busy":
		return "working"
	case "done":
		return "done"
	case "failed":
		return "failed"
	}
	return "idle"
}

func ago(a *claude.Agent, now time.Time) string {
	t := a.UpdatedAt
	if t.IsZero() {
		t = a.StartedAt
	}
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func trunc(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
