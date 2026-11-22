package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

// claim sets a claim as regia mcp's claim would.
func claim(t *testing.T, id, scope, why string) {
	t.Helper()
	if _, _, err := layout.Update(func(l *layout.Layout) error { l.Grants[id], l.Claims[id] = scope, why; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestClaimShownAndTakenBack(t *testing.T) {
	m := teamModel(t)
	claim(t, "aaaa1111", group.Everything, "morning triage")
	m.cursorID = "aaaa1111"
	m.syncLayout()
	m.rebuild()
	v := ansi.Strip(m.View())
	if !strings.Contains(v, grantGlyph+" alpha") || !strings.Contains(v, "the whole tree: morning triage") {
		t.Fatalf("the claim is marked and explained:\n%s", v)
	}
	send(m, tkey("O"))
	if l := layout.Load(); l.Grants["aaaa1111"] != "" || l.Claims["aaaa1111"] != "" {
		t.Fatalf("O takes a claim back: %v %v (%s)", l.Grants, l.Claims, m.status)
	}
	send(m, tkey("O")) // and grants as before
	if l := layout.Load(); l.Grants["aaaa1111"] != "g:t" || l.Claims["aaaa1111"] != "" {
		t.Fatalf("O grants: %v %v", l.Grants, l.Claims)
	}
}

func TestClaimEndsWhenDone(t *testing.T) {
	m := teamModel(t)
	claim(t, "aaaa1111", group.Everything, "busy with it") // alpha works
	claim(t, "bbbb2222", "g:t", "finished")                // beta is done
	m.refresh()
	l := layout.Load()
	if l.Claims["bbbb2222"] != "" || l.Grants["bbbb2222"] != "" {
		t.Fatalf("a done agent's claim ends: %v", l.Claims)
	}
	if l.Claims["aaaa1111"] == "" {
		t.Fatal("a working agent keeps its claim")
	}
	if !strings.Contains(m.status, "beta is done: its claim on the tree ended") {
		t.Fatalf("status %q", m.status)
	}
	// a grant you gave stays when the agent is done
	layout.Update(func(l *layout.Layout) error { l.Grants["bbbb2222"] = "g:t"; return nil })
	m.syncLayout()
	m.refresh()
	if layout.Load().Grants["bbbb2222"] != "g:t" {
		t.Fatal("your grant stays")
	}
}
