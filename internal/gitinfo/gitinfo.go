// Package gitinfo reads the git state of an agent's working directory
// (branch, upstream, ahead/behind, changes, last commit) with a short cache.
package gitinfo

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Info is what the card shows about the agent's working directory.
type Info struct {
	OK        bool
	Branch    string // "" when detached
	Upstream  string
	Ahead     int
	Behind    int
	Changed   int // tracked files with changes (staged or not)
	Untracked int
	Head      string
	Subject   string
	CommitAt  time.Time
	Worktree  bool // a linked worktree, not the main checkout
}

const gitTTL = 5 * time.Second

type gitEntry struct {
	info Info
	at   time.Time
}

var (
	gitMu       sync.Mutex
	gitCache    = map[string]gitEntry{}
	gitInFlight = map[string]bool{}
)

func Cached(dir string) (Info, bool) {
	gitMu.Lock()
	defer gitMu.Unlock()
	e, ok := gitCache[dir]
	return e.info, ok
}

// Refresher returns a function that re-reads dir's info into the cache, or
// nil when the cached info is fresh or a refresh is already running. The
// caller runs it in the background.
func Refresher(dir string) func() {
	if dir == "" {
		return nil
	}
	gitMu.Lock()
	e, ok := gitCache[dir]
	if (ok && time.Since(e.at) < gitTTL) || gitInFlight[dir] {
		gitMu.Unlock()
		return nil
	}
	gitInFlight[dir] = true
	gitMu.Unlock()
	return func() {
		info := Read(dir)
		gitMu.Lock()
		gitCache[dir] = gitEntry{info, time.Now()}
		delete(gitInFlight, dir)
		gitMu.Unlock()
	}
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "--no-optional-locks"}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

func Read(dir string) Info {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var g Info
	st, err := git(ctx, dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return g
	}
	g.OK = true
	for _, l := range strings.Split(st, "\n") {
		f := strings.Fields(l)
		switch {
		case strings.HasPrefix(l, "# branch.head "):
			if b := strings.TrimPrefix(l, "# branch.head "); b != "(detached)" {
				g.Branch = b
			}
		case strings.HasPrefix(l, "# branch.upstream "):
			g.Upstream = strings.TrimPrefix(l, "# branch.upstream ")
		case strings.HasPrefix(l, "# branch.ab ") && len(f) == 4:
			g.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
			g.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
		case strings.HasPrefix(l, "1 "), strings.HasPrefix(l, "2 "), strings.HasPrefix(l, "u "):
			g.Changed++
		case strings.HasPrefix(l, "? "):
			g.Untracked++
		}
	}
	if lg, err := git(ctx, dir, "log", "-1", "--format=%h%x00%s%x00%ct"); err == nil {
		if p := strings.SplitN(strings.TrimSpace(lg), "\x00", 3); len(p) == 3 {
			g.Head, g.Subject = p[0], p[1]
			if ts, err := strconv.ParseInt(p[2], 10, 64); err == nil {
				g.CommitAt = time.Unix(ts, 0)
			}
		}
	}
	if gd, err := git(ctx, dir, "rev-parse", "--git-dir", "--git-common-dir"); err == nil {
		if p := strings.Fields(gd); len(p) == 2 && p[0] != p[1] {
			g.Worktree = true
		}
	}
	return g
}
