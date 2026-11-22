// Package layout is the user's own organization of agents: groups,
// coordinators with their agents, order, aliases and folds, saved in
// ~/.config/regia/layout.json. It places new agents automatically (fork
// parent, spawner, project group) and flattens the tree into display rows.
package layout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/spawns"
)

// Layout is the user's own organization of agents, independent of Claude Code.
// Nodes are agent ids or group ids ("g:..." user groups, "p:..." auto project
// groups). Any agent can be a parent: that is how a coordinator holds its agents.
type Layout struct {
	Groups    map[string]string    `json:"groups"`             // group id -> title
	Parent    map[string]string    `json:"parent"`             // node -> parent node ("" = root)
	Order     map[string][]string  `json:"order"`              // parent -> ordered children
	Alias     map[string]string    `json:"alias"`              // agent id -> display name
	Collapsed map[string]bool      `json:"collapsed"`          // node -> collapsed
	Pinned    map[string]bool      `json:"pinned"`             // placed by hand: never re-linked automatically
	Sidebar   int                  `json:"sidebar,omitempty"`  // list width in columns (0: automatic)
	Archived  map[string]time.Time `json:"archived,omitempty"` // agent id -> when: hidden by regia, untouched in Claude
	Muted     map[string]bool      `json:"muted,omitempty"`    // group or coordinator -> left out of group awareness
	Dirs      map[string]string    `json:"dirs,omitempty"`     // group -> directory new agents in it start in
	Seen      map[string]time.Time `json:"seen,omitempty"`     // agent id -> when you last looked at it (nil: never recorded)
	Pins      map[string]bool      `json:"pins,omitempty"`     // agent id -> pinned by you: shown even when done (Pinned above is placement)
	Snoozed   map[string]time.Time `json:"snoozed,omitempty"`  // agent id -> its last change when you snoozed it: off the inbox until it changes again
	Grants    map[string]string    `json:"grants,omitempty"`   // agent id -> what it may organize through regia mcp: a group id, or "*" for the whole tree
	Claims    map[string]string    `json:"claims,omitempty"`   // agent id -> why it claimed its grant itself (regia mcp claim); none: you granted it (O)

	Spawns map[string]spawns.Link `json:"-"` // child -> spawner, from spawns.jsonl (not saved)

	mtime time.Time // file mtime at last load/save, to notice other writers
	base  []byte    // the file as last loaded or saved: what Save merges against
}

// ChangedOnDisk reports whether another process (a second regia) saved the
// layout since this copy was loaded or saved.
func (l *Layout) ChangedOnDisk() bool {
	fi, err := os.Stat(Path())
	return err == nil && !fi.ModTime().Equal(l.mtime)
}

type Row struct {
	ID        string
	Depth     int
	Group     bool
	Title     string
	Agent     *claude.Agent
	HasKids   bool
	Collapsed bool
	Archived  bool
	Count     int      // visible agents below
	Blocked   int      // agents below waiting on you (●): blocked, not stale or snoozed
	Ready     int      // agents below with a result you haven't seen (bold ✓)
	Busy      int      // agents below working right now (◆)
	Where     []string // inbox only: the ancestors' titles, outermost first (see Trail)
}

func Path() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "regia", "layout.json")
}

func Load() *Layout {
	l := &Layout{}
	if b, err := os.ReadFile(Path()); err == nil {
		_ = json.Unmarshal(b, l)
		l.base = b
	}
	if fi, err := os.Stat(Path()); err == nil {
		l.mtime = fi.ModTime()
	}
	l.fill()
	return l
}

// fill makes the maps a loaded layout may lack, so callers can write to them.
func (l *Layout) fill() {
	if l.Groups == nil {
		l.Groups = map[string]string{}
	}
	if l.Parent == nil {
		l.Parent = map[string]string{}
	}
	if l.Order == nil {
		l.Order = map[string][]string{}
	}
	if l.Alias == nil {
		l.Alias = map[string]string{}
	}
	if l.Archived == nil {
		l.Archived = map[string]time.Time{}
	}
	if l.Collapsed == nil {
		l.Collapsed = map[string]bool{}
	}
	if l.Pinned == nil {
		l.Pinned = map[string]bool{}
	}
	if l.Muted == nil {
		l.Muted = map[string]bool{}
	}
	if l.Dirs == nil {
		l.Dirs = map[string]string{}
	}
	if l.Pins == nil {
		l.Pins = map[string]bool{}
	}
	if l.Claims == nil {
		l.Claims = map[string]string{}
	}
	if l.Grants == nil {
		l.Grants = map[string]string{}
	}
}

