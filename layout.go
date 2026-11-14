package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Layout is the user's own organization of agents, independent of Claude Code.
// Nodes are agent ids or group ids ("g:..." user groups, "p:..." auto project
// groups). Any agent can be a parent: that is how a coordinator holds its agents.
type Layout struct {
	Groups    map[string]string   `json:"groups"`    // group id -> title
	Parent    map[string]string   `json:"parent"`    // node -> parent node ("" = root)
	Order     map[string][]string `json:"order"`     // parent -> ordered children
	Alias     map[string]string   `json:"alias"`     // agent id -> display name
	Collapsed map[string]bool     `json:"collapsed"` // node -> collapsed
	Pinned    map[string]bool     `json:"pinned"`    // placed by hand: never re-linked automatically

	spawns map[string]spawnLink // child -> spawner, from spawns.jsonl (not saved here)

	mtime time.Time // file mtime at last load/save, to notice other writers
}

type Row struct {
	ID        string
	Depth     int
	Group     bool
	Title     string
	Agent     *Agent
	HasKids   bool
	Collapsed bool
	Count     int // visible agents below
	Blocked   int // blocked agents below
	Busy      int
}

func layoutPath() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "regia", "layout.json")
}

func loadLayout() *Layout {
	l := &Layout{}
	if b, err := os.ReadFile(layoutPath()); err == nil {
		_ = json.Unmarshal(b, l)
	}
	if fi, err := os.Stat(layoutPath()); err == nil {
		l.mtime = fi.ModTime()
	}
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
	if l.Collapsed == nil {
		l.Collapsed = map[string]bool{}
	}
	if l.Pinned == nil {
		l.Pinned = map[string]bool{}
	}
	return l
}

func (l *Layout) Save() error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	p := layoutPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	if fi, err := os.Stat(p); err == nil {
		l.mtime = fi.ModTime()
	}
	return nil
}

func (l *Layout) isGroup(id string) bool { _, ok := l.Groups[id]; return ok }

// autoParent is where an agent goes when regia decides: under the session it
// was forked from, else under the session that started it (`claude --bg`,
// learned from spawns.jsonl), else "" (its project group).
func (l *Layout) autoParent(a *Agent, agents map[string]*Agent) string {
	if fp := a.ForkParent; fp != "" && agents[fp] != nil && fp != a.ID {
		return fp
	}
	if sp := spawnParent(l.spawns, a.ID, agents); sp != "" && sp != a.ID {
		return sp
	}
	return ""
}

