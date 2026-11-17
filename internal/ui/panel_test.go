package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/group"
)

// giveTranscript writes a transcript for agent id, alternating your messages
// and the agent's replies (msgs[0] is yours), a minute apart and ending a
// minute ago, plus a tool call after each reply.
func giveTranscript(t *testing.T, m *model, id string, msgs ...string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), id+".jsonl")
	var b strings.Builder
	at := time.Now().Add(-time.Duration(len(msgs)) * time.Minute)
	line := func(v any) {
		j, _ := json.Marshal(v)
		b.Write(append(j, '\n'))
	}
	for i, s := range msgs {
		ts := at.Add(time.Duration(i) * time.Minute).UTC().Format(time.RFC3339)
		if i%2 == 0 {
			line(map[string]any{"type": "user", "timestamp": ts, "message": map[string]any{"content": s}})
			continue
		}
		// the agent runs a command, then answers
		tid := fmt.Sprintf("toolu_%d", i)
		line(map[string]any{"type": "assistant", "timestamp": ts, "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use", "id": tid, "name": "Bash", "input": map[string]any{"command": "tool call " + s}},
		}}})
		line(map[string]any{"type": "user", "timestamp": ts, "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": tid, "content": "tool output " + s},
		}}})
		line(map[string]any{"type": "assistant", "timestamp": ts, "message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": s},
		}}})
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	m.agents[id].Transcript = p
}

// exchanges is n turns of "ask NN" / "reply NN".
func exchanges(n int) []string {
	var out []string
	for i := range n {
		out = append(out, fmt.Sprintf("ask %02d", i), fmt.Sprintf("reply %02d", i))
	}
	return out
}

// renderPanel is the agent panel as the right side draws it, w×h.
func renderPanel(m *model, w, h int) []string {
	m.panelFor = "" // as if just selected
	ls := m.placePanel(m.detailLines(w), h, w)
	for i := range ls {
		ls[i] = strings.TrimRight(ansi.Strip(ls[i]), " ")
	}
	return ls
}

func TestAgentPanelLayout(t *testing.T) {
	m := testModel(t)
	m.Update(tkey("esc"))
	goTo(t, m, "aaaa1111")
	r := m.selected()
	msgs := append(exchanges(8), "fix the panel", "Done: the panel shows the conversation now.")
	giveTranscript(t, m, "aaaa1111", msgs...)
	m.hist, m.histFor = claude.History{Task: "the task", Turns: []claude.Turn{{
		User:    &claude.HEntry{At: time.Now(), Detail: "fix the panel"},
		Results: []claude.HEntry{{At: time.Now(), State: "done", Detail: "JOB STATE LINE"}},
	}}}, r.ID
	a := m.agents["aaaa1111"]
	a.State, a.Needs, a.Suggested = "blocked", "Which key should commit the rename?", "enter, and esc cancels"

	ls := renderPanel(m, 60, 24)
	all := strings.Join(ls, "\n")
	t.Log("\n" + all)
	if !strings.HasPrefix(ls[0], "╭") || !strings.Contains(ls[1], "the task") {
		t.Fatalf("the context box should be on top:\n%s", all)
	}
	// the ask between the box and the conversation
	ask := strings.Index(all, "Which key should commit")
	if ask < strings.Index(all, "╰") || ask > strings.Index(all, "▸ ") || !strings.Contains(all, "y copies") {
		t.Fatalf("the ask should sit between the box and the conversation:\n%s", all)
	}
	// the foot: rule, state, meta, at the very bottom
	n := len(ls)
	if !strings.Contains(ls[n-2], "alpha") || !strings.Contains(ls[n-1], "aaaa1111") || !strings.HasPrefix(ls[n-3], "────") {
		t.Fatalf("the foot should be at the bottom:\n%s", all)
	}
	// the body ends with the latest reply, its time on the line above, right
	// above the foot
	if !strings.Contains(ls[n-4], "Done: the panel shows the conversation now.") || !regexp.MustCompile(`^\s+\d\d:\d\d\s*$`).MatchString(ansi.Strip(ls[n-5])) {
		t.Fatalf("the latest reply should sit right above the foot:\n%s", all)
	}
	if !strings.Contains(all, "fix the panel") || !strings.Contains(all, "earlier · pgup") {
		t.Fatalf("the conversation should open at its end with earlier messages above:\n%s", all)
	}
	// no compact history (job states), no tool calls, no hint to toggle
	for _, bad := range []string{"JOB STATE LINE", "tool call", "latest reply", "t: whole"} {
		if strings.Contains(all, bad) {
			t.Fatalf("panel should not show %q:\n%s", bad, all)
		}
	}
	// pgup scrolls back to the first message, the box and foot stay
	for range 10 {
		m.Update(tkey("pgup"))
	}
	ls = m.placePanel(m.detailLines(60), 24, 60)
	all = ansi.Strip(strings.Join(ls, "\n"))
	if !strings.Contains(all, "ask 00") || !strings.Contains(all, "newer · pgdn") || !strings.Contains(all, "the task") || !strings.Contains(all, "aaaa1111") {
		t.Fatalf("pgup should scroll the conversation back, box and foot pinned:\n%s", all)
	}
}