// Save writes l. When another process saved since l was loaded, the two are
// merged first (see merge3): l's own changes win, everything else is kept,
// and l becomes the merged layout.
func (l *Layout) Save() error {
	return locked(func() error {
		disk, _ := os.ReadFile(Path())
		if len(disk) > 0 && !bytes.Equal(disk, l.base) {
			ours, err := json.Marshal(l)
			if err != nil {
				return err
			}
			merged, err := merge3(l.base, ours, disk)
			if err != nil {
				return err
			}
			if err := l.adopt(merged); err != nil {
				return err
			}
		}
		return l.write()
	})
}

func (l *Layout) IsGroup(id string) bool { _, ok := l.Groups[id]; return ok }

// autoParent is where an agent goes when regia decides: under the session it
// was forked from, else under the session that started it (`claude --bg`,
// learned from spawns.jsonl), else "" (its project group).
func (l *Layout) autoParent(a *claude.Agent, agents map[string]*claude.Agent) string {
	if fp := a.ForkParent; fp != "" && agents[fp] != nil && fp != a.ID {
		return fp
	}
	if sp := l.spawnParent(a.ID, agents); sp != "" && sp != a.ID {
		return sp
	}
	return ""
}

func (l *Layout) projectGroup(a *claude.Agent) string {
	gid := "p:" + claude.Project(a.Cwd)
	if !l.IsGroup(gid) {
		l.Groups[gid] = claude.Project(a.Cwd)
		l.Parent[gid] = ""
		l.Order[""] = append(l.Order[""], gid)
	}
	return gid
}

// Place assigns a parent to agents seen for the first time (see autoParent),
// and re-links agents still sitting in their auto project group once their
// spawner becomes known: the hook may write the link a moment after the agent
// first shows up, and a backfill finds links for agents placed long ago.
// Agents moved by hand (Pinned) are left alone. Returns true if the layout changed.
func (l *Layout) Place(agents map[string]*claude.Agent) bool {
	changed := false
	ordered := make([]*claude.Agent, 0, len(agents))
	for _, a := range agents {
		ordered = append(ordered, a)
	}
	claude.SortByStart(ordered)
	for _, a := range ordered {
		cur, placed := l.Parent[a.ID]
		if placed && (l.Pinned[a.ID] || !strings.HasPrefix(cur, "p:")) {
			continue
		}
		parent := l.autoParent(a, agents)
		if parent != "" && l.IsDescendant(parent, a.ID) {
			parent = ""
		}
		if placed && parent == "" {
			continue // stays in its project group
		}
		if parent == "" {
			parent = l.projectGroup(a)
		}
		if placed {
			l.Order[cur] = remove(l.Order[cur], a.ID)
		}
		l.Parent[a.ID] = parent
		l.Order[parent] = append(l.Order[parent], a.ID)
		changed = true
	}
	return changed
}

// effParent is id's nearest ancestor that still exists: when a coordinator is
// removed from the list its agents move up into its parent instead of vanishing.
func (l *Layout) effParent(id string, agents map[string]*claude.Agent) string {
	p := l.Parent[id]
	for n := 0; p != "" && !l.exists(p, agents) && n < 64; n++ {
		p = l.Parent[p]
	}
	return p
}

func (l *Layout) exists(id string, agents map[string]*claude.Agent) bool {
	return l.IsGroup(id) || agents[id] != nil
}

// Children returns the ordered Children of parent that currently exist.
func (l *Layout) Children(parent string, agents map[string]*claude.Agent) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range l.Order[parent] {
		if !seen[id] && l.exists(id, agents) && l.effParent(id, agents) == parent {
			seen[id] = true
			out = append(out, id)
		}
	}
	// Nodes whose parent is set but missing from the order list.
	var extra []*claude.Agent
	for id := range l.Parent {
		if !seen[id] && l.exists(id, agents) && l.effParent(id, agents) == parent {
			if a := agents[id]; a != nil {
				extra = append(extra, a)
			} else {
				out = append(out, id)
			}
			seen[id] = true
		}
	}
	claude.SortByStart(extra)
	for _, a := range extra {
		out = append(out, a.ID)
	}
	return out
}

type ViewOpts struct {
	ShowDone     bool
	ShowArchived bool
	Filter       string
	Keep         string // always shown (the selected agent, so it doesn't vanish while you read it)
}

