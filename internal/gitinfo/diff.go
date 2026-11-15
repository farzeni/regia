package gitinfo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Diff is an agent's changes: its working tree against the merge-base with the
// repo's default branch (committed and uncommitted), for the panel's diff view.
type Diff struct {
	Err   string // "directory gone", "no git repo", …; empty when OK
	Base  string // the branch it was compared with (main/master), "" if none found
	Stat  string // git diff --stat
	Body  string // the unified diff, capped at MaxDiffLines
	Cut   int    // lines left out of Body by the cap
	Empty bool   // no changes
}

// MaxDiffLines caps Body; the rest is counted in Cut.
const MaxDiffLines = 5000

var (
	diffMu    sync.Mutex
	diffCache = map[string]Diff{} // key: dir + HEAD + porcelain status
)

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return string(out), err
}

// DefaultBranch guesses the repo's default branch: origin/HEAD, else main,
// master and their origin/ counterparts. "" when none exists.
func DefaultBranch(ctx context.Context, dir string) string {
	if s, err := gitOut(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b := strings.TrimSpace(s); b != "" {
			return b
		}
	}
	for _, b := range []string{"main", "master", "origin/main", "origin/master"} {
		if _, err := gitOut(ctx, dir, "rev-parse", "--verify", "-q", b); err == nil {
			return b
		}
	}
	return ""
}

// DiffKey is the cache key for dir's current state; "" when dir has no repo.
func diffKey(ctx context.Context, dir string) (string, error) {
	head, err := gitOut(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	status, _ := gitOut(ctx, dir, "status", "--porcelain")
	return dir + "\x00" + strings.TrimSpace(head) + "\x00" + status, nil
}

// LoadDiff computes (or returns the cached) Diff for dir. It runs git, so
// call it from a tea.Cmd, never in Update or View.
func LoadDiff(dir string) Diff {
	if dir == "" {
		return Diff{Err: "no directory"}
	}
	if _, err := os.Stat(dir); err != nil {
		return Diff{Err: "directory gone"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key, err := diffKey(ctx, dir)
	if err != nil {
		return Diff{Err: "no git repo"}
	}
	diffMu.Lock()
	d, ok := diffCache[key]
	diffMu.Unlock()
	if ok {
		return d
	}

	base := "HEAD"
	if b := DefaultBranch(ctx, dir); b != "" {
		d.Base = b
		if mb, err := gitOut(ctx, dir, "merge-base", b, "HEAD"); err == nil {
			base = strings.TrimSpace(mb)
		}
	}
	// against the merge-base, working tree included: uncommitted changes too
	d.Stat, _ = gitOut(ctx, dir, "diff", "--stat=100", base)
	body, err := gitOut(ctx, dir, "diff", "--no-color", "--no-ext-diff", base)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Diff{Err: "git diff timed out", Base: d.Base}
	}
	d.Stat = strings.TrimRight(d.Stat, "\n")
	if st, b := untracked(ctx, dir); st != "" {
		d.Stat = strings.TrimLeft(d.Stat+"\n"+st, "\n")
		body = strings.TrimRight(body, "\n") + "\n" + b
	}
	d.Body, d.Cut = capLines(body, MaxDiffLines)
	d.Empty = d.Stat == "" && strings.TrimSpace(d.Body) == ""
	diffMu.Lock()
	if len(diffCache) > 64 { // states pile up as agents work; keep it small
		diffCache = map[string]Diff{}
	}
	diffCache[key] = d
	diffMu.Unlock()
	return d
}

// maxUntracked caps the new, not yet added files shown: each costs two git runs.
const maxUntracked = 50

// untracked diffs the files git doesn't track yet (new files an agent hasn't
// added), which plain git diff leaves out, without touching the index: stat
// lines like --stat's, and the diffs.
func untracked(ctx context.Context, dir string) (stat, body string) {
	out, err := gitOut(ctx, dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil || out == "" {
		return "", ""
	}
	files := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	var st, b strings.Builder
	for i, f := range files {
		if i == maxUntracked {
			fmt.Fprintf(&st, " … %d more untracked files not shown\n", len(files)-i)
			break
		}
		// --no-index exits 1 when the files differ, which they always do here
		ns, _ := gitOut(ctx, dir, "diff", "--no-index", "--numstat", "--", "/dev/null", f)
		add := "?"
		if fs := strings.Fields(ns); len(fs) > 0 {
			add = fs[0]
		}
		if add == "-" {
			fmt.Fprintf(&st, " %s (new, untracked) | Bin\n", f)
		} else {
			n, _ := strconv.Atoi(add)
			fmt.Fprintf(&st, " %s (new, untracked) | %s %s\n", f, add, strings.Repeat("+", min(n, 30)))
		}
		df, _ := gitOut(ctx, dir, "diff", "--no-index", "--no-color", "--no-ext-diff", "--", "/dev/null", f)
		b.WriteString(df)
	}
	return strings.TrimRight(st.String(), "\n"), b.String()
}

func capLines(s string, max int) (string, int) {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return "", 0
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s, 0
	}
	return strings.Join(lines[:max], "\n"), len(lines) - max
}
