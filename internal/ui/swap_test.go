package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/layout"
)

// TestSwapOnTheCard: the session that took over shows where it came from with
// the note, and a claude rm that refused; the replaced one, who took its place.
func TestSwapOnTheCard(t *testing.T) {
	m := testModel(t)
	m.Update(tkey("esc"))
	at := time.Date(2026, 11, 24, 9, 10, 0, 0, time.Local)
	m.layout.Swaps = map[string][]layout.Swap{"bbbb2222": {
		{From: "old1", Title: "epic E1", At: at.Add(-24 * time.Hour), Reason: "rotation"},
		{From: "aaaa1111", Title: "alpha", At: at, Reason: "context stale", Note: "epics/E1/journal.md", RmErr: "2 unpushed commits"},
	}}
	m.saveLayoutQuiet()

	m.cursorID = "bbbb2222"
	m.rebuild()
	v := ansi.Strip(m.View())
	for _, want := range []string{"swapped", "from alpha", "context stale", "epics/E1/journal.md", "1 before", "claude rm refused alpha: 2 unpushed commits"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the card lacks %q:\n%s", want, v)
		}
	}

	m.cursorID = "aaaa1111"
	m.rebuild()
	if v := ansi.Strip(m.View()); !strings.Contains(v, "replaced") || !strings.Contains(v, "by beta (bbbb2222)") {
		t.Fatalf("the replaced session says who took over:\n%s", v)
	}
}

// TestRetireSwappedFallback: a session that swapped itself out is removed by
// regia once it is idle, in case its Stop hook didn't; never while it works.
func TestRetireSwappedFallback(t *testing.T) {
	m := testModel(t)
	var got []string
	orig := retireSession
	t.Cleanup(func() { retireSession = orig })
	retireSession = func(id string) error { got = append(got, id); return nil }

	m.layout.Retire = map[string]string{"aaaa1111": "stale", "bbbb2222": "stale"}
	cmd := m.retireSwapped()
	if cmd == nil {
		t.Fatal("bbbb2222 is idle: removed")
	}
	m.Update(runCmd(cmd))
	if len(got) != 1 || got[0] != "bbbb2222" {
		t.Fatalf("only the idle one, aaaa1111 is working: %v", got)
	}
	if m.retiring["bbbb2222"] {
		t.Fatal("done: no longer running")
	}
}