func TestAgentPanelNoConversation(t *testing.T) {
	m := testModel(t)
	m.Update(tkey("esc"))
	goTo(t, m, "aaaa1111")
	if all := strings.Join(renderPanel(m, 60, 20), "\n"); !strings.Contains(all, "no conversation yet") {
		t.Fatalf("an agent without a transcript says so:\n%s", all)
	}
}

func TestCoordinatorPanelWithoutBlackboard(t *testing.T) {
	m := teamModel(t)
	group.AddNote("aaaa1111", group.Note{By: "bbbb2222", Name: "beta", Text: "NOTE FOR THE TEAM"})
	giveTranscript(t, m, "aaaa1111", "coordinate the team", "Started beta on the panel.")
	goTo(t, m, "aaaa1111")
	if !m.selected().HasKids {
		t.Fatal("alpha should be a coordinator")
	}
	m.hist = claude.History{Task: "coordinate the team"}
	ls := renderPanel(m, 60, 16)
	all := strings.Join(ls, "\n")
	t.Log("\n" + all)
	if strings.Contains(all, "NOTE FOR THE TEAM") || strings.Contains(all, "blackboard") {
		t.Fatalf("a coordinator's panel should not show the blackboard:\n%s", all)
	}
	if n := len(ls); !strings.Contains(ls[n-4], "Started beta on the panel.") || !strings.Contains(ls[n-2], "alpha") {
		t.Fatalf("a coordinator's panel is an agent's: conversation, then the foot:\n%s", all)
	}
	// N still opens it
	m.Update(tkey("N"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "NOTE FOR THE TEAM") {
		t.Fatalf("N should show the coordinator's blackboard:\n%s", v)
	}
}

func TestPanelKeyTGone(t *testing.T) {
	m := testModel(t)
	m.Update(tkey("esc"))
	goTo(t, m, "aaaa1111")
	giveTranscript(t, m, "aaaa1111", "do it", "did it")
	before := strings.Join(renderPanel(m, 60, 16), "\n")
	for _, k := range []string{"t"} { // o focuses the conversation now
		m.status = ""
		if m.Update(tkey(k)); m.status != "" {
			t.Fatalf("%s should do nothing", k)
		}
		if after := strings.Join(renderPanel(m, 60, 16), "\n"); after != before {
			t.Fatalf("%s should not change the panel:\n%s", k, after)
		}
	}
	m.help = true
	if v := ansi.Strip(m.View()); strings.Contains(v, "terminal output") || strings.Contains(v, "whole conversation") {
		t.Fatalf("help should not list o or t:\n%s", v)
	}
}
