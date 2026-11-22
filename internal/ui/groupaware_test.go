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
