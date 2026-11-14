package main

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// GitInfo is what the card shows about the agent's working directory.
type GitInfo struct {
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
	info GitInfo
	at   time.Time
}

var (
	gitMu       sync.Mutex
	gitCache    = map[string]gitEntry{}
	gitInFlight = map[string]bool{}
)

type gitMsg struct{ dir string }

func cachedGit(dir string) (GitInfo, bool) {
	gitMu.Lock()
	defer gitMu.Unlock()
	e, ok := gitCache[dir]
	return e.info, ok
}

// gitCmd refreshes dir's info in the background when it's stale.
func gitCmd(dir string) tea.Cmd {
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
	return func() tea.Msg {
		info := readGit(dir)
		gitMu.Lock()
		gitCache[dir] = gitEntry{info, time.Now()}
		delete(gitInFlight, dir)
		gitMu.Unlock()
		return gitMsg{dir}
	}
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "--no-optional-locks"}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

func readGit(dir string) GitInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var g GitInfo
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

// gitLines renders the card's GIT field: branch line + last commit line.
func (g GitInfo) lines() (string, string) {
	if !g.OK {
		return "", ""
	}
	b := g.Branch
	if b == "" {
		b = "(detached " + g.Head + ")"
	}
	parts := []string{"⎇ " + b}
	if g.Worktree {
		parts = append(parts, "worktree")
	}
	switch {
	case g.Upstream == "":
		parts = append(parts, "no upstream")
	case g.Ahead == 0 && g.Behind == 0:
		parts = append(parts, "in sync")
	default:
		ab := ""
		if g.Ahead > 0 {
			ab += "↑" + strconv.Itoa(g.Ahead)
		}
		if g.Behind > 0 {
			ab += "↓" + strconv.Itoa(g.Behind)
		}
		parts = append(parts, ab)
	}
	switch {
	case g.Changed == 0 && g.Untracked == 0:
		parts = append(parts, "clean")
	default:
		var d []string
		if g.Changed > 0 {
			d = append(d, strconv.Itoa(g.Changed)+" changed")
		}
		if g.Untracked > 0 {
			d = append(d, strconv.Itoa(g.Untracked)+" untracked")
		}
		parts = append(parts, strings.Join(d, ", "))
	}
	last := ""
	if g.Head != "" {
		last = g.Head + " " + g.Subject + " · " + ago(g.CommitAt) + " ago"
	}
	return strings.Join(parts, " · "), last
}