// Visible reports whether an agent passes the filter and the done toggle. A
// pinned agent stays even when done; the filter still applies to it.
func (o ViewOpts) Visible(a *claude.Agent, alias string, pinned bool) bool {
	if o.Filter != "" {
		f := strings.ToLower(o.Filter)
		hay := strings.ToLower(strings.Join([]string{a.Name, alias, a.ID, a.Cwd, a.Needs, a.Detail, a.Branch}, " "))
		return strings.Contains(hay, f)
	}
	if !o.ShowDone && a.State == "done" && a.ID != o.Keep && !pinned {
		return false
	}
	return true
}

// Flatten walks the tree into display rows. A hidden node still shows when a
// descendant is visible; auto groups with nothing visible are dropped.
func (l *Layout) Flatten(agents map[string]*claude.Agent, o ViewOpts) []Row {
	var rows []Row
	var walk func(id string, depth int, visited map[string]bool) (rs []Row, n Tally)
	walk = func(id string, depth int, visited map[string]bool) ([]Row, Tally) {
		if visited[id] || depth > 32 {
			return nil, Tally{}
		}
		visited[id] = true
		kids := l.Children(id, agents)
		var sub []Row
		var n Tally
		for _, k := range kids {
			r, t := walk(k, depth+1, visited)
			sub = append(sub, r...)
			n.add(t)
		}
		row := Row{ID: id, Depth: depth, HasKids: len(sub) > 0}
		selfVisible := false
		if a := agents[id]; a != nil {
			row.Agent = a
			row.Title = a.Name
			if al := l.Alias[id]; al != "" {
				row.Title = al
			}
			row.Archived = l.IsArchived(id)
			// archived: hidden unless shown (A) or searched for
			selfVisible = o.Visible(a, l.Alias[id], l.Pins[id]) && (!row.Archived || o.ShowArchived || o.Filter != "")
			if selfVisible {
				n.Count++
				n.Tick(a)
			}
		} else {
			row.Group = true
			row.Title = l.Groups[id]
			selfVisible = strings.HasPrefix(id, "g:") && o.Filter == ""
		}
		if !selfVisible && len(sub) == 0 {
			return nil, Tally{}
		}
		row.Count, row.Blocked, row.Ready, row.Busy = n.Count, n.Blocked, n.Ready, n.Busy
		row.Collapsed = l.Collapsed[id] && o.Filter == ""
		out := []Row{row}
		if !row.Collapsed {
			out = append(out, sub...)
		}
		return out, n
	}
	visited := map[string]bool{}
	for _, id := range l.Children("", agents) {
		r, _ := walk(id, 0, visited)
		rows = append(rows, r...)
	}
	return rows
}

// --- archive ----------------------------------------------------------------

func (l *Layout) IsArchived(id string) bool { _, ok := l.Archived[id]; return ok }

// Archive hides agents from regia's tree; Claude Code is not touched. You
// put them away, so they lose their pin.
func (l *Layout) Archive(ids ...string) {
	now := time.Now()
	for _, id := range ids {
		l.Archived[id] = now
		delete(l.Pins, id)
	}
}

// --- pins -------------------------------------------------------------------

// SetPins pins (on) or unpins agents. A pinned agent stays in the tree when
// it is done, like one still working; pinning an archived one brings it back.
func (l *Layout) SetPins(on bool, ids ...string) {
	if l.Pins == nil {
		l.Pins = map[string]bool{}
	}
	for _, id := range ids {
		if on {
			l.Pins[id] = true
			delete(l.Archived, id)
		} else {
			delete(l.Pins, id)
		}
	}
}

func (l *Layout) Unarchive(ids ...string) {
	for _, id := range ids {
		delete(l.Archived, id)
	}
}

// Revive unarchives agents that are working again (someone resumed them),
// reporting whether anything changed.
func (l *Layout) Revive(agents map[string]*claude.Agent) bool {
	changed := false
	for id := range l.Archived {
		if a := agents[id]; a != nil && a.State == "busy" {
			delete(l.Archived, id)
			changed = true
		}
	}
	return changed
}

// --- editing operations -------------------------------------------------

func (l *Layout) materialize(parent string, agents map[string]*claude.Agent) []string {
	l.Order[parent] = l.Children(parent, agents)
	return l.Order[parent]
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}

func remove(xs []string, x string) []string {
	if i := indexOf(xs, x); i >= 0 {
		return append(xs[:i:i], xs[i+1:]...)
	}
	return xs
}

func insertAfter(xs []string, after, x string) []string {
	i := indexOf(xs, after)
	if i < 0 {
		return append(xs, x)
	}
	out := append([]string{}, xs[:i+1]...)
	out = append(out, x)
	return append(out, xs[i+1:]...)
}

