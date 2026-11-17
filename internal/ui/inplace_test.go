package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// lineWith is the index of the first screen line holding s, or -1.
func lineWith(m *model, s string) int {
	for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

// r edits the title right in the sidebar row, at its place; the line above
// the input stays free.
func TestRenameInPlace(t *testing.T) {
	m := twoGroups(t)
	goTo(t, m, "aaaa2222")
	at := lineWith(m, "a2")
	send(m, tkey("r"))
	if m.editingRow("aaaa2222") == nil {
		t.Fatalf("r should edit the row in place: mode %v", m.mode)
	}
	send(m, tea.KeyMsg{Type: tea.KeyBackspace, Alt: true}) // alt+backspace: the whole word
	typeText(m, "alpha two")
	if got := lineWith(m, "alpha two"); got != at {
		t.Fatalf("edited title on line %d, the row is on %d:\n%s", got, at, ansi.Strip(m.View()))
	}
	if strings.Contains(ansi.Strip(m.footer()), "rename") {
		t.Fatalf("the footer should not hold the edit:\n%s", ansi.Strip(m.footer()))
	}
	send(m, tkey("enter"))
	if m.mode != modeNormal || m.layout.Alias["aaaa2222"] != "alpha two" {
		t.Fatalf("mode %v alias %q", m.mode, m.layout.Alias["aaaa2222"])
	}

	// esc leaves the name as it was
	send(m, tkey("r"))
	typeText(m, "zzz")
	send(m, tkey("esc"))
	if m.mode != modeNormal || m.layout.Alias["aaaa2222"] != "alpha two" {
		t.Fatalf("esc: mode %v alias %q", m.mode, m.layout.Alias["aaaa2222"])
	}
}

// The edit takes the usual readline keys.
func TestInPlaceWordKeys(t *testing.T) {
	m := twoGroups(t)
	goTo(t, m, "aaaa1111")
	send(m, tkey("r"))
	e := m.edit
	k := func(t tea.KeyType, alt bool) { send(m, tea.KeyMsg{Type: t, Alt: alt}) }
	want := func(step, s string) {
		t.Helper()
		if e.ti.Value() != s {
			t.Fatalf("%s: %q, want %q", step, e.ti.Value(), s)
		}
	}
	k(tea.KeyCtrlU, false)
	want("ctrl+u", "")
	typeText(m, "fix the flaky test now")
	k(tea.KeyBackspace, true)
	want("alt+backspace", "fix the flaky test ")
	k(tea.KeyCtrlW, false)
	want("ctrl+w", "fix the flaky ")
	k(tea.KeyBackspace, false)
	want("backspace", "fix the flaky")
	k(tea.KeyLeft, true) // alt+left: back a word
	k(tea.KeyCtrlK, false)
	want("alt+left, ctrl+k", "fix the ")
	k(tea.KeyCtrlA, false)
	typeText(m, "now ")
	want("ctrl+a + type", "now fix the ")
	k(tea.KeyDelete, false)
	want("delete", "now ix the ")
	k(tea.KeyEnd, false)
	typeText(m, "end")
	want("end + type", "now ix the end")
	k(tea.KeyHome, false)
	k(tea.KeyRight, false)
	k(tea.KeyCtrlE, false)
	k(tea.KeyLeft, false)
	k(tea.KeyBackspace, false)
	want("ctrl+e, left, backspace", "now ix the ed")
	send(m, tkey("enter"))
	if m.layout.Alias["aaaa1111"] != "now ix the ed" {
		t.Fatalf("alias %q", m.layout.Alias["aaaa1111"])
	}
}

// n puts the new group in the tree where it will stay and has you name it
// there; esc takes it away again.
func TestNewGroupInPlace(t *testing.T) {
	m := twoGroups(t)
	before := tree(m)
	goTo(t, m, "aaaa1111")
	send(m, tkey("n"))
	gid := m.cursorID
	if !m.layout.IsGroup(gid) || m.editingRow(gid) == nil {
		t.Fatalf("n should add a group row and edit it: cursor %q mode %v", gid, m.mode)
	}
	ids := rowIDs(m)
	if i := slices.Index(ids, gid); i < 1 || ids[i-1] != "aaaa1111" {
		t.Fatalf("the new group should sit right after a1: %v", ids)
	}
	typeText(m, "work")
	if got, sel := lineWith(m, "work"), lineWith(m, "a1")+1; got != sel {
		t.Fatalf("name typed on line %d, the group row is on %d:\n%s", got, sel, ansi.Strip(m.View()))
	}
	send(m, tkey("esc"))
	if m.layout.IsGroup(gid) || tree(m) != before {
		t.Fatalf("esc should take the group away:\n%s\nwas\n%s", tree(m), before)
	}

	send(m, tkey("n"))
	typeText(m, "work")
	send(m, tkey("enter"))
	gid = m.cursorID
	if m.layout.Groups[gid] != "work" || m.mode != modeNormal {
		t.Fatalf("group %q %q mode %v", gid, m.layout.Groups[gid], m.mode)
	}
}

// n in select mode: the group of the marked agents is named in place, and
// esc puts them back where they were.
func TestGroupMarkedInPlace(t *testing.T) {
	m := twoGroups(t)
	before := tree(m)
	send(m, tkey("v"))
	goTo(t, m, "aaaa2222")
	send(m, tkey(" "))
	goTo(t, m, "bbbb1111")
	send(m, tkey(" "))
	send(m, tkey("n"))
	gid := m.cursorID
	if m.editingRow(gid) == nil || m.layout.Parent["bbbb1111"] != gid {
		t.Fatalf("marked agents should be in the group being named:\n%s", tree(m))
	}
	send(m, tkey("esc"))
	if m.layout.IsGroup(gid) || tree(m) != before {
		t.Fatalf("esc should put the tree back:\n%s\nwas\n%s", tree(m), before)
	}
	send(m, tkey("n"))
	typeText(m, "pair")
	send(m, tkey("enter"))
	gid = m.cursorID
	if m.layout.Groups[gid] != "pair" || m.layout.Parent["aaaa2222"] != gid || m.selecting {
		t.Fatalf("grouped: %q selecting %v\n%s", m.layout.Groups[gid], m.selecting, tree(m))
	}
}

// A title longer than the row scrolls sideways inside it.
func TestInPlaceLongTitleScrolls(t *testing.T) {
	m := twoGroups(t)
	goTo(t, m, "aaaa1111")
	send(m, tkey("r"))
	send(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	long := "START " + strings.Repeat("x", 120) + " END"
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(long)})
	r := m.selected()
	lw := m.sidebarWidth(m.w - 2*padX)
	line := m.rowLine(*r, lw)
	if w := ansi.StringWidth(line); w > lw {
		t.Fatalf("row %d wide, sidebar %d: %q", w, lw, ansi.Strip(line))
	}
	if s := ansi.Strip(line); !strings.Contains(s, "END") || strings.Contains(s, "START") {
		t.Fatalf("the end should show while typing there: %q", s)
	}
	send(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	if s := ansi.Strip(m.rowLine(*r, lw)); !strings.Contains(s, "START") {
		t.Fatalf("ctrl+a should scroll back to the start: %q", s)
	}
}
