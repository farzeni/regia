package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

// searchModel: two sessions with transcripts (found by session id, no job
// files) and one without.
func searchModel(t *testing.T) *model {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cd)
	proj := filepath.Join(cd, "projects", "-p")
	_ = os.MkdirAll(proj, 0o755)
	write := func(sid string, lines ...string) {
		if err := os.WriteFile(filepath.Join(proj, sid+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("s-alpha",
		`{"type":"user","timestamp":"2026-11-14T09:00:00Z","message":{"content":"please fix the Payment webhook"}}`,
		`{"type":"assistant","timestamp":"2026-11-14T09:01:00Z","message":{"content":[{"type":"text","text":"The payment webhook retries now"},{"type":"tool_use","name":"Bash","input":{"command":"curl payment-service"}}]}}`,
		`{"type":"user","timestamp":"2026-11-14T09:02:00Z","message":{"content":"thanks"}}`,
	)
	write("s-beta",
		`{"type":"user","timestamp":"2026-11-15T09:00:00Z","message":{"content":"what about payment refunds?"}}`,
	)
	dir := t.TempDir()
	m := newModel()
	m.w, m.h = 140, 30
	m.setAgents([]byte(`[
	 {"id":"aaaa1111","sessionId":"s-alpha","cwd":"` + dir + `","kind":"background","name":"alpha","state":"done","startedAt":1794645285576},
	 {"id":"bbbb2222","sessionId":"s-beta","cwd":"` + dir + `","kind":"background","name":"beta","state":"done","startedAt":1794645285577},
	 {"id":"cccc3333","sessionId":"s-none","cwd":"` + dir + `","kind":"background","name":"gamma","state":"done","startedAt":1794645285578}]`))
	m.View()
	return m
}

func typeQuery(m *model, s string) {
	for _, r := range s {
		k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			k = tea.KeyMsg{Type: tea.KeySpace}
		}
		m.Update(k)
	}
	// the debounce tick: run the search right away and deliver its result
	if cmd := m.runSearch(); cmd != nil {
		m.Update(cmd())
	}
	m.View()
}

func TestSearchTabCycle(t *testing.T) {
	m := searchModel(t)
	for _, want := range []viewKind{viewInbox, viewSearch, viewTree} {
		m.Update(tkey("tab"))
		if m.view != want {
			t.Fatalf("tab: view %v, want %v", m.view, want)
		}
	}
	for _, want := range []viewKind{viewSearch, viewInbox, viewTree} {
		m.Update(tkey("shift+tab"))
		if m.view != want {
			t.Fatalf("shift+tab: view %v, want %v", m.view, want)
		}
	}
	// from the input box too, and the search takes the keys from it
	m.Update(tkey("i"))
	m.Update(tkey("shift+tab"))
	if m.view != viewSearch || m.typing {
		t.Fatalf("shift+tab from the input: view %v typing %v", m.view, m.typing)
	}
	if !strings.Contains(ansi.Strip(m.header()), "[search]") {
		t.Fatalf("header: %q", ansi.Strip(m.header()))
	}
}

func TestSearchResults(t *testing.T) {
	m := searchModel(t)
	m.Update(tkey("shift+tab"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "type to search inside every session") {
		t.Fatalf("empty search view:\n%s", v)
	}
	typeQuery(m, "payment")
	// newest match first: beta, then alpha with its two messages (the tool call left out)
	want := []string{"bbbb2222", "bbbb2222#0", "aaaa1111", "aaaa1111#1", "aaaa1111#0"}
	if got := rowIDs(m); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("rows %v, want %v", got, want)
	}
	v := ansi.Strip(m.View())
	for _, s := range []string{"3 matches in 2 sessions", "agent", "The payment webhook retr", "what about payment refunds?"} {
		if !strings.Contains(v, s) {
			t.Errorf("view lacks %q:\n%s", s, v)
		}
	}
	if !strings.Contains(m.View(), stMatch.Render("payment")) && !strings.Contains(m.View(), stMatch.Render("Payment")) {
		t.Error("matches not highlighted")
	}

	// words must all be in the same message, in any case
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	typeQuery(m, "WEBHOOK fix")
	if got := rowIDs(m); strings.Join(got, " ") != "aaaa1111 aaaa1111#0" {
		t.Fatalf("two words: rows %v", got)
	}

	// tool output on demand
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	typeQuery(m, "curl")
	if len(m.rows) != 0 || !strings.Contains(ansi.Strip(m.View()), "ctrl+t searches tool output too") {
		t.Fatalf("tool call matched without ctrl+t: %v", rowIDs(m))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if cmd := m.runSearch(); cmd != nil {
		m.Update(cmd())
	}
	if got := rowIDs(m); strings.Join(got, " ") != "aaaa1111 aaaa1111#2" {
		t.Fatalf("ctrl+t: rows %v", got)
	}
}

func TestSearchPanelAndKeys(t *testing.T) {
	m := searchModel(t)
	m.Update(tkey("shift+tab"))
	typeQuery(m, "webhook")
	goTo(t, m, "aaaa1111#1")
	v := ansi.Strip(m.View())
	// the hit with its neighbours around it
	for _, s := range []string{"please fix the Payment webhook", "The payment webhook retries now", "thanks", "match 1 of 2"} {
		if !strings.Contains(v, s) {
			t.Errorf("panel lacks %q:\n%s", s, v)
		}
	}
	// fold the session to its header, unfold it
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := rowIDs(m); strings.Join(got, " ") != "aaaa1111" {
		t.Fatalf("folded: rows %v", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if len(m.rows) != 3 {
		t.Fatalf("unfolded: rows %v", rowIDs(m))
	}
	// letters type into the query, they aren't commands
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.srch.query != "webhookx" || m.view != viewSearch {
		t.Fatalf("x: query %q view %v", m.srch.query, m.view)
	}
	// esc clears the query, then goes back to the tree on the session last selected
	m.Update(tkey("esc"))
	if m.srch.query != "" || len(m.rows) != 0 {
		t.Fatalf("esc: query %q rows %v", m.srch.query, rowIDs(m))
	}
	typeQuery(m, "refunds")
	m.Update(tkey("down"))
	m.Update(tkey("esc"))
	m.Update(tkey("esc"))
	if m.view != viewTree || m.cursorID != "bbbb2222" {
		t.Fatalf("esc esc: view %v cursor %q", m.view, m.cursorID)
	}
	// coming back, the query was cleared
	m.Update(tkey("shift+tab"))
	if m.srch.query != "" {
		t.Fatalf("query %q", m.srch.query)
	}
}

func TestSnippetShowsMatch(t *testing.T) {
	long := strings.Repeat("lorem ipsum ", 30) + "the needle is here " + strings.Repeat("dolor ", 30)
	q := parseQ("needle")
	s := snippet(long, q, 40)
	if !strings.Contains(s, "needle") || !strings.HasPrefix(s, "…") {
		t.Fatalf("snippet %q", s)
	}
}

func parseQ(s string) claude.Query { return claude.ParseQuery(s) }

// Real sessions, real layout (a copy): prints the search view for a query.
// Skipped unless REGIA_REAL_SEARCH is set.
func TestSearchRealScreen(t *testing.T) {
	q := os.Getenv("REGIA_REAL_SEARCH")
	if q == "" {
		t.Skip("set REGIA_REAL_SEARCH")
	}
	home, _ := os.UserHomeDir()
	cfg := t.TempDir()
	_ = os.MkdirAll(filepath.Join(cfg, "regia"), 0o755)
	if b, err := os.ReadFile(filepath.Join(home, ".config", "regia", "layout.json")); err == nil {
		_ = os.WriteFile(filepath.Join(cfg, "regia", "layout.json"), b, 0o644)
	}
	t.Setenv("XDG_CONFIG_HOME", cfg)
	if os.Getenv("REGIA_SCREEN_OUT") != "" {
		withColors(t)
	}
	b, err := claude.FetchAgents()
	if err != nil {
		t.Fatal(err)
	}
	m := newModel()
	m.w, m.h = 150, 40
	m.setAgents(b)
	m.Update(tkey("shift+tab"))
	typeQuery(m, q)
	for range 3 {
		m.Update(tkey("down"))
	}
	if out := os.Getenv("REGIA_SCREEN_OUT"); out != "" { // the colored screen, to look at
		_ = os.WriteFile(out, []byte(m.View()), 0o644)
	}
	t.Log("\n" + ansi.Strip(m.View()))
}

// The query stays live while you move: type, move, enter opens; typing more
// after moving refines the results and starts again from the top.
func TestSearchMoveWhileTyping(t *testing.T) {
	m := searchModel(t)
	m.Update(tkey("shift+tab"))
	typeQuery(m, "payment")
	m.Update(tkey("down"))
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if m.cursorID != "bbbb2222#0" {
		t.Fatalf("down: cursor %s", m.cursorID)
	}
	typeQuery(m, " refunds")
	if got := rowIDs(m); strings.Join(got, " ") != "bbbb2222 bbbb2222#0" || m.cursorID != "bbbb2222" {
		t.Fatalf("refined: rows %v cursor %s", got, m.cursorID)
	}
	m.Update(tkey("down"))
	if _, cmd := m.Update(tkey("enter")); cmd == nil || m.cursorID != "bbbb2222#0" {
		t.Fatalf("enter did not attach the highlighted hit: cmd=%v cursor %s", cmd != nil, m.cursorID)
	}
}
