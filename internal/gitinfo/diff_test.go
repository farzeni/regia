package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func TestLoadDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "init")
	if d := LoadDiff(dir); !d.Empty || d.Err != "" {
		t.Fatalf("want empty, got %+v", d)
	}

	gitRun(t, dir, "checkout", "-qb", "feat")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644)
	gitRun(t, dir, "commit", "-qam", "change")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("three\n"), 0o644) // uncommitted
	os.WriteFile(filepath.Join(dir, "b.bin"), []byte{0, 1, 2, 0}, 0o644)
	gitRun(t, dir, "add", "b.bin")
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand\nnew\n"), 0o644) // untracked
	d := LoadDiff(dir)
	if d.Err != "" || d.Base != "main" || d.Empty {
		t.Fatalf("bad diff %+v", d)
	}
	for _, want := range []string{"-one", "+three", "Binary files", "+brand", "b/new.txt"} {
		if !strings.Contains(d.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, d.Body)
		}
	}
	if !strings.Contains(d.Stat, "a.txt") || !strings.Contains(d.Stat, "b.bin") || !strings.Contains(d.Stat, "new.txt (new, untracked) | 2 ++") {
		t.Errorf("stat: %s", d.Stat)
	}

	if d := LoadDiff(filepath.Join(dir, "gone")); d.Err != "directory gone" {
		t.Errorf("gone: %+v", d)
	}
	if d := LoadDiff(t.TempDir()); d.Err != "no git repo" {
		t.Errorf("no repo: %+v", d)
	}
}

func TestCapLines(t *testing.T) {
	if s, cut := capLines("a\nb\nc\n", 2); s != "a\nb" || cut != 1 {
		t.Fatalf("%q %d", s, cut)
	}
	if s, cut := capLines("a\n", 2); s != "a" || cut != 0 {
		t.Fatalf("%q %d", s, cut)
	}
}
