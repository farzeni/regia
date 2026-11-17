package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/plugins"
	"github.com/farzeni/regia/internal/prinfo"
)

// The agent's box is tabbed: "agent" (the context) first, then one tab per
// plugin with something to say; , and . switch, the choice is kept across
// agents, the box keeps its height, and a click on a tab picks it.
func TestTabbedBox(t *testing.T) {
	f := newReady(t)
	m := f.m
	m.w, m.h = 160, 40
	m.opts.ShowDone = true
	m.cursorID = "done0001"
	m.rebuild()
	p := &plugins.Manifest{Name: "gh-pr", Command: []string{"x"}}
	plugins.SetForTest([]*plugins.Manifest{p})
	defer plugins.SetForTest(nil)
	plugins.Seed(p, "done0001", plugins.Output{Fields: []plugins.Field{{Label: "ci", Text: "3/3 · 1 failed", Status: "error"}},
		Badge: &plugins.Badge{Text: "✗", Status: "error"}})
	m.agents["done0001"].Unhealthy = "no output for 20m" // a context line (the fixture has no task, git or status)

	boxOf := func() []string {
		_, card, _ := m.agentDetail(m.selected(), 70)
		var b []string
		for _, l := range card {
			if s := ansi.Strip(l); strings.HasPrefix(s, "╭") || strings.HasPrefix(s, "│") || strings.HasPrefix(s, "╰") {
				if ansi.StringWidth(l) != 70 {
					t.Fatalf("box line %d wide, want 70: %q", ansi.StringWidth(l), s)
				}
				b = append(b, s)
			}
		}
		return b
	}
	b := boxOf()
	if !strings.HasPrefix(b[0], "╭─ agent │ changes │ gh-pr ✗ ─") || !strings.Contains(strings.Join(b, "\n"), "stuck?") {
		t.Fatalf("agent tab first:\n%s", strings.Join(b, "\n"))
	}
	h := len(b)
	m.Update(tkey("."))
	if m.panelTab != changesTab {
		t.Fatalf(". goes to the changes tab first, got %q", m.panelTab)
	}
	m.Update(tkey("."))
	b = boxOf()
	if m.panelTab != "gh-pr" || !strings.Contains(strings.Join(b, "\n"), "│ ci  3/3 · 1 failed") || strings.Contains(strings.Join(b, "\n"), "stuck?") {
		t.Fatalf(". shows the plugin tab:\n%s", strings.Join(b, "\n"))
	}
	if len(b) != h {
		t.Fatalf("switching tabs changed the box's height %d → %d", h, len(b))
	}
	m.Update(tkey("."))
	if m.panelTab != "agent" {
		t.Fatalf(". wraps around to agent, got %q", m.panelTab)
	}
	m.Update(tkey(","))
	if m.panelTab != "gh-pr" {
		t.Fatalf(", goes back, got %q", m.panelTab)
	}

	// kept when moving to another agent; one without that plugin shows "agent"
	m.cursorID = "busy0001"
	m.rebuild()
	if b := boxOf(); len(b) > 0 && strings.Contains(b[0], "gh-pr") {
		t.Fatalf("busy0001 has no gh-pr tab: %q", b[0])
	}
	if m.panelTab != "gh-pr" {
		t.Fatal("the choice stays for the next agent that has the tab")
	}

	// a click on "agent" in the tab bar picks it
	m.cursorID = "done0001"
	m.rebuild()
	m.View()
	if len(m.tabHits) != 3 {
		t.Fatalf("tab hits %+v", m.tabHits)
	}
	x := padX + m.lw + gap + m.tabHits[0].x0
	m.Update(tea.MouseMsg{X: x, Y: listTop + m.tabRow, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.panelTab != "agent" {
		t.Fatalf("click on the agent tab: %q", m.panelTab)
	}
}

// regia's own pr line steps aside when a plugin's card links the same PR.
func TestPluginHidesDuplicatePRLine(t *testing.T) {
	f := newReady(t)
	m := f.m
	a := m.agents["done0001"]
	a.Cwd, a.Branch = t.TempDir(), "feat-pr"
	prinfo.Seed(a.Cwd, "feat-pr", prinfo.Info{OK: true, Number: 5, State: "open", URL: "https://gh/o/r/pull/5"})
	p := &plugins.Manifest{Name: "gh-pr", Command: []string{"x"}}
	plugins.SetForTest([]*plugins.Manifest{p})
	defer plugins.SetForTest(nil)
	m.opts.ShowDone = true
	m.cursorID = "done0001"
	m.rebuild()
	builtin := func() bool {
		return strings.Contains(ansi.Strip(strings.Join(m.detailLines(100).lines(), "\n")), "#5 · open")
	}
	if !builtin() {
		t.Fatal("no plugin: regia's pr line shows")
	}
	plugins.Seed(p, "done0001", plugins.Output{Fields: []plugins.Field{{Text: "other", URL: "https://jira/X-1"}}})
	if !builtin() {
		t.Fatal("a plugin linking elsewhere: regia's pr line stays")
	}
	plugins.Seed(p, "done0001", plugins.Output{Fields: []plugins.Field{{Label: "pr", Text: "#5 open", URL: "https://gh/o/r/pull/5/"}}})
	if builtin() {
		t.Fatal("a plugin linking the PR: regia's pr line is hidden")
	}
}

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
	for _, want := range []string{"╭─ agent │ changes │ jira J ", "! actions ─╮"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card lacks %q:\n%s", want, card)
		}
	}
	m.Update(tkey("."))
	m.Update(tkey("."))
	card = ansi.Strip(strings.Join(m.detailLines(100).lines(), "\n"))
	for _, want := range []string{"│ jira  PROJ-7 · In Progress", "https://jira/PROJ-7", "╰──"} {
		if !strings.Contains(card, want) {
			t.Fatalf("jira tab lacks %q:\n%s", want, card)
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
	tabs := pluginTabs(a, 70)
	if len(tabs) != 1 || tabs[0].name != p.Name || !strings.Contains(ansi.Strip(strings.Join(tabs[0].body, "\n")), "⚠ exit status 1: gh: not logged in") {
		t.Fatalf("%+v", tabs)
	}

	// a plugin with nothing to say gets no tab
	plugins.Seed(p, "busy0001", plugins.Output{})
	if tabs := pluginTabs(a, 70); len(tabs) != 0 {
		t.Fatalf("empty result made a tab: %+v", tabs)
	}
}
