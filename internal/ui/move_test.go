package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeIn(m *model, s string) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	m.Update(tkey("enter"))
}

// tree as rows: "  " per depth level, then the title
func tree(m *model) string {
	var b []string
	for _, r := range m.rows {
		b = append(b, strings.Repeat("  ", r.Depth)+r.Title)
	}
	return strings.Join(b, "\n")
}

// The bug: a new group, an agent moved with m then p on the group landed
// after the group instead of inside it.
func TestPutOnGroupGoesInside(t *testing.T) {
	m := twoGroups(t)
	goTo(t, m, "bbbb1111")
	m.Update(tkey("n"))
	typeIn(m, "work")
	gid := m.cursorID
	if !m.layout.IsGroup(gid) {
		t.Fatalf("new group not selected: %s", gid)
	}
	goTo(t, m, "aaaa1111")
	m.Update(tkey("m"))
	goTo(t, m, gid)
	m.Update(tkey("p"))
	if p := m.layout.Parent["aaaa1111"]; p != gid {
		t.Fatalf("p on a group should put inside it, parent %q\n%s", p, tree(m))
	}
	if m.cursorID != "aaaa1111" || m.cut != nil {
		t.Fatalf("cursor %s cut %v", m.cursorID, m.cut)
	}
	// a second one goes in at the top, right under the group row
	goTo(t, m, "aaaa2222")
	m.Update(tkey("m"))
	goTo(t, m, gid)
	m.Update(tkey("p"))
	ids := rowIDs(m)
	g := slices.Index(ids, gid)
	if ids[g+1] != "aaaa2222" || ids[g+2] != "aaaa1111" {
		t.Fatalf("want work > a2, a1:\n%s", tree(m))
	}
}

func TestPutOtherWay(t *testing.T) {
	m := twoGroups(t)
	// P on an agent: under it, as its coordinator
	goTo(t, m, "bbbb1111")
	m.Update(tkey("m"))
	goTo(t, m, "aaaa1111")
	m.Update(tkey("P"))
	if p := m.layout.Parent["bbbb1111"]; p != "aaaa1111" {
		t.Fatalf("P on an agent: parent %q\n%s", p, tree(m))
	}
	// p on an agent: right after it, same parent
	goTo(t, m, "bbbb1111")
	m.Update(tkey("m"))
	goTo(t, m, "aaaa2222")
	m.Update(tkey("p"))
	if p := m.layout.Parent["bbbb1111"]; p != m.layout.Parent["aaaa2222"] {
		t.Fatalf("p on an agent: parent %q\n%s", p, tree(m))
	}
	ids := rowIDs(m)
	if slices.Index(ids, "bbbb1111") != slices.Index(ids, "aaaa2222")+1 {
		t.Fatalf("not right after a2:\n%s", tree(m))
	}
	// P on a group: after the group, at its level
	goTo(t, m, "aaaa1111")
	m.Update(tkey("m"))
	g := m.layout.Parent["aaaa2222"]
	goTo(t, m, g)
	m.Update(tkey("P"))
	if p := m.layout.Parent["aaaa1111"]; p != m.layout.Parent[g] {
		t.Fatalf("P on a group: parent %q\n%s", p, tree(m))
	}
	// into itself is refused
	goTo(t, m, g)
	m.Update(tkey("m"))
	goTo(t, m, "aaaa2222")
	m.Update(tkey("P"))
	if !strings.Contains(m.status, "inside itself") {
		t.Fatalf("status %q", m.status)
	}
}

func TestSelectMoveTogether(t *testing.T) {
	m := twoGroups(t)
	g2 := m.layout.Parent["bbbb1111"]
	m.Update(tkey("v"))
	goTo(t, m, "aaaa1111")
	m.Update(tkey(" "))
	goTo(t, m, "aaaa2222")
	m.Update(tkey(" "))
	m.Update(tkey("m"))
	if m.selecting || len(m.cut) != 2 {
		t.Fatalf("selecting %v cut %v", m.selecting, m.cut)
	}
	goTo(t, m, g2)
	m.Update(tkey("p"))
	for _, id := range []string{"aaaa1111", "aaaa2222"} {
		if p := m.layout.Parent[id]; p != g2 {
			t.Fatalf("%s parent %q\n%s", id, p, tree(m))
		}
	}
	ids := rowIDs(m)
	g := slices.Index(ids, g2)
	if !slices.Equal(ids[g+1:g+4], []string{"aaaa1111", "aaaa2222", "bbbb1111"}) {
		t.Fatalf("order kept, at the top:\n%s", tree(m))
	}
}

func TestSelectNewGroup(t *testing.T) {
	m := twoGroups(t)
	// a coordinator with its agent: marking both moves the coordinator, the
	// agent comes along under it
	if err := m.layout.Put([]string{"aaaa2222"}, "aaaa1111", "", m.agents); err != nil {
		t.Fatal(err)
	}
	m.rebuild()
	m.Update(tkey("v"))
	goTo(t, m, "aaaa1111")
	m.Update(tkey(" ")) // marks a1 and a2 under it
	goTo(t, m, "bbbb1111")
	m.Update(tkey(" "))
	m.Update(tkey("n"))
	typeIn(m, "release")
	gid := m.cursorID
	if m.layout.Groups[gid] != "release" || m.selecting {
		t.Fatalf("group %q selecting %v\n%s", gid, m.selecting, tree(m))
	}
	if m.layout.Parent["aaaa1111"] != gid || m.layout.Parent["bbbb1111"] != gid || m.layout.Parent["aaaa2222"] != "aaaa1111" {
		t.Fatalf("wrong parents:\n%s", tree(m))
	}
	want := "one\n  release\n    a1\n      a2\n    b1" // where a1 was
	if got := tree(m); !strings.Contains(got, want) {
		t.Fatalf("want\n%s\ngot\n%s", want, got)
	}
}
