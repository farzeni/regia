package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// TestSearchHistory: ctrl+g picks the whole history, which finds a session
// regia doesn't list any more, named by its title; enter resumes it.
func TestSearchHistory(t *testing.T) {
	m := searchModel(t)
	proj := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-p")
	gone := strings.Join([]string{
		`{"type":"user","cwd":"/work/old","timestamp":"2026-10-01T09:00:00Z","message":{"content":"migrate the invoice table"}}`,
		`{"type":"ai-title","aiTitle":"Invoice migration","sessionId":"s-gone"}`,
		`{"type":"assistant","timestamp":"2026-10-01T09:05:00Z","message":{"content":[{"type":"text","text":"invoice table migrated"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(proj, "s-gone.jsonl"), []byte(gone), 0o644); err != nil {
		t.Fatal(err)
	}
	m.setView(viewSearch)
	typeQuery(m, "invoice")
	if all := ansi.Strip(m.View()); strings.Contains(all, "Invoice migration") || !strings.Contains(all, "ctrl+g the whole history") {
		t.Fatalf("the listed sessions only, with a pointer to the history:\n%s", all)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if all := ansi.Strip(m.View()); !strings.Contains(all, "every conversation on disk") {
		t.Fatalf("ctrl+g should list the scopes:\n%s", all)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("picking history should search again")
	}
	m.Update(cmd())
	all := ansi.Strip(m.View())
	if !strings.Contains(all, "search history ›") || !strings.Contains(all, "Invoice migration") || !strings.Contains(all, "enter resumes it") {
		t.Fatalf("history should find the removed session by its title:\n%s", all)
	}
	if m.srch.query != "invoice" {
		t.Fatalf("the scope picker changed the query: %q", m.srch.query)
	}

	var sid, dir string
	defer func(f func(string, string) tea.Cmd) { resumeSession = f }(resumeSession)
	resumeSession = func(s, d string) tea.Cmd { sid, dir = s, d; return nil }
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if sid != "s-gone" || dir != "/work/old" {
		t.Fatalf("enter should resume s-gone in /work/old, got %q in %q", sid, dir)
	}

	// listed sessions keep their own row, not a second one from the history
	m.setQuery("payment")
	m.Update(m.runSearch()())
	n := 0
	for _, r := range m.rows {
		if r.HasKids {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("payment is in the two listed sessions, got %d sessions:\n%s", n, ansi.Strip(m.View()))
	}
}
