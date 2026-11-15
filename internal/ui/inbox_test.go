package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/layout"
)

func TestDayStartsAt4am(t *testing.T) {
	loc := time.Local
	day := func(d, h, min int) time.Time { return time.Date(2026, 11, d, h, min, 0, 0, loc) }
	for _, c := range []struct{ now, want time.Time }{
		{day(15, 10, 0), day(15, 4, 0)},
		{day(15, 4, 0), day(15, 4, 0)},
		{day(15, 3, 59), day(14, 4, 0)}, // still last night
		{day(15, 0, 30), day(14, 4, 0)},
		{day(1, 2, 0), time.Date(2026, 10, 31, 4, 0, 0, 0, loc)},
	} {
		if got := dayStart(c.now); !got.Equal(c.want) {
			t.Errorf("dayStart(%s) = %s, want %s", c.now, got, c.want)
		}
	}
}

// A question is stale once it's from before today (4am), not 24h after.
func TestStaleMeansBeforeToday(t *testing.T) {
	f := newReady(t)
	m := f.m
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local)
	ask, old := m.agents["ask00001"], m.agents["oldask01"]
	ask.UpdatedAt = time.Date(2026, 11, 15, 4, 30, 0, 0, time.Local) // this morning
	old.UpdatedAt = time.Date(2026, 11, 14, 23, 0, 0, 0, time.Local) // last night: 11h ago
	m.classify(now)
	if ask.Stale || !old.Stale {
		t.Fatalf("this morning stale=%v, last night stale=%v", ask.Stale, old.Stale)
	}
	// at 2am, last night 11pm is still today
	m.classify(time.Date(2026, 11, 15, 2, 0, 0, 0, time.Local))
	if old.Stale {
		t.Fatal("before 4am, the evening before still counts as today")
	}
	// finished rows fade by the same rule
	done := m.agents["done0001"]
	done.UpdatedAt = old.UpdatedAt
	if !faded(done, now) {
		t.Fatal("a session finished last night should fade this morning")
	}
	done.UpdatedAt = ask.UpdatedAt
	if faded(done, now) {
		t.Fatal("a session finished this morning should stay white")
	}
}

func TestInboxFoldsEarlierDays(t *testing.T) {
	f := newReady(t)
	m := f.m
	m.Update(tkey("tab"))
	if got := rowIDs(m); !slices.Equal(got, []string{"ask00001", earlierID}) {
		t.Fatalf("inbox %v: today's question, then the folded earlier days", got)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "▸ earlier days") || !strings.Contains(out, "1 question · oldest 2d · space opens") {
		t.Fatalf("fold row:\n%s", out)
	}
	if strings.Contains(out, "oldask01") {
		t.Fatal("folded items listed in the inbox")
	}
	goTo(t, m, earlierID)
	if out := ansi.Strip(m.View()); !strings.Contains(out, "waiting since before today") || !strings.Contains(out, "oldask01") {
		t.Fatalf("the fold's panel should list what's inside:\n%s", out)
	}
	m.Update(tkey(" "))
	if got := rowIDs(m); !slices.Equal(got, []string{"ask00001", earlierID, "oldask01"}) {
		t.Fatalf("opened: %v", got)
	}
	if m.rows[2].Depth != 1 {
		t.Fatal("items in the fold are indented")
	}
	if l := layout.Load(); len(l.Collapsed) != 0 && l.Collapsed[earlierID] {
		t.Fatal("the fold is not a layout node: nothing saved")
	}
	goTo(t, m, "oldask01")
	m.Update(tkey("h")) // up to the fold
	if m.cursorID != earlierID {
		t.Fatalf("h inside the fold: cursor on %s", m.cursorID)
	}
	m.Update(tkey("h"))
	if got := rowIDs(m); !slices.Equal(got, []string{"ask00001", earlierID}) {
		t.Fatalf("folded again: %v", got)
	}
	// agent actions do nothing on the fold row
	m.Update(tkey("e"))
	m.Update(tkey("f"))
	if len(m.layout.Archived) != 0 || len(m.layout.Pins) != 0 {
		t.Fatal("e/f on the fold row acted on something")
	}
	// a filter searches inside the fold too
	m.opts.Filter = "oldask"
	m.rebuild()
	if !slices.Contains(rowIDs(m), "oldask01") {
		t.Fatalf("filter should open the fold: %v", rowIDs(m))
	}
}

func TestInboxNoFoldWhenNothingOld(t *testing.T) {
	m := manyAgents(t, 3)
	m.Update(tkey("tab"))
	if slices.Contains(rowIDs(m), earlierID) {
		t.Fatalf("no earlier days, no fold row: %v", rowIDs(m))
	}
}

func TestSnooze(t *testing.T) {
	f := newReady(t)
	m := f.m
	m.Update(tkey("tab"))
	goTo(t, m, "ask00001")
	m.Update(tkey("Z"))
	a := m.agents["ask00001"]
	if !a.Snoozed || a.YourTurn() {
		t.Fatalf("snoozed=%v yourTurn=%v", a.Snoozed, a.YourTurn())
	}
	if head := ansi.Strip(m.header()); !strings.Contains(head, "● 0 need you") {
		t.Fatalf("a snoozed question still counted: %q", head)
	}
	if !slices.Contains(rowIDs(m), "ask00001") {
		t.Fatal("the snoozed row vanished from under the cursor")
	}
	if !strings.Contains(ansi.Strip(m.View()), "WAITING ON YOU · SNOOZED") {
		t.Fatal("panel doesn't say snoozed")
	}
	goTo(t, m, earlierID)
	m.rebuild() // the next tick
	if slices.Contains(rowIDs(m), "ask00001") {
		t.Fatalf("snoozed question still in the inbox after moving away: %v", rowIDs(m))
	}
	// it survives a restart
	f.list(baseStates)
	if !m.agents["ask00001"].Snoozed || layout.Load().Snoozed["ask00001"].IsZero() {
		t.Fatal("snooze not saved")
	}
	// in the tree it's still there, marked
	m.Update(tkey("tab"))
	if !strings.Contains(ansi.Strip(m.View()), "ask00001  (snoozed)") {
		t.Fatalf("tree row should say snoozed:\n%s", ansi.Strip(m.View()))
	}
	// the agent does something new: back in the inbox, snooze gone
	f.job("ask00001", "blocked", time.Now().Add(time.Second))
	f.list(baseStates)
	if a := m.agents["ask00001"]; a.Snoozed || !a.YourTurn() {
		t.Fatal("a new question after the snooze should be your turn again")
	}
	if _, ok := layout.Load().Snoozed["ask00001"]; ok {
		t.Fatal("cleared snooze still saved")
	}
}

func TestSnoozeToggleAndReady(t *testing.T) {
	f := newReady(t)
	m := f.m
	goTo(t, m, "ask00001")
	m.Update(tkey("Z"))
	m.Update(tkey("Z"))
	if a := m.agents["ask00001"]; a.Snoozed || !a.YourTurn() {
		t.Fatal("Z twice should bring it back")
	}
	// on a result to read, Z marks it read
	f.job("busy0001", "done", time.Now())
	f.list(maps(baseStates, "busy0001", "done"))
	goTo(t, m, "busy0001")
	m.Update(tkey("Z"))
	if s := m.agents["busy0001"].State; s != "done" {
		t.Fatalf("Z on a ready agent: %q, want done", s)
	}
}
