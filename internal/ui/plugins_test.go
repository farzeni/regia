package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/plugins"
)

// What a plugin says shows up on the card, as a badge on the row, puts the
// agent in the inbox with the reason, and fills the ! menu.
func TestPluginResultsInUI(t *testing.T) {
	f := newReady(t)
	m := f.m
	p := &plugins.Manifest{Name: "jira", Command: []string{"x"}}
	plugins.SetForTest([]*plugins.Manifest{p})
	defer plugins.SetForTest(nil)

	now := time.Now()
	inInbox := func() bool {
		for _, r := range m.inboxRows(now) {
			if r.ID == "done0001" {
				return true
			}
		}
		return false
	}
	if inInbox() {
		t.Fatal("seen done agent without attention is not in the inbox")
	}
	plugins.Seed(p, "done0001", plugins.Output{
		Fields:    []plugins.Field{{Text: "PROJ-7 · In Progress", Status: "warn", URL: "https://jira/PROJ-7"}},
		Badge:     &plugins.Badge{Text: "J"},
		Attention: "PR open but ticket In Progress",
		Actions:   []plugins.Action{{ID: "review", Label: "move to In Review", Confirm: true}},
	})
	if !inInbox() {
		t.Fatal("attention brings it into the inbox")
	}
	a := m.agents["done0001"]
	if s := ansi.Strip(m.inboxSubline(m.inboxRow(a, 0), 140)); !strings.Contains(s, "jira: PR open but ticket In Progress") {
		t.Fatalf("subline %q", s)
	}

	m.opts.ShowDone = true
	m.rebuild()
	m.cursorID = "done0001"
	m.rebuild()
	r := m.selected()
	if r == nil || r.ID != "done0001" {
		t.Fatal("select it")
	}
	if l := ansi.Strip(m.rowLine(*r, 60)); !strings.Contains(l, " J") {
		t.Fatalf("row %q has no badge", l)
	}
	card := ansi.Strip(strings.Join(m.detailLines(100).lines(), "\n"))
	for _, want := range []string{"╭─ jira ", "! actions ─╮", "│ jira  PROJ-7 · In Progress", "https://jira/PROJ-7", "╰──"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card lacks %q:\n%s", want, card)
		}
	}
	// every line of the box is exactly as wide as the panel
	for _, l := range pluginCards(m.agents["done0001"], 60) {
		if l != "" && ansi.StringWidth(l) != 60 {
			t.Fatalf("box line %d wide, want 60: %q", ansi.StringWidth(l), ansi.Strip(l))
		}
	}

	m.Update(tkey("!"))
	if m.mode != modeMenu || len(m.menu) != 2 || !strings.Contains(m.menu[0].label, "move to In Review") ||
		!strings.Contains(m.menu[1].label, "open jira") {
		t.Fatalf("menu %+v", m.menu)
	}
	if !strings.Contains(ansi.Strip(m.View()), "move to In Review") {
		t.Fatal("menu drawn")
	}
	m.Update(tkey("esc"))
	if m.mode != modeNormal || m.menu != nil {
		t.Fatal("esc closes the menu")
	}
	m.Update(tkey("!"))
	m.Update(tkey("1"))
	if m.mode != modeConfirm || m.confirmKey != "1" || !strings.Contains(m.confirmMsg, "move to In Review") {
		t.Fatalf("confirm: mode %v key %q msg %q", m.mode, m.confirmKey, m.confirmMsg)
	}
	m.Update(tkey("esc"))
	if m.mode != modeNormal {
		t.Fatal("anything else cancels")
	}

	m.layout.Archive("done0001")
	if inInbox() {
		t.Fatal("archived agents stay out")
	}
}

// A failing plugin says why, dim, on the card.
func TestPluginErrorOnCard(t *testing.T) {
	f := newReady(t)
	m := f.m
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"command":["./check.sh"],"label":"ci"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte("#!/bin/sh\necho 'gh: not logged in' >&2; exit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := plugins.LoadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	plugins.SetForTest([]*plugins.Manifest{p})
	defer plugins.SetForTest(nil)
	plugins.Seed(p, "busy0001", plugins.Output{Fields: []plugins.Field{{Text: "deploy-preview ✓"}}})
	a := m.agents["busy0001"]
	if fs := m.pluginRefreshers(a); len(fs) != 0 {
		t.Fatal("fresh result: nothing due")
	}
	plugins.SetForTest([]*plugins.Manifest{p}) // clears the cache: due again
	for _, f := range m.pluginRefreshers(a) {
		f()
	}
	card := ansi.Strip(strings.Join(pluginCards(a, 70), "\n"))
	if !strings.Contains(card, "╭─ "+p.Name) || !strings.Contains(card, "│ ⚠ exit status 1: gh: not logged in") {
		t.Fatalf("%s", card)
	}

	// a plugin with nothing to say gets no box
	plugins.Seed(p, "busy0001", plugins.Output{})
	if c := pluginCards(a, 70); len(c) != 0 {
		t.Fatalf("empty result drew %q", c)
	}
}
