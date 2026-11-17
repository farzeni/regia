package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

func TestPanelConversation(t *testing.T) {
	m := searchModel(t) // alpha: you, agent (+ a tool call), you "thanks"
	goTo(t, m, "aaaa1111")

	// the agent answers in markdown
	p := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-p", "s-alpha.jsonl")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"type":"assistant","timestamp":"2026-11-14T09:03:00Z","message":{"content":[{"type":"text","text":"**Done.** The webhook:\n\n- retries 3 times\n- logs each failure"}]}}` + "\n")
	f.Close()
	v := ansi.Strip(m.View())
	for _, want := range []string{"thanks", "Done. The webhook:", "• retries 3 times", "• logs each failure"} {
		if !strings.Contains(v, want) {
			t.Errorf("panel lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "**Done") {
		t.Errorf("markdown not rendered:\n%s", v)
	}
	if strings.Index(v, "logs each failure") > strings.Index(v, "DONE") {
		t.Errorf("the conversation should be above the foot:\n%s", v)
	}

	// a reply longer than the panel: it opens at the end, pgup reads back
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"type":"assistant","timestamp":"2026-11-14T09:04:00Z","message":{"content":[{"type":"text","text":"FIRST LINE` + strings.Repeat(`\n\nmore`, 40) + `\n\nLAST LINE"}]}}` + "\n")
	f.Close()
	m.panelFor = "" // as if just selected
	if v = ansi.Strip(m.View()); !strings.Contains(v, "LAST LINE") || strings.Contains(v, "FIRST LINE") {
		t.Fatalf("the conversation should open at its end:\n%s", v)
	}
	m.h = 40 // room for the head and the first line under the top messages
	for range 20 {
		m.Update(tkey("pgup"))
	}
	if v = ansi.Strip(m.View()); !strings.Contains(v, "FIRST LINE") {
		t.Fatalf("pgup should read back:\n%s", v)
	}

	conv := ansi.Strip(strings.Join(m.conversationLines(m.agents["aaaa1111"], 80), "\n"))
	for _, want := range []string{"please fix the Payment webhook", "The payment webhook retries now", "thanks", "• retries 3 times", "LAST LINE"} {
		if !strings.Contains(conv, want) {
			t.Errorf("conversation lacks %q:\n%s", want, conv)
		}
	}
	if strings.Contains(conv, "curl payment-service") {
		t.Errorf("conversation shows tool calls:\n%s", conv)
	}
}

// Real sessions (a copy of the layout): the panel of one agent. Skipped
// unless REGIA_REAL_PANEL names an agent id; REGIA_SCREEN_OUT saves the
// colored frame.
func TestPanelRealScreen(t *testing.T) {
	id := os.Getenv("REGIA_REAL_PANEL")
	if id == "" {
		t.Skip("set REGIA_REAL_PANEL")
	}
	home, _ := os.UserHomeDir()
	cfg := t.TempDir()
	_ = os.MkdirAll(filepath.Join(cfg, "regia"), 0o755)
	if b, err := os.ReadFile(filepath.Join(home, ".config", "regia", "layout.json")); err == nil {
		_ = os.WriteFile(filepath.Join(cfg, "regia", "layout.json"), b, 0o644)
	}
	t.Setenv("XDG_CONFIG_HOME", cfg)
	out := os.Getenv("REGIA_SCREEN_OUT")
	if out != "" {
		withColors(t)
	}
	b, err := claude.FetchAgents()
	if err != nil {
		t.Fatal(err)
	}
	m := newModel()
	m.w, m.h = 150, 45
	m.setAgents(b)
	goTo(t, m, id)
	v := m.View()
	if out != "" {
		_ = os.WriteFile(out, []byte(v), 0o644)
	}
	t.Log("\n" + ansi.Strip(v))
}
