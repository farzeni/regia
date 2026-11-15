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

const (
	maxListed = 10 // siblings / agents listed before "+N more"
	maxNotes  = 10 // notes shown per container
)

// Context renders what `regia context` prints: where self is, the others
// with their state and last line, and the latest notes of every container it
// can see. all shows every note instead of the latest ones.
func Context(t *Tree, pos Position, agents map[string]*claude.Agent, all bool, now time.Time) string {
	name := func(id string) string {
		if a := agents[id]; a != nil {
			return a.Name
		}
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "regia · you are %q (%s)\n", t.Title(pos.Self, name), pos.Self)
	if !pos.Grouped() {
		b.WriteString("You are not in a group (a hand-made group in regia, or a coordinator's agents): there is no one to share with.\n")
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

	shown := 0
	for _, c := range pos.Visible() {
		notes := ReadNotes(c)
		if len(notes) == 0 {
			continue
		}
		if shown == 0 {
			b.WriteString("blackboard (newest last):\n")
		}
		shown++
		where := t.Title(c, name)
		if c == pos.Self {
			where = "your agents"
		}
		fmt.Fprintf(&b, "  [%s]\n", where)
		start := 0
		if !all && len(notes) > maxNotes {
			start = len(notes) - maxNotes
			fmt.Fprintf(&b, "    (+%d older: regia context --all)\n", start)
		}
		for _, n := range notes[start:] {
			who := n.author()
			if n.By == pos.Self {
				who = "you"
			}
			fmt.Fprintf(&b, "    %s %s: %s\n", noteTime(n.At, now), who, n.Text)
		}
	}
	if shown == 0 {
		b.WriteString("blackboard: empty — `regia note \"…\"` writes on it.\n")
	}
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

func noteTime(t, now time.Time) string {
	if t.Format("2006-01-02") == now.Format("2006-01-02") {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

func trunc(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
