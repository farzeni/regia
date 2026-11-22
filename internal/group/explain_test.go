package group

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAsks(t *testing.T) {
	for p, want := range map[string]bool{
		"check your siblings":                    true,
		"use regia context before you start":     true,
		"ask your coordinator":                   true,
		"what are the others in the group doing": true,
		"controlla i tuoi fratelli":              true,
		"guarda cosa fa il tuo gruppo":           true,
		"fix the bug in regia's sidebar":         false,
		"group the rows by date":                 false,
		"write release notes":                    false,
	} {
		if Asks(p) != want {
			t.Errorf("Asks(%q) = %v", p, !want)
		}
	}
}

// A user message about the group, or a compacted conversation, brings the
// intro back; an agent in no group is told so when asked.
func TestAnswerAndReintro(t *testing.T) {
	tr := fixture(t)
	t0 := time.Date(2026, 11, 14, 10, 0, 0, 0, time.Local)
	intro := func(msg string) bool {
		return strings.Contains(msg, "You are part of a group") && strings.Contains(msg, "your siblings are the other agents") &&
			strings.Contains(msg, "Your coordinator is") && strings.Contains(msg, "`regia context`")
	}

	msg := Check(tr, "w1", "PostToolUse", t0, names)
	if !intro(msg) {
		t.Fatalf("first intro:\n%s", msg)
	}
	if n := len(strings.Fields(msg)); n > 120 {
		t.Fatalf("intro is %d words", n)
	}
	if msg := Answer(tr, "w1", t0.Add(time.Minute), names); !intro(msg) {
		t.Fatalf("asked after the intro: intro again:\n%s", msg)
	}
	if msg := Answer(tr, "w1", t0.Add(2*time.Minute), names); msg != "" {
		t.Fatalf("asked again within ExplainEvery: %q", msg)
	}
	if msg := Answer(tr, "w1", t0.Add(15*time.Minute), names); !intro(msg) {
		t.Fatalf("asked later:\n%s", msg)
	}
	if msg := Check(tr, "w1", "UserPromptSubmit", t0.Add(16*time.Minute), names); msg != "" {
		t.Fatalf("nothing new after the intro: %q", msg)
	}

	if msg := Reintro(tr, "w1", t0.Add(17*time.Minute), names); !intro(msg) {
		t.Fatalf("after compaction:\n%s", msg)
	}

	if msg := Answer(tr, "lonely", t0, names); !strings.Contains(msg, "not in a regia group") || !strings.Contains(msg, "no siblings") {
		t.Fatalf("lonely asked: %q", msg)
	}
	if msg := Answer(tr, "lonely", t0.Add(time.Minute), names); msg != "" {
		t.Fatalf("lonely asked again: %q", msg)
	}
	if msg := Reintro(tr, "lonely", t0, names); msg != "" {
		t.Fatalf("lonely compacted: %q", msg)
	}
	if c := Context(tr, tr.Resolve("w1"), nil, t0); !strings.Contains(c, "(siblings: the other agents in your group") {
		t.Fatalf("context explains its words:\n%s", c)
	}
}

// What the hook answers to each payload, as Claude Code sends them.
func TestHookPrompts(t *testing.T) {
	fixture(t)
	run := func(job, payload string) string {
		var out bytes.Buffer
		HandleHook(strings.NewReader(payload), &out, "/home/u/.claude/jobs/"+job, time.Now())
		var got struct {
			HookSpecificOutput struct{ HookEventName, AdditionalContext string }
		}
		if out.Len() > 0 {
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatalf("%q: %v", out.String(), err)
			}
		}
		return got.HookSpecificOutput.HookEventName + "|" + got.HookSpecificOutput.AdditionalContext
	}
	prompt := func(p string) string {
		return fmt.Sprintf(`{"session_id":"x","hook_event_name":"UserPromptSubmit","prompt":%q}`, p)
	}

	// a spawned agent gets the intro at once, before its first tool call
	if out := run("w2", `{"hook_event_name":"SessionStart","source":"startup"}`); !strings.HasPrefix(out, "SessionStart|[regia] You are part of a group") {
		t.Fatalf("startup: %q", out)
	}
	if out := run("w2", prompt("fix the sidebar")); out != "|" {
		t.Fatalf("unrelated prompt after the intro: %q", out)
	}
	if out := run("w2", prompt("check your siblings and go")); !strings.Contains(out, "your siblings are") {
		t.Fatalf("siblings prompt: %q", out)
	}
	if out := run("w2", `{"hook_event_name":"SessionStart","source":"resume"}`); out != "|" {
		t.Fatalf("resume keeps the conversation: %q", out)
	}
	if out := run("w2", `{"hook_event_name":"SessionStart","source":"compact"}`); !strings.HasPrefix(out, "SessionStart|[regia] You are part of a group") {
		t.Fatalf("compact: %q", out)
	}

	if out := run("lonely", `{"hook_event_name":"SessionStart","source":"startup"}`); out != "|" {
		t.Fatalf("lonely startup: %q", out)
	}
	if out := run("lonely", prompt("fix the sidebar")); out != "|" {
		t.Fatalf("lonely, unrelated: %q", out)
	}
	if out := run("lonely", prompt("use regia context to see what the others did")); !strings.Contains(out, "UserPromptSubmit|[regia] You are not in a regia group") {
		t.Fatalf("lonely asked: %q", out)
	}
}
