package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestMergeBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "init")
	fork := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-qb", "feat")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644)
	gitRun(t, dir, "commit", "-qam", "change")

	if c, b, e := MergeBase(dir); c != fork || b != "main" || e != "" {
		t.Fatalf("got %q %q %q, want the fork point %s on main", c, b, e, fork)
	}
	if _, _, e := MergeBase(filepath.Join(dir, "gone")); e != "directory gone" {
		t.Errorf("gone: %q", e)
	}
	if _, _, e := MergeBase(t.TempDir()); e != "no git repo" {
		t.Errorf("no repo: %q", e)
	}
}
