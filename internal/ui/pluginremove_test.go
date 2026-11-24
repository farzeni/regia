package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/plugins"
)

// TestPluginActionRemoves: an action answering {"remove": true} removes the
// agent the way x does, with what the gates read about it.
func TestPluginActionRemoves(t *testing.T) {
	m := testModel(t)
	var got []plugins.Input
	orig := removeAgent
	t.Cleanup(func() { removeAgent = orig })
	removeAgent = func(in plugins.Input) (string, error) { got = append(got, in); return "", nil }

	_, cmd := m.Update(pluginActionMsg{plugin: "handoff", label: "remove", agent: "aaaa1111", out: plugins.Output{Remove: true, Message: "handoff found"}})
	if cmd == nil || m.agents["aaaa1111"] != nil {
		t.Fatalf("the agent leaves the list at once: %v", m.agents["aaaa1111"] != nil)
	}
	m.Update(runCmd(cmd))
	if len(got) != 1 || got[0].Agent.ID != "aaaa1111" || got[0].Agent.Title != "alpha" || !strings.Contains(m.status, "removed aaaa1111") {
		t.Fatalf("removed through removeAgent: %+v, status %q", got, m.status)
	}
}

// TestRemovedSessionPlugins: in the whole history, a removed session shows
// what the plugins naming "removed" say about it (its handoff), and ctrl+o
// offers their links.
func TestRemovedSessionPlugins(t *testing.T) {
	m := searchModel(t)
	proj := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-p")
	gone := `{"type":"user","cwd":"/work/old","timestamp":"2026-10-01T09:00:00Z","message":{"content":"migrate the invoice table"}}` + "\n" +
		`{"type":"ai-title","aiTitle":"Invoice migration","sessionId":"s-gone"}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "s-gone.jsonl"), []byte(gone), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &plugins.Manifest{Name: "handoff", Command: []string{"true"}, States: []string{plugins.Removed}}
	plugins.SetForTest([]*plugins.Manifest{p})
	defer plugins.SetForTest(nil)
	plugins.Seed(p, "s-gone", plugins.Output{Fields: []plugins.Field{{Label: "handoff", Text: "ENG-12 invoice migration", URL: "file:///work/handoffs/s-gone.md"}}})

	m.setView(viewSearch)
	m.srch.scope = scopeHistory
	typeQuery(m, "invoice")
	m.Update(m.runSearch()())
	all := ansi.Strip(m.View())
	if !strings.Contains(all, "ENG-12 invoice migration") || !strings.Contains(all, "ctrl+o: its plugin links") {
		t.Fatalf("the plugin's fields under the removed session:\n%s", all)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if m.mode != modeMenu || !strings.Contains(m.menu[0].label, "s-gone.md") {
		t.Fatalf("ctrl+o opens its links: mode %v %+v", m.mode, m.menu)
	}
}
