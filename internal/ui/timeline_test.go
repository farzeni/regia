package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

func TestTimelineLinesGroupsByDay(t *testing.T) {
	now := time.Now()
	y := now.AddDate(0, 0, -1)
	items := []tlItem{
		{At: y, Who: "alpha", Icon: "▶", Color: cBusy, Text: "started: x"},
		{At: now, Who: "beta", Icon: "●", Color: cBlocked, Text: "waiting: ok?\nmore"},
	}
	var plain []string
	for _, l := range timelineLines(items, 60) {
		plain = append(plain, ansi.Strip(l))
	}
	got := strings.Join(plain, "\n")
	for _, want := range []string{"── Yesterday", "── Today", "alpha ▶ started: x", "beta  ● waiting: ok? more"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Index(got, "Yesterday") > strings.Index(got, "Today") {
		t.Error("days not oldest first")
	}
	if l := timelineLines(nil, 60); len(l) != 1 {
		t.Errorf("empty timeline = %q", l)
	}
}

func TestCommitterIsLastStartedBefore(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	a := &claude.Agent{ID: "a", StartedAt: t0}
	b := &claude.Agent{ID: "b", StartedAt: t0.Add(time.Hour)}
	ag := []*claude.Agent{a, b}
	if committer(ag, t0.Add(30*time.Minute)) != a || committer(ag, t0.Add(2*time.Hour)) != b || committer(ag, t0.Add(-time.Hour)) != a {
		t.Error("wrong committer")
	}
}

func TestLogKeysShareBranch(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 10, 0, 30, 0, time.UTC)
	as := []*claude.Agent{
		{ID: "a", Cwd: "/r", StartedAt: t0},
		{ID: "b", Cwd: "/r", StartedAt: t0.Add(time.Hour)},
		{ID: "c", Cwd: "/r/.claude/worktrees/x", Branch: "x", StartedAt: t0},
		{ID: "s:1", Cwd: "/r", Interactive: true},
	}
	ks := logKeys(as)
	if len(ks) != 2 {
		t.Fatalf("keys = %v", ks)
	}
	for k, ag := range ks {
		if k.Dir == "/r" && (k.Ref != "HEAD" || len(ag) != 2 || !k.Since.Equal(t0.Truncate(time.Minute))) {
			t.Errorf("main key = %+v (%d agents)", k, len(ag))
		}
	}
}
