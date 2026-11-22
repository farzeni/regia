package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

// O steps an agent's grant: its group, the whole tree, nothing.
func TestGrantKey(t *testing.T) {
	m := teamModel(t)
	m.cursorID = "bbbb2222"
	m.rebuild()
	for _, want := range []string{"g:t", group.Everything, ""} {
		send(m, tkey("O"))
		if got := layout.Load().Grants["bbbb2222"]; got != want {
			t.Fatalf("grant %q, want %q (%s)", got, want, m.status)
		}
		if want == "g:t" && !strings.Contains(ansi.Strip(m.View()), grantGlyph+" beta") {
			t.Fatalf("grant not shown:\n%s", ansi.Strip(m.View()))
		}
	}
	m.cursorID = "g:t"
	m.rebuild()
	send(m, tkey("O"))
	if len(layout.Load().Grants) != 0 || !strings.Contains(m.status, "select an agent") {
		t.Fatalf("a group gets no grant: %q", m.status)
	}
}

// An agent's change is announced, U lists it and its key undoes it.
func TestActionsUndo(t *testing.T) {
	m := teamModel(t)
	m.refresh() // the first look only takes note of the log
	_, c, err := layout.Update(func(l *layout.Layout) error { l.Groups["g:t"] = "renamed"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	group.Record(group.Action{At: time.Now(), By: "aaaa1111", Name: "alpha", Tool: "rename_group", Summary: `renamed group "team" to "renamed"`, Change: c})
	m.refresh()
	if !strings.Contains(m.status, "alpha: renamed group") || !strings.Contains(m.status, "U to undo") {
		t.Fatalf("not announced: %q", m.status)
	}
	send(m, tkey("U"))
	if v := ansi.Strip(m.View()); m.mode != modeMenu || !strings.Contains(v, "alpha · renamed group") {
		t.Fatalf("U lists the action:\n%s", v)
	}
	send(m, tkey("1"))
	if m.layout.Groups["g:t"] != "team" || !strings.Contains(m.status, "undone") {
		t.Fatalf("undo: %q %q", m.layout.Groups["g:t"], m.status)
	}
	send(m, tkey("U"))
	send(m, tkey("1"))
	if !strings.Contains(m.status, "already undone") {
		t.Fatalf("status %q", m.status)
	}
}
