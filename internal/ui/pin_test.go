package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/layout"
)

// A pinned session stays in the tree after it's done and seen; unpinned, it
// goes like any finished one.
func TestPinnedStaysWhenDone(t *testing.T) {
	f := newReady(t)
	m := f.m
	goTo(t, m, "busy0001")
	m.Update(tkey("f"))
	if !m.layout.Pins["busy0001"] || !layout.Load().Pins["busy0001"] {
		t.Fatal("f doesn't pin, or the pin isn't saved")
	}
	if !strings.Contains(ansi.Strip(m.rowLine(*m.selected(), 60)), pinGlyph) {
		t.Fatal("pinned row shows no pin")
	}
	// it finishes, you look at it, you move away: still there
	t0 := time.Now()
	f.job("busy0001", "done", t0)
	st := maps(baseStates, "busy0001", "done")
	f.list(st)
	m.markSeen("busy0001", t0.Add(time.Second))
	goTo(t, m, "ask00001")
	m.rebuild() // the row you were reading is no longer kept
	if s := m.agents["busy0001"].State; s != "done" {
		t.Fatalf("state %q, want done", s)
	}
	if !slices.Contains(treeIDs(m), "busy0001") {
		t.Fatalf("pinned done agent hidden: %v", treeIDs(m))
	}
	// the filter still applies to it
	m.opts.Filter = "ask00001"
	m.rebuild()
	if slices.Contains(treeIDs(m), "busy0001") {
		t.Fatal("pinned agent ignores the filter")
	}
	m.opts.Filter = ""
	m.rebuild()
	// f again unpins: hidden once you move away
	goTo(t, m, "busy0001")
	m.Update(tkey("f"))
	if m.layout.Pins["busy0001"] {
		t.Fatal("f again doesn't unpin")
	}
	goTo(t, m, "ask00001")
	m.rebuild()
	if slices.Contains(treeIDs(m), "busy0001") {
		t.Fatal("unpinned done agent still shown")
	}
}

func TestPinGroupAndArchive(t *testing.T) {
	f := newReady(t)
	m := f.m
	var gid string
	for _, r := range m.rows {
		if r.Group {
			gid = r.ID
		}
	}
	goTo(t, m, gid)
	m.Update(tkey("f"))
	for _, id := range []string{"ask00001", "oldask01", "busy0001"} {
		if !m.layout.Pins[id] {
			t.Fatalf("f on the group didn't pin %s", id)
		}
	}
	m.Update(tkey("f")) // all pinned: unpins them all
	if len(m.layout.Pins) != 0 {
		t.Fatalf("f again on the group left %v", m.layout.Pins)
	}
	// archiving drops the pin; pinning brings an archived agent back
	m.layout.SetPins(true, "ask00001")
	m.layout.Archive("ask00001")
	if m.layout.Pins["ask00001"] {
		t.Fatal("archived agent kept its pin")
	}
	m.layout.SetPins(true, "ask00001")
	if m.layout.IsArchived("ask00001") {
		t.Fatal("pinning didn't unarchive")
	}
}
