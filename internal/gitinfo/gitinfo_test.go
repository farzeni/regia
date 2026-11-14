package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestReadGit(t *testing.T) {
	base := t.TempDir()
	remote, clone := filepath.Join(base, "remote.git"), filepath.Join(base, "clone")
	run(t, base, "init", "-q", "--bare", "-b", "main", remote)
	run(t, base, "clone", "-q", remote, clone)
	_ = os.WriteFile(filepath.Join(clone, "a.txt"), []byte("1"), 0o644)
	run(t, clone, "add", "a.txt")
	run(t, clone, "commit", "-qm", "first")
	run(t, clone, "push", "-q", "-u", "origin", "main")
	_ = os.WriteFile(filepath.Join(clone, "a.txt"), []byte("2"), 0o644)
	run(t, clone, "commit", "-qam", "second: ahead by one")
	_ = os.WriteFile(filepath.Join(clone, "a.txt"), []byte("3"), 0o644)   // changed
	_ = os.WriteFile(filepath.Join(clone, "new.txt"), []byte("x"), 0o644) // untracked

	g := Read(clone)
	if !g.OK || g.Branch != "main" || g.Upstream != "origin/main" || g.Ahead != 1 || g.Behind != 0 ||
		g.Changed != 1 || g.Untracked != 1 || g.Subject != "second: ahead by one" || g.Worktree {
		t.Fatalf("clone: %+v", g)
	}

	wt := filepath.Join(base, "wt")
	run(t, clone, "worktree", "add", "-q", "-b", "feature", wt)
	g = Read(wt)
	if !g.OK || g.Branch != "feature" || !g.Worktree || g.Upstream != "" {
		t.Fatalf("worktree: %+v", g)
	}

	if g := Read(t.TempDir()); g.OK {
		t.Fatalf("plain directory reported as repo: %+v", g)
	}
}
