package group

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
	"github.com/farzeni/regia/internal/spawns"
)

// What agents can do to regia's tree through `regia mcp`, scoped by who is
// asking (see docs/design/agents-mcp.md):
//   - a worker reads its group and changes nothing until it claims;
//   - a coordinator (an agent with agents under it) organizes what is under
//     it: groups among its agents, moves between them, archive;
//   - an orchestrator does the same under its group, or anywhere: it claimed
//     that itself (claim, with a reason, shown in regia as ✦) or you granted
//     it (O). A claim never reaches muted parts and ends when the agent is
//     done; your grant of the whole tree reaches everything.
// The tree is the one you see in regia. Every change is recorded (Action) so
// you can undo it, and nothing here touches Claude Code itself.

const (
	RoleWorker       = "worker"
	RoleCoordinator  = "coordinator"
	RoleOrchestrator = "orchestrator"
	Everything       = "*" // a grant over the whole tree
)

// ErrOff is returned by every tool while group awareness is off.
var ErrOff = errors.New("group awareness is off in regia (regia group on)")

// Caller is the agent asking, as the tools see it.
type Caller struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Scope string `json:"scope,omitempty"` // what it may organize: its own id, a group id, or "*"; "" for a worker
	Group string `json:"group,omitempty"` // the group or coordinator it is in, "" when none
	Claim string `json:"claim,omitempty"` // why it claimed its scope itself; "" when the user granted it
}

// state is the tree as the tools read it: the layout with every listed agent
// placed, as regia's panel would.
type world struct {
	l      *layout.Layout
	agents map[string]*claude.Agent
}

func newWorld(l *layout.Layout, agents map[string]*claude.Agent) *world {
	l.Spawns = spawns.Cached()
	l.Place(agents)
	return &world{l: l, agents: agents}
}

func (w *world) title(id string) string {
	if id == "" {
		return "the top level"
	}
	if t := w.l.Groups[id]; t != "" {
		return t
	}
	if a := w.l.Alias[id]; a != "" {
		return a
	}
	if a := w.agents[id]; a != nil {
		return a.Name
	}
	return id
}

func (w *world) parent(id string) string  { return w.l.EffParent(id, w.agents) }
func (w *world) kids(id string) []string  { return w.l.Children(id, w.agents) }
func (w *world) exists(id string) bool    { return w.l.IsGroup(id) || w.agents[id] != nil }
func (w *world) isGroup(id string) bool   { return w.l.IsGroup(id) }
func (w *world) isAgent(id string) bool   { return w.agents[id] != nil }
func (w *world) container(id string) bool { return w.isGroup(id) || len(w.kids(id)) > 0 }

// under says whether id sits strictly below root ("" or "*": anywhere).
func (w *world) under(id, root string) bool {
	if id == "" || id == root {
		return false
	}
	if root == "" || root == Everything {
		return true
	}
	for p, n := w.parent(id), 0; p != "" && n < 64; p, n = w.parent(p), n+1 {
		if p == root {
			return true
		}
	}
	return false
}

// mutedBetween says whether a muted group or coordinator sits between root
// and id (id included): muted parts are out of reach.
func (w *world) mutedBetween(id, root string) bool {
	for p, n := id, 0; p != "" && p != root && n < 64; p, n = w.parent(p), n+1 {
		if w.l.Muted[p] {
			return true
		}
	}
	return false
}

// who resolves the caller.
func (w *world) who(self string) Caller {
	c := Caller{ID: self, Name: w.title(self), Role: RoleWorker}
	for p, n := w.parent(self), 0; p != "" && n < 64; p, n = w.parent(p), n+1 {
		if !strings.HasPrefix(p, "p:") {
			c.Group = p
			break
		}
	}
	switch g := w.l.Grants[self]; {
	case g == Everything || (g != "" && w.exists(g)):
		c.Role, c.Scope, c.Claim = RoleOrchestrator, g, w.l.Claims[self]
	case len(w.kids(self)) > 0:
		c.Role, c.Scope = RoleCoordinator, self
	}
	return c
}

// readRoot is the top of what c may read: its scope, else its group, else
// itself.
func (c Caller) readRoot() string {
	switch {
	case c.Scope != "":
		return c.Scope
	case c.Group != "":
		return c.Group
	}
	return c.ID
}

// canRead says whether c may look at id.
func (c Caller) canRead(w *world, id string) bool {
	root := c.readRoot()
	if root == Everything || id == root || id == c.ID {
		return true
	}
	return w.under(id, root) && !w.mutedBetween(id, root)
}

