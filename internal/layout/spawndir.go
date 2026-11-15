package layout

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/claude"
)

// Where a new agent starts. SpawnDir walks up the tree from the selected
// node and takes the first of:
//
//  1. an agent's directory, unless it is your home directory (agents in ~
//     were usually started from a terminal there, by accident);
//  2. a group's saved directory (Dirs, set with w);
//
// and, when the walk finds neither, a guess for the nearest group that has
// one:
//
//  3. the directory most of the group's agents work in;
//  4. ~/Projects/<name> or ~/Git/<name>, for the group's project or title;
//
// and finally your home directory.

// DirSource says why SpawnDir picked a directory.
type DirSource struct {
	Kind string // "agent", "group", "agents", "folder", "home"
	From string // the agent or group it came from
	N    int    // for "agents": how many agents work there
}

// RepoDir is where a directory's work lives: an agent's worktree
// (…/.claude/worktrees/x) belongs to that agent, so new agents start in the
// repository instead.
func RepoDir(dir string) string {
	repo, _, _ := strings.Cut(dir, "/.claude/worktrees/")
	return repo
}

// SpawnDir is the directory for a new agent placed at start (the selected
// node; "" when nothing is selected).
func (l *Layout) SpawnDir(start string, agents map[string]*claude.Agent) (string, DirSource) {
	home := claude.Home()
	var path []string
	for n, i := start, 0; n != "" && i < 64; n, i = l.effParent(n, agents), i+1 {
		path = append(path, n)
	}
	for _, n := range path {
		if a := agents[n]; a != nil {
			if d := RepoDir(a.Cwd); d != "" && d != home {
				return d, DirSource{Kind: "agent", From: n}
			}
		} else if d := l.Dirs[n]; d != "" && l.IsGroup(n) {
			return d, DirSource{Kind: "group", From: n}
		}
	}
	for _, n := range path {
		if !l.IsGroup(n) {
			continue
		}
		if d, k, _ := l.agentDirs(n, agents); k > 0 {
			return d, DirSource{Kind: "agents", From: n, N: k}
		}
		if d := l.folderFor(n); d != "" {
			return d, DirSource{Kind: "folder", From: n}
		}
	}
	return home, DirSource{Kind: "home"}
}

// Guess is what the group's directory would be if none were saved: from its
// agents or its name ("" when neither says).
func (l *Layout) Guess(gid string, agents map[string]*claude.Agent) (string, DirSource) {
	if d, k, _ := l.agentDirs(gid, agents); k > 0 {
		return d, DirSource{Kind: "agents", From: gid, N: k}
	}
	if d := l.folderFor(gid); d != "" {
		return d, DirSource{Kind: "folder", From: gid}
	}
	return "", DirSource{}
}

// agentDirs finds the directory most agents under node work in (home
// excluded; a tie goes to the most recently active), how many work there,
// and whether all of them (home included) do.
func (l *Layout) agentDirs(node string, agents map[string]*claude.Agent) (dir string, n int, all bool) {
	home := claude.Home()
	count := map[string]int{}
	last := map[string]time.Time{}
	total := 0
	var walk func(string, int)
	walk = func(id string, depth int) {
		if depth > 64 {
			return
		}
		for _, k := range l.Children(id, agents) {
			if a := agents[k]; a != nil {
				total++
				if d := RepoDir(a.Cwd); d != "" && d != home {
					count[d]++
					if t := a.UpdatedAt; t.After(last[d]) || last[d].IsZero() {
						last[d] = t
					}
				}
			}
			walk(k, depth+1)
		}
	}
	walk(node, 0)
	for d, c := range count {
		if c > n || (c == n && (last[d].After(last[dir]) || (last[d].Equal(last[dir]) && d < dir))) {
			dir, n = d, c
		}
	}
	return dir, n, n > 0 && n == total
}

// folderFor is ~/Projects/<name> or ~/Git/<name>, when it exists, for an
// auto project group's project or a group's title.
func (l *Layout) folderFor(gid string) string {
	name := l.Groups[gid]
	if p, ok := strings.CutPrefix(gid, "p:"); ok {
		name = p
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "~" || name == "." || name == ".." || strings.ContainsRune(name, '/') {
		return ""
	}
	home := claude.Home()
	for _, root := range []string{"Projects", "Git"} {
		d := filepath.Join(home, root, name)
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return ""
}
