// Package layout is the user's own organization of agents: groups,
// coordinators with their agents, order, aliases and folds, saved in
// ~/.config/regia/layout.json. It places new agents automatically (fork
// parent, spawner, project group) and flattens the tree into display rows.
package layout

import (
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
	Archived  map[string]time.Time `json:"archived,omitempty"` // agent id -> when: hidden by regia, untouched in Claude

	Spawns map[string]spawns.Link `json:"-"` // child -> spawner, from spawns.jsonl (not saved)

	mtime time.Time // file mtime at last load/save, to notice other writers
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
	Count     int // visible agents below
	Blocked   int // blocked agents below
	Busy      int
}

func Path() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "regia", "layout.json")
}

func Load() *Layout {
	l := &Layout{}
	if b, err := os.ReadFile(Path()); err == nil {
		_ = json.Unmarshal(b, l)
	}
	if fi, err := os.Stat(Path()); err == nil {
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
	if l.Archived == nil {
		l.Archived = map[string]time.Time{}
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
	p := Path()
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

func (l *Layout) IsGroup(id string) bool { _, ok := l.Groups[id]; return ok }

// autoParent is where an agent goes when regia decides: under the session it
// was forked from, else under the session that started it (`claude --bg`,
// learned from spawns.jsonl), else "" (its project group).
func (l *Layout) autoParent(a *claude.Agent, agents map[string]*claude.Agent) string {
	if fp := a.ForkParent; fp != "" && agents[fp] != nil && fp != a.ID {
		return fp
	}
	if sp := spawnParent(l.Spawns, a.ID, agents); sp != "" && sp != a.ID {
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
}

func (o ViewOpts) Visible(a *claude.Agent, alias string) bool {
	if o.Filter != "" {
		f := strings.ToLower(o.Filter)
		hay := strings.ToLower(strings.Join([]string{a.Name, alias, a.ID, a.Cwd, a.Needs, a.Detail, a.Branch}, " "))
		return strings.Contains(hay, f)
	}
	if !o.ShowDone && (a.State == "done") {
		return false
	}
	return true
}

// Flatten walks the tree into display rows. A hidden node still shows when a
// descendant is visible; auto groups with nothing visible are dropped.
func (l *Layout) Flatten(agents map[string]*claude.Agent, o ViewOpts) []Row {
	var rows []Row
	var walk func(id string, depth int, visited map[string]bool) (rs []Row, count, blocked, busy int)
	walk = func(id string, depth int, visited map[string]bool) ([]Row, int, int, int) {
		if visited[id] || depth > 32 {
			return nil, 0, 0, 0
		}
		visited[id] = true
		kids := l.Children(id, agents)
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
			row.Archived = l.IsArchived(id)
			// archived: hidden unless shown (A) or searched for
			selfVisible = o.Visible(a, l.Alias[id]) && (!row.Archived || o.ShowArchived || o.Filter != "")
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
			selfVisible = strings.HasPrefix(id, "g:") && o.Filter == ""
		}
		if !selfVisible && len(sub) == 0 {
			return nil, 0, 0, 0
		}
		row.Count, row.Blocked, row.Busy = count, blocked, busy
		row.Collapsed = l.Collapsed[id] && o.Filter == ""
		out := []Row{row}
		if !row.Collapsed {
			out = append(out, sub...)
		}
		return out, count, blocked, busy
	}
	visited := map[string]bool{}
	for _, id := range l.Children("", agents) {
		r, _, _, _ := walk(id, 0, visited)
		rows = append(rows, r...)
	}
	return rows
}

// --- archive ----------------------------------------------------------------

func (l *Layout) IsArchived(id string) bool { _, ok := l.Archived[id]; return ok }

// Archive hides agents from regia's tree; Claude Code is not touched.
func (l *Layout) Archive(ids ...string) {
	now := time.Now()
	for _, id := range ids {
		l.Archived[id] = now
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

func (l *Layout) NewGroup(title, near string, agents map[string]*claude.Agent) string {
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
}

// spawnParent resolves a child's spawner to a listed agent id, if any.
func spawnParent(links map[string]spawns.Link, child string, agents map[string]*claude.Agent) string {
	l, ok := links[child]
	if !ok {
		return ""
	}
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
