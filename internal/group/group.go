// Package group makes agents aware of the group they belong to in regia's
// tree: their siblings, their coordinator, the agents they started, and notes
// the group shares.
//
// Only relationships made on purpose count: hand-made groups ("g:…") and
// coordinators (an agent that started others, or that agents were moved
// under). Automatic project groups ("p:…") don't: agents that only share a
// directory see nothing about each other.
//
// It is built to cost nothing when unused and to stay out of the way:
//   - agents outside a group get nothing (the hook exits at once);
//   - a grouped agent is told once, in a few lines, that `regia context` and
//     `regia note` exist; after that the hook speaks only when a sibling
//     wrote a note it hasn't seen or it was moved, at most once a minute,
//     with one line that points to `regia context` instead of pasting notes;
//   - the whole thing is off with `regia group off` or REGIA_GROUP=off, and a
//     group or coordinator can be muted in regia (M).
package group

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/farzeni/regia/internal/layout"
	"github.com/farzeni/regia/internal/spawns"
)

// SelfID is the id regia lists a session under: its job id for a background
// session, "s:<8>" for an interactive one.
func SelfID(jobDir, sessionID string) string {
	if jobDir != "" {
		return filepath.Base(jobDir)
	}
	if sessionID != "" {
		if len(sessionID) > 8 {
			sessionID = sessionID[:8]
		}
		return "s:" + sessionID
	}
	return ""
}

// SelfFromEnv is SelfID for a command run inside a Claude Code session.
func SelfFromEnv() string {
	return SelfID(os.Getenv("CLAUDE_JOB_DIR"), os.Getenv("CLAUDE_CODE_SESSION_ID"))
}

// --- on / off ---------------------------------------------------------------

func configPath() string { return filepath.Join(filepath.Dir(layout.Path()), "config.json") }

type config struct {
	Group string `json:"group,omitempty"` // "off" disables group awareness
}

func readConfig() config {
	var c config
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

// Enabled says whether group awareness is on: not turned off with
// `regia group off`, nor for this session with REGIA_GROUP=off.
func Enabled() bool {
	switch strings.ToLower(os.Getenv("REGIA_GROUP")) {
	case "off", "0", "false", "no":
		return false
	}
	return readConfig().Group != "off"
}

// SetEnabled turns group awareness on or off for every session.
func SetEnabled(on bool) error {
	c := readConfig()
	c.Group = ""
	if !on {
		c.Group = "off"
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	_ = os.MkdirAll(filepath.Dir(configPath()), 0o755)
	return os.WriteFile(configPath(), b, 0o644)
}

// --- position in the tree ---------------------------------------------------

// Tree answers "who is above / beside / below" from layout.json and
// spawns.jsonl alone, without listing agents (the hook must be fast).
type Tree struct {
	L      *layout.Layout
	Spawns map[string]spawns.Link
}

func LoadTree() *Tree {
	sp := spawns.Cached()
	if sp == nil {
		sp = map[string]spawns.Link{}
	}
	return &Tree{L: layout.Load(), Spawns: sp}
}

// parentOf is id's parent as far as groups are concerned: where it sits in
// layout.json, or its spawner when regia hasn't placed it yet (regia isn't
// running) or it still sits in its project group waiting to be re-linked.
// A project group, or the root, is "".
func (t *Tree) parentOf(id string) string {
	p, placed := t.L.Parent[id]
	if placed && (t.L.Pinned[id] || !strings.HasPrefix(p, "p:")) {
		if strings.HasPrefix(p, "p:") {
			return ""
		}
		return p
	}
	if s, ok := t.Spawns[id]; ok && s.Parent != id {
		return s.Parent
	}
	return ""
}

// children lists the nodes whose parent is id, in regia's order first.
func (t *Tree) children(id string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(c string) {
		if !seen[c] && c != id && t.parentOf(c) == id {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, c := range t.L.Order[id] {
		add(c)
	}
	for c := range t.L.Parent {
		add(c)
	}
	for c := range t.Spawns {
		add(c)
	}
	return out
}

func IsGroup(id string) bool { return strings.HasPrefix(id, "g:") }

// Position is where an agent sits, with muted parts already left out.
type Position struct {
	Self string
	// Chain lists the containers above, nearest first: hand-made groups and
	// coordinators, up to the first project group or the root.
	Chain []string
	// Family is true when the agent coordinates agents of its own.
	Family bool
}

// Resolve finds self's position. A muted container cuts the chain there.
func (t *Tree) Resolve(self string) Position {
	pos := Position{Self: self}
	seen := map[string]bool{self: true}
	for p := t.parentOf(self); p != "" && !seen[p] && len(pos.Chain) < 32; p = t.parentOf(p) {
		seen[p] = true
		if t.L.Muted[p] {
			break
		}
		pos.Chain = append(pos.Chain, p)
	}
	pos.Family = !t.L.Muted[self] && len(t.children(self)) > 0
	return pos
}

// Grouped says whether the agent has anyone to be aware of.
func (p Position) Grouped() bool { return len(p.Chain) > 0 || p.Family }

// Nearest is the container a note from this agent goes to: the closest one
// above, or its own family when nothing is above.
func (p Position) Nearest() string {
	if len(p.Chain) > 0 {
		return p.Chain[0]
	}
	if p.Family {
		return p.Self
	}
	return ""
}

// Visible lists the containers whose notes the agent sees: its own family's
// (its agents write there) and every container above (notes flow down).
func (p Position) Visible() []string {
	var out []string
	if p.Family {
		out = append(out, p.Self)
	}
	return append(out, p.Chain...)
}

// Siblings are the other members of the nearest container above.
func (t *Tree) Siblings(p Position) []string {
	if len(p.Chain) == 0 {
		return nil
	}
	var out []string
	for _, c := range t.children(p.Chain[0]) {
		if c != p.Self && !t.L.Muted[c] {
			out = append(out, c)
		}
	}
	return out
}

// Children are the agents (or groups) under self.
func (t *Tree) Children(p Position) []string {
	if !p.Family {
		return nil
	}
	return t.children(p.Self)
}

// Members counts the agents a note in container c reaches (everything below it).
func (t *Tree) Members(c string) int {
	n := 0
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		for _, ch := range t.children(id) {
			if depth > 32 || t.L.Muted[ch] {
				continue
			}
			if !IsGroup(ch) {
				n++
			}
			walk(ch, depth+1)
		}
	}
	walk(c, 0)
	return n
}

// Title names a node: a group's title, an alias, else the given name lookup.
func (t *Tree) Title(id string, name func(string) string) string {
	if g, ok := t.L.Groups[id]; ok {
		return g
	}
	if a := t.L.Alias[id]; a != "" {
		return a
	}
	if name != nil {
		if n := name(id); n != "" {
			return n
		}
	}
	return id
}
