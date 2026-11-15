package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

func TestPanelLatestReply(t *testing.T) {
	m := searchModel(t) // alpha: you, agent (+ a tool call), you "thanks"
	goTo(t, m, "aaaa1111")
	v := ansi.Strip(m.View())
	if strings.Contains(v, "latest reply") {
		t.Fatalf("you wrote after the agent's reply: no latest reply expected:\n%s", v)
	}

	// the agent answers in markdown
	p := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-p", "s-alpha.jsonl")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"type":"assistant","timestamp":"2026-11-14T09:03:00Z","message":{"content":[{"type":"text","text":"**Done.** The webhook:\n\n- retries 3 times\n- logs each failure"}]}}` + "\n")
	f.Close()
	v = ansi.Strip(m.View())
	for _, want := range []string{"latest reply", "Done. The webhook:", "• retries 3 times", "• logs each failure"} {
		if !strings.Contains(v, want) {
			t.Errorf("panel lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "**Done") {
		t.Errorf("markdown not rendered:\n%s", v)
	}
	// it sits at the bottom of the compact history, above the foot
	if strings.Index(v, "latest reply") > strings.Index(v, "DONE") {
		t.Errorf("latest reply should be above the foot:\n%s", v)
	}

	// a reply longer than the panel: it opens on the reply's start
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"type":"assistant","timestamp":"2026-11-14T09:04:00Z","message":{"content":[{"type":"text","text":"FIRST LINE` + strings.Repeat(`\n\nmore`, 40) + `\n\nLAST LINE"}]}}` + "\n")
	f.Close()
	m.panelFor = "" // as if just selected
	if v = ansi.Strip(m.View()); !strings.Contains(v, "latest reply") || !strings.Contains(v, "FIRST LINE") || strings.Contains(v, "LAST LINE") {
		t.Fatalf("a long reply should open at its start:\n%s", v)
	}
	for range 10 {
		m.Update(tkey("pgdown"))
	}
	if v = ansi.Strip(m.View()); !strings.Contains(v, "LAST LINE") {
		t.Fatalf("pgdn should read on to the end:\n%s", v)
	}

	// t: the whole conversation, tool calls left out
	m.Update(tkey("t"))
	v = ansi.Strip(m.View())
	if !strings.Contains(v, "FIRST LINE") || strings.Contains(v, "latest reply") { // opens on the last reply too
		t.Errorf("t: the conversation should open on the last reply:\n%s", v)
	}
	lines, _ := conversationLines(m.agents["aaaa1111"], 80)
	conv := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"please fix the Payment webhook", "The payment webhook retries now", "thanks", "• retries 3 times", "LAST LINE"} {
		if !strings.Contains(conv, want) {
			t.Errorf("conversation lacks %q:\n%s", want, conv)
		}
	}
	if strings.Contains(conv, "curl payment-service") {
		t.Errorf("conversation shows tool calls:\n%s", conv)
	}
	m.Update(tkey("t"))
	if v = ansi.Strip(m.View()); !strings.Contains(v, "latest reply") {
		t.Errorf("t again: back to compact:\n%s", v)
	}
}

func TestLatestReplyWaitsForYou(t *testing.T) {
	m := searchModel(t)
	if latestReply(m.agents["bbbb2222"]) != nil { // only your message so far
		t.Fatal("latest reply with no agent message")
	}
	if latestReply(m.agents["cccc3333"]) != nil { // no transcript
		t.Fatal("latest reply without a transcript")
	}
}

// Real sessions (a copy of the layout): the panel of one agent, compact then
// whole conversation. Skipped unless REGIA_REAL_PANEL names an agent id;
// REGIA_SCREEN_OUT saves the colored frames (<out>.compact, <out>.full).
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
	for _, mode := range []string{"compact", "full"} {
		if mode == "full" {
			m.Update(tkey("t"))
		}
		v := m.View()
		if out != "" {
			_ = os.WriteFile(out+"."+mode, []byte(v), 0o644)
		}
		t.Log("\n" + ansi.Strip(v))
	}
}
