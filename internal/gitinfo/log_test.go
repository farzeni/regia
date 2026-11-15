package gitinfo

import (
	"os/exec"
	"testing"
	"time"
)

func TestParseLog(t *testing.T) {
	cs := parseLog("abc123\x00Fix: a thing\x001700000000\nbroken line\ndef456\x00Two\x00nope\n")
	if len(cs) != 1 || cs[0].Hash != "abc123" || cs[0].Subject != "Fix: a thing" || cs[0].At.Unix() != 1700000000 {
		t.Fatalf("parseLog = %+v", cs)
	}
}

func TestReadLogRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "first")
	run("commit", "-q", "--allow-empty", "-m", "second")
	k := LogKey{Dir: dir, Ref: "main", Since: time.Now().Add(-time.Hour)}
	if f := LogRefresher(k); f == nil {
		t.Fatal("no refresher on an empty cache")
	} else {
		f()
	}
	cs, ok := CachedLog(k)
	if !ok || len(cs) != 2 || cs[0].Subject != "second" {
		t.Fatalf("log = %+v %v", cs, ok)
	}
	if LogRefresher(k) != nil {
		t.Error("fresh cache refreshed again")
	}
	if got := ReadLog(LogKey{Dir: t.TempDir()}); got != nil {
		t.Errorf("not a repo gave %+v", got)
	}
}