// canChange says why c may not move, archive, rename or remove id ("" if it may).
func (c Caller) canChange(w *world, id string) string {
	switch {
	case c.Scope == "":
		return "you haven't claimed any part of the tree: call claim (your group, or the whole tree) with the reason, then try again"
	case !w.exists(id):
		return fmt.Sprintf("%s is not in regia's tree", id)
	case id == c.ID:
		return "you can't move or archive yourself"
	case !w.under(id, c.Scope):
		return fmt.Sprintf("%s (%s) is outside what you may organize (%s): claim %s first", w.title(id), id, c.scopeWords(w), c.wider())
	case (c.Scope != Everything || c.Claim != "") && w.mutedBetween(id, c.Scope):
		return fmt.Sprintf("%s is in a part of the tree the user muted", w.title(id))
	}
	return ""
}

// canPutInto says why c may not put things under parent ("" if it may).
func (c Caller) canPutInto(w *world, parent string) string {
	switch {
	case c.Scope == "":
		return c.canChange(w, parent)
	case parent == c.Scope || (parent == "" && c.Scope == Everything):
		return ""
	case parent == "":
		return fmt.Sprintf("the top level is outside what you may organize (%s)", c.scopeWords(w))
	case parent != "" && !w.exists(parent):
		return fmt.Sprintf("%s is not in regia's tree", parent)
	}
	return c.canChange(w, parent)
}

// wider is what c would claim to reach further.
func (c Caller) wider() string {
	if c.Group != "" && c.Scope != c.Group && c.Scope != Everything {
		return "your group or the whole tree"
	}
	return "the whole tree"
}

func (c Caller) scopeWords(w *world) string {
	switch {
	case c.Scope == Everything:
		return "the whole tree"
	case c.Scope == c.ID:
		return "the agents under you"
	}
	return fmt.Sprintf("group %q", w.title(c.Scope))
}

// --- reads ------------------------------------------------------------------

// Whoami is the caller: role, scope and group, with their titles.
type WhoamiOut struct {
	Caller
	ScopeTitle string `json:"scope_title,omitempty"`
	GroupTitle string `json:"group_title,omitempty"`
	Can        string `json:"can"`
}

func Whoami(self string) (WhoamiOut, error) {
	w, err := read(self)
	if err != nil {
		return WhoamiOut{}, err
	}
	c := w.who(self)
	out := WhoamiOut{Caller: c}
	if c.Group != "" {
		out.GroupTitle = w.title(c.Group)
	}
	switch c.Role {
	case RoleWorker:
		out.Can = "read your group (tree, agent, inbox, timeline, changes); to change the tree, claim your group or the whole tree first"
	default:
		out.ScopeTitle = c.scopeWords(w)
		out.Can = "read and organize " + out.ScopeTitle + ": create_group, rename_group, delete_group, move, archive, unarchive, rename_agent, pin"
		if c.Role == RoleCoordinator {
			out.Can += "; to reach beyond your agents, claim " + c.wider()
		} else if c.Claim != "" {
			out.Can += "; release when you're done"
		}
	}
	return out, nil
}

// Node is one row of the tree.
type Node struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // group, agent
	Title    string `json:"title"`
	State    string `json:"state,omitempty"` // agents: working, waiting on the user, idle, done, failed
	Question string `json:"question,omitempty"`
	Doing    string `json:"doing,omitempty"`
	Updated  string `json:"updated,omitempty"` // "12m ago"
	Archived bool   `json:"archived,omitempty"`
	Muted    bool   `json:"muted,omitempty"`
	Children []Node `json:"children,omitempty"`
}

// TreeOpts narrows what TreeOf returns, to keep it short on a large tree.
type TreeOpts struct {
	Archived bool   // also archived agents
	Under    string // only this group's (or agent's) part, inside what the caller may read
	Brief    bool   // ids, titles and states only: no question, doing, updated
}

// TreeOf is what the caller may read, as nested nodes: its scope, else its
// group, or the part under o.Under. Archived agents are left out unless
// o.Archived.
func TreeOf(self string, o TreeOpts) (Node, error) {
	w, err := read(self)
	if err != nil {
		return Node{}, err
	}
	c := w.who(self)
	root := c.readRoot()
	if root == Everything {
		root = ""
	}
	now := time.Now()
	var walk func(id string, depth int) Node
	walk = func(id string, depth int) Node {
		n := w.node(id, now)
		if o.Brief {
			n.Question, n.Doing, n.Updated = "", "", ""
		}
		if depth > 32 || (c.Scope != Everything && id != root && w.l.Muted[id]) {
			return n
		}
		for _, k := range w.kids(id) {
			if !o.Archived && w.l.IsArchived(k) {
				continue
			}
			n.Children = append(n.Children, walk(k, depth+1))
		}
		return n
	}
	t := walk(root, 0)
	if o.Under == "" || o.Under == root {
		return t, nil
	}
	if n, ok := find(t, o.Under); ok {
		return n, nil
	}
	return Node{}, fmt.Errorf("%s is not in what you may read (tree lists it)", o.Under)
}