// Move swaps id with its nearest visible sibling in direction dir (-1 / +1).
func (l *Layout) Move(id string, dir int, agents map[string]*claude.Agent, visible map[string]bool) bool {
	p := l.Parent[id]
	sib := l.materialize(p, agents)
	i := indexOf(sib, id)
	for j := i + dir; j >= 0 && j < len(sib); j += dir {
		if visible[sib[j]] {
			sib[i], sib[j] = sib[j], sib[i]
			return true
		}
	}
	return false
}

func (l *Layout) IsDescendant(id, of string) bool {
	for cur, n := id, 0; cur != "" && n < 64; n++ {
		if cur == of {
			return true
		}
		cur = l.Parent[cur]
	}
	return false
}

// PlaceNew puts an agent you just started under parent, after `after` (or at
// the end), and pins it there: you chose the place.
func (l *Layout) PlaceNew(id, parent, after string) {
	l.Parent[id] = parent
	l.Order[parent] = insertAfter(remove(l.Order[parent], id), after, id)
	l.Pinned[id] = true
}

// Reparent moves id under newParent, after `after` (or at the end).
func (l *Layout) Reparent(id, newParent, after string, agents map[string]*claude.Agent) error {
	if id == newParent || (newParent != "" && l.IsDescendant(newParent, id)) {
		return fmt.Errorf("can't move a node inside itself")
	}
	old := l.Parent[id]
	l.materialize(old, agents)
	l.Order[old] = remove(l.Order[old], id)
	l.materialize(newParent, agents)
	l.Parent[id] = newParent
	l.Order[newParent] = insertAfter(remove(l.Order[newParent], id), after, id)
	l.Pinned[id] = true
	delete(l.Collapsed, newParent)
	return nil
}

// EffParent is id's parent as shown (the nearest ancestor still listed).
func (l *Layout) EffParent(id string, agents map[string]*claude.Agent) string {
	return l.effParent(id, agents)
}

// Put moves ids, in this order, under parent right after `after` ("": at the
// top), and pins them there. Nodes under a moved node go with it.
func (l *Layout) Put(ids []string, parent, after string, agents map[string]*claude.Agent) error {
	moving := map[string]bool{}
	for _, id := range ids {
		if id == parent || (parent != "" && l.IsDescendant(parent, id)) {
			return fmt.Errorf("can't move a node inside itself")
		}
		if id == after {
			return fmt.Errorf("can't put a node next to itself")
		}
		moving[id] = true
	}
	for _, id := range ids {
		old := l.Parent[id]
		if old != parent {
			l.materialize(old, agents)
			l.Order[old] = remove(l.Order[old], id)
		}
	}
	var sib []string
	for _, id := range l.materialize(parent, agents) {
		if !moving[id] {
			sib = append(sib, id)
		}
	}
	i := 0
	if after != "" {
		i = indexOf(sib, after) + 1 // not there: at the top
	}
	out := append(append(append([]string{}, sib[:i]...), ids...), sib[i:]...)
	l.Order[parent] = out
	for _, id := range ids {
		l.Parent[id] = parent
		l.Pinned[id] = true
	}
	delete(l.Collapsed, parent)
	return nil
}

// Topmost drops the ids that sit under another id of the list: they move
// with it.
func (l *Layout) Topmost(ids []string) []string {
	in := map[string]bool{}
	for _, id := range ids {
		in[id] = true
	}
	var out []string
	for _, id := range ids {
		top := true
		for p, n := l.Parent[id], 0; p != "" && n < 64; p, n = l.Parent[p], n+1 {
			if in[p] {
				top = false
				break
			}
		}
		if top {
			out = append(out, id)
		}
	}
	return out
}

// GroupOf makes a new group where the first of ids is and moves ids into it.
func (l *Layout) GroupOf(title string, ids []string, agents map[string]*claude.Agent) (string, error) {
	ids = l.Topmost(ids)
	if len(ids) == 0 {
		return "", fmt.Errorf("nothing to group")
	}
	gid := l.NewGroup(title, ids[0], agents)
	if err := l.Put(ids, gid, "", agents); err != nil {
		l.DeleteGroup(gid, agents)
		return "", err
	}
	if d, n, all := l.agentDirs(gid, agents); all && n > 0 {
		l.Dirs[gid] = d // they all work in one place: so will new agents
	}
	return gid, nil
}

// Indent makes id a child of its previous visible sibling (coordinator).
func (l *Layout) Indent(id string, agents map[string]*claude.Agent, visible map[string]bool) error {
	sib := l.materialize(l.Parent[id], agents)
	for j := indexOf(sib, id) - 1; j >= 0; j-- {
		if visible[sib[j]] {
			return l.Reparent(id, sib[j], "", agents)
		}
	}
	return fmt.Errorf("no sibling above to nest under")
}