func (l *Layout) projectGroup(a *Agent) string {
	gid := "p:" + project(a.Cwd)
	if !l.isGroup(gid) {
		l.Groups[gid] = project(a.Cwd)
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
func (l *Layout) Place(agents map[string]*Agent) bool {
	changed := false
	ordered := make([]*Agent, 0, len(agents))
	for _, a := range agents {
		ordered = append(ordered, a)
	}
	sortByStart(ordered)
	for _, a := range ordered {
		cur, placed := l.Parent[a.ID]
		if placed && (l.Pinned[a.ID] || !strings.HasPrefix(cur, "p:")) {
			continue
		}
		parent := l.autoParent(a, agents)
		if parent != "" && l.isDescendant(parent, a.ID) {
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
func (l *Layout) effParent(id string, agents map[string]*Agent) string {
	p := l.Parent[id]
	for n := 0; p != "" && !l.exists(p, agents) && n < 64; n++ {
		p = l.Parent[p]
	}
	return p
}

func (l *Layout) exists(id string, agents map[string]*Agent) bool {
	return l.isGroup(id) || agents[id] != nil
}

// children returns the ordered children of parent that currently exist.
func (l *Layout) children(parent string, agents map[string]*Agent) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range l.Order[parent] {
		if !seen[id] && l.exists(id, agents) && l.effParent(id, agents) == parent {
			seen[id] = true
			out = append(out, id)
		}
	}
	// Nodes whose parent is set but missing from the order list.
	var extra []*Agent
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
	sortByStart(extra)
	for _, a := range extra {
		out = append(out, a.ID)
	}
	return out
}

type viewOpts struct {
	showDone bool
	filter   string
}

func (o viewOpts) visible(a *Agent, alias string) bool {
	if o.filter != "" {
		f := strings.ToLower(o.filter)
		hay := strings.ToLower(strings.Join([]string{a.Name, alias, a.ID, a.Cwd, a.Needs, a.Detail, a.Branch}, " "))
		return strings.Contains(hay, f)
	}
	if !o.showDone && (a.State == "done") {
		return false
	}
	return true
}

// Flatten walks the tree into display rows. A hidden node still shows when a
// descendant is visible; auto groups with nothing visible are dropped.
func (l *Layout) Flatten(agents map[string]*Agent, o viewOpts) []Row {
	var rows []Row
	var walk func(id string, depth int, visited map[string]bool) (rs []Row, count, blocked, busy int)
	walk = func(id string, depth int, visited map[string]bool) ([]Row, int, int, int) {
		if visited[id] || depth > 32 {
			return nil, 0, 0, 0
		}
		visited[id] = true
		kids := l.children(id, agents)
		var sub []Row
		count, blocked, busy := 0, 0, 0
		for _, k := range kids {
			r, c, b, u := walk(k, depth+1, visited)
			sub = append(sub, r...)
			count, blocked, busy = count+c, blocked+b, busy+u
		}
		row := Row{ID: id, Depth: depth, HasKids: len(sub) > 0}
		selfVisible := false
		if a := agents[id]; a != nil {
			row.Agent = a
			row.Title = a.Name
			if al := l.Alias[id]; al != "" {
				row.Title = al
			}
			selfVisible = o.visible(a, l.Alias[id])
			if selfVisible {
				count++
				if a.State == "blocked" {
					blocked++
				}
				if a.State == "busy" {
					busy++
				}
			}
		} else {
			row.Group = true
			row.Title = l.Groups[id]
			selfVisible = strings.HasPrefix(id, "g:") && o.filter == ""
		}
		if !selfVisible && len(sub) == 0 {
			return nil, 0, 0, 0
		}
		row.Count, row.Blocked, row.Busy = count, blocked, busy
		row.Collapsed = l.Collapsed[id] && o.filter == ""
		out := []Row{row}
		if !row.Collapsed {
			out = append(out, sub...)
		}
		return out, count, blocked, busy
	}
	visited := map[string]bool{}
	for _, id := range l.children("", agents) {
		r, _, _, _ := walk(id, 0, visited)
		rows = append(rows, r...)
	}
	return rows
}

// --- editing operations -------------------------------------------------

func (l *Layout) materialize(parent string, agents map[string]*Agent) []string {
	l.Order[parent] = l.children(parent, agents)
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
func (l *Layout) Move(id string, dir int, agents map[string]*Agent, visible map[string]bool) bool {
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

func (l *Layout) isDescendant(id, of string) bool {
	for cur, n := id, 0; cur != "" && n < 64; n++ {
		if cur == of {
			return true
		}
		cur = l.Parent[cur]
	}
	return false
}

// Reparent moves id under newParent, after `after` (or at the end).
func (l *Layout) Reparent(id, newParent, after string, agents map[string]*Agent) error {
	if id == newParent || (newParent != "" && l.isDescendant(newParent, id)) {
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

// Indent makes id a child of its previous visible sibling (coordinator).
func (l *Layout) Indent(id string, agents map[string]*Agent, visible map[string]bool) error {
	sib := l.materialize(l.Parent[id], agents)
	for j := indexOf(sib, id) - 1; j >= 0; j-- {
		if visible[sib[j]] {
			return l.Reparent(id, sib[j], "", agents)
		}
	}
	return fmt.Errorf("no sibling above to nest under")
}

func (l *Layout) Outdent(id string, agents map[string]*Agent) error {
	p := l.Parent[id]
	if p == "" {
		return fmt.Errorf("already at top level")
	}
	return l.Reparent(id, l.Parent[p], p, agents)
}

func (l *Layout) NewGroup(title, near string, agents map[string]*Agent) string {
	gid := fmt.Sprintf("g:%d", time.Now().UnixNano())
	l.Groups[gid] = title
	parent := ""
	if near != "" {
		parent = l.Parent[near]
	}
	l.materialize(parent, agents)
	l.Parent[gid] = parent
	l.Order[parent] = insertAfter(l.Order[parent], near, gid)
	return gid
}

// DeleteGroup lifts the group's children into its place.
func (l *Layout) DeleteGroup(gid string, agents map[string]*Agent) {
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
}