// find is the node id inside t.
func find(t Node, id string) (Node, bool) {
	if t.ID == id {
		return t, true
	}
	for _, c := range t.Children {
		if n, ok := find(c, id); ok {
			return n, true
		}
	}
	return Node{}, false
}

func (w *world) node(id string, now time.Time) Node {
	n := Node{ID: id, Title: w.title(id), Kind: "group", Muted: w.l.Muted[id]}
	if id == "" {
		n.ID, n.Title = "", "regia"
		return n
	}
	if a := w.agents[id]; a != nil {
		claude.Enrich(a)
		n.Kind, n.State, n.Archived = "agent", stateWord(a), w.l.IsArchived(id)
		n.Doing = trunc(claude.Tidy(a.Detail), 160)
		if a.State == "blocked" {
			n.Question = trunc(claude.Tidy(a.Needs), 200)
		}
		if s := ago(a, now); s != "" {
			n.Updated = s + " ago"
		}
	}
	return n
}

// AgentOut is one agent in detail.
type AgentOut struct {
	Node
	Parent    string   `json:"parent,omitempty"`
	Intent    string   `json:"intent,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
	Branch    string   `json:"branch,omitempty"`
	Started   string   `json:"started,omitempty"`
	Tokens    int      `json:"tokens,omitempty"`
	PRs       []string `json:"prs,omitempty"`
	Stuck     string   `json:"stuck,omitempty"` // why a working agent looks stuck
	Agents    []string `json:"agents,omitempty"`
	LastWords []string `json:"last_messages,omitempty"` // the latest messages, oldest first
}

const lastWords = 6

func AgentInfo(self, id string) (AgentOut, error) {
	w, err := read(self)
	if err != nil {
		return AgentOut{}, err
	}
	c := w.who(self)
	a := w.agents[id]
	if a == nil {
		return AgentOut{}, fmt.Errorf("no agent %q in regia's tree (ids are in tree)", id)
	}
	if !c.canRead(w, id) {
		return AgentOut{}, fmt.Errorf("%s is outside what you may read", id)
	}
	now := time.Now()
	out := AgentOut{Node: w.node(id, now), Parent: w.parent(id), Intent: a.Intent, Cwd: a.Cwd, Branch: a.Branch, Tokens: a.Tokens}
	if !a.StartedAt.IsZero() {
		out.Started = a.StartedAt.Format(time.RFC3339)
	}
	for _, p := range a.PRs {
		out.PRs = append(out.PRs, p.Href)
	}
	out.Stuck = claude.Health(a, now)
	out.Agents = w.kids(id)
	var words []string
	for _, m := range claude.Messages(claude.TranscriptPath(a)) {
		who := ""
		switch m.Role {
		case claude.RoleYou:
			who = "user: "
		case claude.RoleAgent:
			who = "agent: "
		default:
			continue
		}
		if t := strings.TrimSpace(m.Text); t != "" {
			words = append(words, who+trunc(strings.Join(strings.Fields(t), " "), 600))
		}
	}
	out.LastWords = words[max(0, len(words)-lastWords):]
	return out, nil
}

// read loads the tree for a read-only tool.
func read(self string) (*world, error) {
	if !Enabled() {
		return nil, ErrOff
	}
	if self == "" {
		return nil, errors.New("regia can't tell which agent you are (no Claude Code session in the environment)")
	}
	return newWorld(layout.Load(), Agents()), nil
}

// --- writes -----------------------------------------------------------------

// change runs op on the tree under the layout lock, then records it.
func change(self, tool string, op func(w *world, c Caller) (string, error)) (string, error) {
	if !Enabled() {
		return "", ErrOff
	}
	if self == "" {
		return "", errors.New("regia can't tell which agent you are (no Claude Code session in the environment)")
	}
	agents := Agents()
	var summary string
	var caller Caller
	_, ch, err := layout.Update(func(l *layout.Layout) error {
		w := newWorld(l, agents)
		caller = w.who(self)
		s, err := op(w, caller)
		summary = s
		return err
	})
	if err != nil {
		return "", err
	}
	if !ch.Empty() {
		_ = Record(Action{At: time.Now(), By: self, Name: caller.Name, Tool: tool, Summary: summary, Change: ch})
	}
	return summary, nil
}

// checkAgents says why c may not change any of ids, all of which must be agents.
func checkAgents(w *world, c Caller, ids []string) error {
	if len(ids) == 0 {
		return errors.New("no agents given")
	}
	for _, id := range ids {
		if why := c.canChange(w, id); why != "" {
			return errors.New(why)
		}
		if !w.isAgent(id) && !w.isGroup(id) {
			return fmt.Errorf("%s is not an agent or a group", id)
		}
	}
	return nil
}

func plural2(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// CreateGroup makes a group titled title under parent ("": your scope's top)
// and moves ids into it.
func CreateGroup(self, title, parent string, ids []string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("a group needs a title")
	}
	return change(self, "create_group", func(w *world, c Caller) (string, error) {
		if parent == "" && c.Scope != Everything {
			parent = c.Scope
		}
		if why := c.canPutInto(w, parent); why != "" {
			return "", errors.New(why)
		}
		if parent != "" && !w.container(parent) && !w.isAgent(parent) {
			return "", fmt.Errorf("%s can't hold a group", parent)
		}
		if len(ids) > 0 {
			if err := checkAgents(w, c, ids); err != nil {
				return "", err
			}
		}
		gid := w.l.NewGroupIn(title, parent, w.agents)
		if len(ids) > 0 {
			if err := w.l.Put(w.l.Topmost(ids), gid, "", w.agents); err != nil {
				return "", err
			}
		}
		s := fmt.Sprintf("created group %q (%s) in %s", title, gid, w.title(parent))
		if len(ids) > 0 {
			s += " with " + plural2(len(ids), "agent", "agents")
		}
		return s, nil
	})
}

// RenameGroup retitles a group.
func RenameGroup(self, gid, title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("a group needs a title")
	}
	return change(self, "rename_group", func(w *world, c Caller) (string, error) {
		if !IsGroup(gid) || !w.isGroup(gid) {
			return "", fmt.Errorf("%s is not a group you can rename (ids start with g:)", gid)
		}
		if why := c.canChange(w, gid); why != "" {
			return "", errors.New(why)
		}
		old := w.l.Groups[gid]
		w.l.Groups[gid] = title
		return fmt.Sprintf("renamed group %q to %q", old, title), nil
	})
}

// DeleteGroup removes a group; what was in it moves up into its place.
func DeleteGroup(self, gid string) (string, error) {
	return change(self, "delete_group", func(w *world, c Caller) (string, error) {
		if !IsGroup(gid) || !w.isGroup(gid) {
			return "", fmt.Errorf("%s is not a group you can remove (ids start with g:)", gid)
		}
		if why := c.canChange(w, gid); why != "" {
			return "", errors.New(why)
		}
		title, up := w.l.Groups[gid], w.parent(gid)
		n := len(w.kids(gid))
		w.l.DeleteGroup(gid, w.agents)
		delete(w.l.Muted, gid)
		return fmt.Sprintf("removed group %q: %s moved up into %s", title, plural2(n, "item", "items"), w.title(up)), nil
	})
}

// Move puts ids under to (a group or an agent), after `after` or at the end.
func Move(self string, ids []string, to, after string) (string, error) {
	return change(self, "move", func(w *world, c Caller) (string, error) {
		if err := checkAgents(w, c, ids); err != nil {
			return "", err
		}
		if why := c.canPutInto(w, to); why != "" {
			return "", errors.New(why)
		}
		if after != "" && w.parent(after) != to {
			return "", fmt.Errorf("%s is not in %s", after, w.title(to))
		}
		if after == "" {
			if ks := w.kids(to); len(ks) > 0 {
				after = ks[len(ks)-1]
			}
		}
		ids = w.l.Topmost(ids)
		if slices.Contains(ids, after) {
			after = ""
		}
		if err := w.l.Put(ids, to, after, w.agents); err != nil {
			return "", err
		}
		return fmt.Sprintf("moved %s into %s", plural2(len(ids), "item", "items"), w.title(to)), nil
	})
}

// Archive hides agents in regia (on) or brings them back; Claude Code is not
// touched, and an archived agent that starts working comes back by itself.
func Archive(self string, ids []string, on bool) (string, error) {
	tool := "archive"
	if !on {
		tool = "unarchive"
	}
	return change(self, tool, func(w *world, c Caller) (string, error) {
		if err := checkAgents(w, c, ids); err != nil {
			return "", err
		}
		for _, id := range ids {
			if !w.isAgent(id) {
				return "", fmt.Errorf("%s is a group: archive its agents", id)
			}
		}
		if on {
			w.l.Archive(ids...)
			return "archived " + plural2(len(ids), "agent", "agents"), nil
		}
		w.l.Unarchive(ids...)
		return "brought back " + plural2(len(ids), "agent", "agents"), nil
	})
}