func (l *Layout) Outdent(id string, agents map[string]*claude.Agent) error {
	p := l.Parent[id]
	if p == "" {
		return fmt.Errorf("already at top level")
	}
	return l.Reparent(id, l.Parent[p], p, agents)
}

// NewGroupIn makes an empty group at the end of parent ("" for the top).
func (l *Layout) NewGroupIn(title, parent string, agents map[string]*claude.Agent) string {
	gid := fmt.Sprintf("g:%d", time.Now().UnixNano())
	l.Groups[gid] = title
	l.materialize(parent, agents)
	l.Parent[gid] = parent
	l.Order[parent] = append(l.Order[parent], gid)
	return gid
}

func (l *Layout) NewGroup(title, near string, agents map[string]*claude.Agent) string {
	gid := fmt.Sprintf("g:%d", time.Now().UnixNano())
	l.Groups[gid] = title
	parent := ""
	if near != "" {
		parent = l.effParent(near, agents)
	}
	l.materialize(parent, agents)
	l.Parent[gid] = parent
	l.Order[parent] = insertAfter(l.Order[parent], near, gid)
	return gid
}

// DeleteGroup lifts the group's children into its place.
func (l *Layout) DeleteGroup(gid string, agents map[string]*claude.Agent) {
	parent := l.Parent[gid]
	kids := l.materialize(gid, agents)
	sib := l.materialize(parent, agents)
	i := indexOf(sib, gid)
	if i < 0 {
		sib, i = append(sib, gid), len(sib)
	}
	out := append([]string{}, sib[:i]...)
	for _, k := range kids {
		l.Parent[k] = parent
		out = append(out, k)
	}
	l.Order[parent] = append(out, sib[i+1:]...)
	delete(l.Order, gid)
	delete(l.Groups, gid)
	delete(l.Parent, gid)
	delete(l.Collapsed, gid)
	delete(l.Dirs, gid)
}

// Trail is where id sits in the tree: its ancestors' titles, outermost first
// (groups by title, coordinators by alias or name).
func (l *Layout) Trail(id string, agents map[string]*claude.Agent) []string {
	var t []string
	for p, n := l.effParent(id, agents), 0; p != "" && n < 32; p, n = l.effParent(p, agents), n+1 {
		title := l.Groups[p]
		if a := agents[p]; a != nil {
			title = a.Name
			if al := l.Alias[p]; al != "" {
				title = al
			}
		}
		t = append([]string{title}, t...)
	}
	return t
}

// spawnParent is where a child's spawner puts it: the spawner when it is
// listed, else, when the spawner is gone (removed, ended), the spawner's own
// place: where it sat in the tree (a coordinator or a hand-made group, not
// its project group), or its own spawner, climbing until something is there.
// "" when nothing is.
func (l *Layout) spawnParent(child string, agents map[string]*claude.Agent) string {
	seen := map[string]bool{child: true}
	for id := child; len(seen) < 64; {
		link, ok := l.Spawns[id]
		if !ok {
			return ""
		}
		if p := listedSpawner(link, agents); p != "" {
			return p
		}
		gone := link.Parent
		if seen[gone] {
			return ""
		}
		seen[gone] = true
		if p := l.effParent(gone, agents); p != "" && !strings.HasPrefix(p, "p:") {
			return p
		}
		id = gone
	}
	return ""
}

// listedSpawner resolves a link's spawner to a listed agent id, if any.
func listedSpawner(l spawns.Link, agents map[string]*claude.Agent) string {
	if agents[l.Parent] != nil {
		return l.Parent
	}
	// A resumed session can carry a session id that differs from its job id.
	if l.ParentSession != "" {
		for id, a := range agents {
			if a.SessionID == l.ParentSession {
				return id
			}
		}
	}
	return ""
}

// Tally counts agents the way the tree draws them: ● waiting on you, bold ✓
// a result you haven't seen, ◆ working. Each agent lands in at most one.
type Tally struct{ Count, Blocked, Ready, Busy int }

// Tick counts a's state (not Count: the caller decides what is visible).
func (t *Tally) Tick(a *claude.Agent) {
	switch {
	case a.State == "blocked" && a.YourTurn():
		t.Blocked++
	case a.State == "ready":
		t.Ready++
	case a.State == "busy":
		t.Busy++
	}
}

func (t *Tally) add(o Tally) {
	t.Count, t.Blocked, t.Ready, t.Busy = t.Count+o.Count, t.Blocked+o.Blocked, t.Ready+o.Ready, t.Busy+o.Busy
}
