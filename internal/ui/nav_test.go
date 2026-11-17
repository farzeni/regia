package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

// twoGroups: agents in two projects, so the tree has two groups.
func twoGroups(t *testing.T) *model {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	d1, d2 := t.TempDir()+"/Projects/one", t.TempDir()+"/Projects/two"
	m := newModel()
	m.w, m.h = 140, 30
	m.setAgents([]byte(`[
	 {"id":"aaaa1111","cwd":"` + d1 + `","kind":"background","name":"a1","status":"busy","state":"working","startedAt":1794645285576},
	 {"id":"aaaa2222","cwd":"` + d1 + `","kind":"background","name":"a2","status":"busy","state":"working","startedAt":1794645285577},
	 {"id":"bbbb1111","cwd":"` + d2 + `","kind":"background","name":"b1","status":"busy","state":"working","startedAt":1794645285578}]`))
	m.rebuild()
	return m
}

func rowIDs(m *model) []string {
	var ids []string
	for _, r := range m.rows {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestGroupJumps(t *testing.T) {
	m := twoGroups(t)
	var groups []int
	for i, r := range m.rows {
		if r.Group {
			groups = append(groups, i)
		}
	}
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, rows %v", rowIDs(m))
	}
	m.moveCursor(-len(m.rows))
	m.moveCursor(1) // an agent inside the first group
	m.Update(tkey("alt+down"))
	if m.cursor != groups[1] {
		t.Fatalf("alt+down: cursor %d, want group at %d", m.cursor, groups[1])
	}
	m.Update(tkey("alt+down")) // no group below: stay
	if m.cursor != groups[1] {
		t.Fatalf("alt+down past the last group moved to %d", m.cursor)
	}
	m.Update(tkey("alt+up"))
	if m.cursor != groups[0] {
		t.Fatalf("alt+up: cursor %d, want %d", m.cursor, groups[0])
	}
	m.Update(tkey("]"))
	m.Update(tkey("["))
	if m.cursor != groups[0] {
		t.Fatalf("] then [: cursor %d, want %d", m.cursor, groups[0])
	}
	m.Update(tkey("i")) // works while typing too
	m.Update(tkey("ctrl+down"))
	if m.cursor != groups[1] || !m.typing {
		t.Fatalf("ctrl+down while typing: cursor %d typing %v", m.cursor, m.typing)
	}
	m.Update(tkey("ctrl+up"))
	if m.cursor != groups[0] || m.draft() != "" {
		t.Fatalf("ctrl+up while typing: cursor %d draft %q", m.cursor, m.draft())
	}
}

func TestFoldAll(t *testing.T) {
	m := twoGroups(t)
	n := len(m.rows)
	m.moveCursor(1) // an agent: once folded, the selection moves to its group
	m.Update(tkey("z"))
	if len(m.rows) != 2 {
		t.Fatalf("z should fold to the 2 groups, rows %v", rowIDs(m))
	}
	if r := m.selected(); r == nil || !r.Group {
		t.Fatalf("selection lost after folding: %v", m.cursorID)
	}
	m.Update(tkey("z"))
	if len(m.rows) != n {
		t.Fatalf("second z should unfold all: %d rows, want %d", len(m.rows), n)
	}
}

func TestPanelBottomUpAndScroll(t *testing.T) {
	m := testModel(t)
	m.Update(tkey("esc")) // command keys
	m.w, m.h = 120, 24
	m.moveCursor(1) // alpha, an agent
	r := m.selected()
	giveTranscript(t, m, r.ID, exchanges(40)...)
	m.hist, m.histFor = claude.History{Task: "the task"}, r.ID

	screen := func() string { return ansi.Strip(m.View()) }
	s := screen()
	// newest at the bottom, oldest scrolled off above
	if !strings.Contains(s, "reply 39") || strings.Contains(s, "ask 00") || !strings.Contains(s, "earlier · pgup") {
		t.Fatalf("panel should show the newest messages:\n%s", s)
	}
	if strings.Index(s, "ask 38") > strings.Index(s, "ask 39") {
		t.Fatal("the conversation should run oldest to newest, top to bottom")
	}
	// the card is above the conversation
	if strings.Index(s, "the task") > strings.Index(s, "reply 39") {
		t.Fatal("card should be above the conversation")
	}
	for range 60 { // runs between the messages: more lines
		m.Update(tkey("pgup"))
	}
	if s = screen(); !strings.Contains(s, "ask 00") || strings.Contains(s, "reply 39") || !strings.Contains(s, "newer · pgdn") {
		t.Fatalf("pgup should scroll back to the start:\n%s", s)
	}
	if !strings.Contains(s, "the task") {
		t.Fatal("the card should stay pinned while scrolling")
	}
	m.Update(tkey("j")) // another agent: back to the newest
	m.moveCursor(-1)
	if s = screen(); !strings.Contains(s, "reply 39") {
		t.Fatal("selecting another row should reset the scroll")
	}
}
