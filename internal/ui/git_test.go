package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/farzeni/regia/internal/gitinfo"
)

func TestGitLines(t *testing.T) {
	cases := []struct {
		g      gitinfo.Info
		l1, l2 string
	}{
		{gitinfo.Info{}, "", ""},
		{gitinfo.Info{OK: true, Branch: "main", Upstream: "origin/main", Ahead: 1, Changed: 1, Untracked: 1,
			Head: "abc1234", Subject: "second: ahead by one", CommitAt: time.Now()},
			"⎇ main · ↑1 · 1 changed, 1 untracked", "abc1234 second: ahead by one"},
		{gitinfo.Info{OK: true, Branch: "feature", Worktree: true}, "⎇ feature · worktree · no upstream · clean", ""},
		{gitinfo.Info{OK: true, Head: "abc1234", Upstream: "origin/x", Behind: 2}, "⎇ (detached abc1234) · ↓2 · clean", "abc1234"},
	}
	for _, c := range cases {
		l1, l2 := gitLines(c.g)
		if l1 != c.l1 || !strings.HasPrefix(l2, c.l2) {
			t.Errorf("gitLines(%+v)\n got %q / %q\nwant %q / %q…", c.g, l1, l2, c.l1, c.l2)
		}
	}
}
