package ui

import (
	"strings"
	"testing"

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
	if v := ansi.Strip(m.View()); !strings.Contains(v, "shared notes") || !strings.Contains(v, "none yet") {
		t.Fatalf("group panel should show the notes section:\n%s", v)
	}
	send(m, tkey("N"))
	if m.mode != modeInput || !strings.Contains(m.inputPrompt, "team") {
		t.Fatalf("N should ask for a note: mode %v %q", m.mode, m.inputPrompt)
	}
	typeText(m, "ship by friday")
	send(m, tkey("enter"))
	ns := group.ReadNotes("g:t")
	if len(ns) != 1 || ns[0].Text != "ship by friday" || ns[0].By != "you" {
		t.Fatalf("notes %+v (status %q)", ns, m.status)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "you: ship by friday") {
		t.Fatalf("note not in the panel:\n%s", v)
	}

	// a worker: the note goes to its coordinator, which shows it above its card
	m.cursorID = "bbbb2222"
	m.rebuild()
	send(m, tkey("N"))
	typeText(m, "beta check")
	send(m, tkey("enter"))
	if ns := group.ReadNotes("aaaa1111"); len(ns) != 1 {
		t.Fatalf("worker's note should go to its coordinator: %+v", ns)
	}
	m.cursorID = "aaaa1111"
	m.rebuild()
	if v := ansi.Strip(m.View()); !strings.Contains(v, "you: beta check") {
		t.Fatalf("coordinator panel lacks its agents' notes:\n%s", v)
	}

	// removing the group removes its notes
	m.cursorID = "g:t"
	m.rebuild()
	send(m, tkey("d"))
	send(m, tkey("d"))
	if group.ReadNotes("g:t") != nil {
		t.Fatal("notes of a removed group should go")
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
