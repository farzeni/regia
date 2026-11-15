package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/layout"
)

// A busy agent quiet for longer than claude.StuckAfter: ⚠ on its row, why on
// its card, and an inbox item right after the questions.
func TestStuckAgentShown(t *testing.T) {
	f := newReady(t)
	m := f.m
	old := time.Now().Add(-23 * time.Minute)
	_ = os.Chtimes(filepath.Join(f.dir, "jobs", "busy0001", "state.json"), old, old)
	m.refresh()
	a := m.agents["busy0001"]
	if a.Unhealthy != "no activity for 23m" {
		t.Fatalf("Unhealthy = %q", a.Unhealthy)
	}
	if m.agents["ask00001"].Unhealthy != "" {
		t.Fatal("only busy agents can look stuck")
	}

	find := func(id string) int {
		return slices.IndexFunc(m.rows, func(r layout.Row) bool { return r.ID == id })
	}
	i := find("busy0001")
	if i < 0 || !strings.Contains(ansi.Strip(m.rowLine(m.rows[i], 60)), stuckGlyph) {
		t.Fatal("no ⚠ on the tree row")
	}
	_, card, _, _ := m.agentDetail(&m.rows[i], 80)
	if txt := ansi.Strip(strings.Join(card, "\n")); !strings.Contains(txt, "no activity for 23m") {
		t.Fatalf("card doesn't say why:\n%s", txt)
	}

	m.view = viewInbox
	m.rebuild()
	q, s := find("ask00001"), find("busy0001")
	if q < 0 || s < 0 || s < q {
		t.Fatalf("inbox: question at %d, stuck at %d: want the stuck agent after the question", q, s)
	}
	if sub := ansi.Strip(m.inboxSubline(m.rows[s], 100)); !strings.Contains(sub, "⚠ no activity for 23m") {
		t.Fatalf("inbox subline %q", sub)
	}
	if a.YourTurn() {
		t.Fatal("a stuck agent isn't counted in need you")
	}
}
