package group

import (
	"os"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/gitinfo"
)

// PRRef is where and what to ask gh about for a's pull request (dir, and a
// PR URL, number or branch), or "" when there's nothing: the last "pr" link
// in its state.json, or else its branch unless that is a default one. Shared
// by the TUI's card and inbox and by regia mcp's inbox.
func PRRef(a *claude.Agent) (dir, ref string) {
	if a == nil || a.Interactive {
		return "", ""
	}
	dir = a.Cwd
	if dir != "" {
		if _, err := os.Stat(dir); err != nil {
			dir = "" // worktree removed: a URL still works from anywhere
		}
	}
	for i := len(a.PRs) - 1; i >= 0; i-- {
		if p := a.PRs[i]; p.Kind == "pr" && (p.Href != "" || p.ID != "") {
			ref = p.Href
			if ref == "" {
				ref = p.ID
				if dir == "" {
					return "", ""
				}
			}
			if dir == "" {
				dir, _ = os.UserHomeDir()
			}
			return dir, ref
		}
	}
	if dir == "" {
		return "", ""
	}
	b := a.Branch
	if gi, ok := gitinfo.Cached(a.Cwd); ok && gi.OK && gi.Branch != "" {
		b = gi.Branch
	}
	switch b {
	case "", "main", "master", "develop", "trunk":
		return "", "" // the default branch has no PR of its own
	}
	return dir, b
}
