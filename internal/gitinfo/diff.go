package gitinfo

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
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

// MergeBase is where an agent's changes start: the commit dir's HEAD shares
// with the repo's default branch (branch), so a diff against it shows the
// agent's commits and its uncommitted changes. With no main/master it is
// HEAD's commit and branch is "". errText says why there is none ("no git
// repo", …). It runs git: call it from a tea.Cmd.
func MergeBase(dir string) (commit, branch, errText string) {
	if dir == "" {
		return "", "", "no directory"
	}
	if _, err := os.Stat(dir); err != nil {
		return "", "", "directory gone"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	head, err := gitOut(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", "no git repo"
	}
	if b := DefaultBranch(ctx, dir); b != "" {
		if mb, err := gitOut(ctx, dir, "merge-base", b, "HEAD"); err == nil {
			return strings.TrimSpace(mb), b, ""
		}
	}
	return strings.TrimSpace(head), "", ""
}
