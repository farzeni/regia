package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/group"
)

// team (g:t) holds alpha, which coordinates beta.
func teamModel(t *testing.T) *model {
	m := testModel(t)
	l := m.layout
	l.Groups["g:t"] = "team"
	l.Parent["g:t"], l.Parent["aaaa1111"], l.Parent["bbbb2222"] = "", "g:t", "aaaa1111"
	l.Order[""] = append([]string{"g:t"}, l.Order[""]...)
	l.Order["g:t"], l.Order["aaaa1111"] = []string{"aaaa1111"}, []string{"bbbb2222"}
	l.Pinned["aaaa1111"], l.Pinned["bbbb2222"] = true, true
	l.Collapsed = map[string]bool{}
	m.saveLayout()
	m.rebuild()
	send(m, tkey("esc")) // command keys
	return m
}

func TestNoteFromRegia(t *testing.T) {
	m := teamModel(t)
	m.cursorID = "g:t"
	m.rebuild()
	if v := ansi.Strip(m.View()); !strings.Contains(v, "blackboard") || !strings.Contains(v, "empty") {
		t.Fatalf("group panel should show the notes section:\n%s", v)
	}
	send(m, tkey("N"))
	if m.editingNotes("g:t") == nil {
		t.Fatalf("N should ask for a note in place: mode %v", m.mode)
	}
	typeText(m, "ship by friday")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "you: ship by friday") {
		t.Fatalf("the new note should be written on the blackboard:\n%s", v)
	}
	send(m, tkey("enter"))
	ns := group.ReadNotes("g:t")
	if len(ns) != 1 || ns[0].Text != "ship by friday" || ns[0].By != "you" {
		t.Fatalf("notes %+v (status %q)", ns, m.status)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "you: ship by friday") {
		t.Fatalf("note not in the panel:\n%s", v)
	}

	// a worker: the note goes to its coordinator's blackboard (N on it)
	m.cursorID = "bbbb2222"
	m.rebuild()
	send(m, tkey("N"))
	typeText(m, "beta check")
	send(m, tkey("enter"))
	if ns := group.ReadNotes("aaaa1111"); len(ns) != 1 {
		t.Fatalf("worker's note should go to its coordinator: %+v", ns)
	}
	if m.cursorID != "aaaa1111" {
		t.Fatalf("N on a worker should select its coordinator, not %s", m.cursorID)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "you: beta check") {
		t.Fatalf("the open blackboard should show on the coordinator's panel:\n%s", v)
	}
	send(m, tkey("esc")) // closed: the coordinator's panel is an agent's
	if v := ansi.Strip(m.View()); strings.Contains(v, "you: beta check") {
		t.Fatalf("coordinator panel should not show the blackboard:\n%s", v)
	}
	send(m, tkey("N"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "you: beta check") {
		t.Fatalf("N on the coordinator lacks its agents' notes:\n%s", v)
	}
	send(m, tkey("esc"))

	// removing the group removes its notes
	m.cursorID = "g:t"
	m.rebuild()
	send(m, tkey("d"))
	send(m, tkey("d"))
	if group.ReadNotes("g:t") != nil {
		t.Fatal("notes of a removed group should go")
	}
}

func TestBlackboardEditDelete(t *testing.T) {
	m := teamModel(t)
	group.AddNote("g:t", group.Note{By: "aaaa1111", Name: "alpha", Text: "first"})
	group.AddNote("g:t", group.Note{By: "aaaa1111", Name: "alpha", Text: "second"})
	m.cursorID = "bbbb2222" // N on a worker opens its coordinator's blackboard
	m.rebuild()
	send(m, tkey("N"))
	if m.notesFor != "aaaa1111" || m.cursorID != "aaaa1111" {
		t.Fatalf("N on a worker: for %q cursor %q", m.notesFor, m.cursorID)
	}
	send(m, tkey("esc")) // closes the prompt for a first note
	send(m, tkey("esc")) // closes the blackboard
	m.cursorID = "g:t"
	m.rebuild()
	send(m, tkey("N"))
	if m.notesFor != "g:t" || m.noteSel != 1 || m.mode != modeNormal {
		t.Fatalf("N: for %q cursor %q sel %d mode %v", m.notesFor, m.cursorID, m.noteSel, m.mode)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "› ") || !strings.Contains(v, "e edit") {
		t.Fatalf("open blackboard not shown:\n%s", v)
	}

	send(m, tkey("k")) // pick "first", edit it
	send(m, tkey("e"))
	if e := m.editingNotes("g:t"); e == nil || e.ti.Value() != "first" || e.note != 0 {
		t.Fatalf("edit in place: %v %+v", m.mode, m.edit)
	}
	send(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	typeText(m, "first, fixed")
	send(m, tkey("enter"))
	ns := group.ReadNotes("g:t")
	if ns[0].Text != "first, fixed" || ns[0].Edited.IsZero() || ns[1].Text != "second" {
		t.Fatalf("after edit %+v (%q)", ns, m.status)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "first, fixed (edited)") {
		t.Fatalf("edit not shown:\n%s", v)
	}

	send(m, tkey("j")) // pick "second": x asks, x again deletes
	send(m, tkey("x"))
	if m.mode != modeConfirm || len(group.ReadNotes("g:t")) != 2 {
		t.Fatal("x should ask first")
	}
	send(m, tkey("x"))
	if ns := group.ReadNotes("g:t"); len(ns) != 1 || ns[0].Text != "first, fixed" {
		t.Fatalf("after delete %+v (%q)", ns, m.status)
	}
	if m.notesFor != "g:t" {
		t.Fatal("the blackboard should stay open after a delete")
	}

	send(m, tkey("n")) // add from the open blackboard
	typeText(m, "third")
	send(m, tkey("enter"))
	if ns := group.ReadNotes("g:t"); len(ns) != 2 || ns[1].Text != "third" || m.noteSel != 1 {
		t.Fatalf("add %+v sel %d", ns, m.noteSel)
	}

	send(m, tkey("esc"))
	if m.notesFor != "" {
		t.Fatal("esc should close the blackboard")
	}
	send(m, tkey("N")) // another key closes it and works as usual
	send(m, tkey("tab"))
	if m.notesFor != "" || m.view != viewInbox {
		t.Fatalf("tab: notes %q view %v", m.notesFor, m.view)
	}
}

func TestMuteKey(t *testing.T) {
	m := teamModel(t)
	m.cursorID = "aaaa1111"
	m.rebuild()
	send(m, tkey("M"))
	if !m.layout.Muted["aaaa1111"] {
		t.Fatalf("M should mute the coordinator: %q", m.status)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "(muted)") {
		t.Fatalf("muted not shown:\n%s", v)
	}
	if (&group.Tree{L: m.layout, Spawns: m.layout.Spawns}).Resolve("bbbb2222").Grouped() {
		t.Fatal("a worker under a muted coordinator should get nothing")
	}
	send(m, tkey("M"))
	if m.layout.Muted["aaaa1111"] {
		t.Fatal("M again should unmute")
	}
	m.cursorID = "bbbb2222" // a plain agent can't be muted
	m.rebuild()
	send(m, tkey("M"))
	if m.layout.Muted["bbbb2222"] || !strings.Contains(m.status, "M mutes") {
		t.Fatalf("status %q", m.status)
	}
}
