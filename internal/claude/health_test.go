package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// healthJob makes a synthetic job (state, timeline) and transcript, all
// written at mod, and returns the agent.
func healthJob(t *testing.T, transcript []string, mod time.Time) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	job := filepath.Join(dir, "jobs", "abcd1234")
	_ = os.MkdirAll(job, 0o755)
	tr := filepath.Join(dir, "projects", "p", "sess.jsonl")
	_ = os.MkdirAll(filepath.Dir(tr), 0o755)
	for p, body := range map[string]string{
		filepath.Join(job, "state.json"):     `{"state":"working"}`,
		filepath.Join(job, "timeline.jsonl"): `{"at":"2026-11-15T10:00:00Z","state":"working","detail":"x","text":""}` + "\n",
		tr:                                   strings.Join(transcript, "\n") + "\n",
	} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(p, mod, mod)
	}
	return &Agent{ID: "abcd1234", State: "busy", Transcript: tr}, tr
}

func toolUse(id, name string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":%q,"name":%q,"input":{}}]}}`, id, name)
}

func toolResult(id, content string, isErr bool) string {
	return fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":%q,"content":%q,"is_error":%v}]}}`, id, content, isErr)
}

func TestHealthQuiet(t *testing.T) {
	now := time.Now()
	a, tr := healthJob(t, []string{toolUse("1", "Bash"), toolResult("1", "ok", false)}, now.Add(-23*time.Minute))
	if got := Health(a, now); got != "no activity for 23m" {
		t.Fatalf("quiet 23m: %q", got)
	}
	// a subagent still writing counts as activity
	sub := filepath.Join(strings.TrimSuffix(tr, ".jsonl"), "subagents")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(sub, "agent-1.jsonl"), []byte("{}\n"), 0o644)
	if got := Health(a, now); got != "" {
		t.Fatalf("subagent active: %q", got)
	}
	// recent activity, or not busy: fine
	a2, _ := healthJob(t, nil, now.Add(-2*time.Minute))
	if got := Health(a2, now); got != "" {
		t.Fatalf("active 2m ago: %q", got)
	}
	a2.State = "blocked"
	if got := Health(a2, now.Add(time.Hour)); got != "" {
		t.Fatalf("blocked is not stuck: %q", got)
	}
}

func TestHealthRepeatedError(t *testing.T) {
	now := time.Now()
	var lines []string
	for i := range 5 {
		id := fmt.Sprint(i)
		lines = append(lines, toolUse(id, "Bash"), toolResult(id, "Exit code 1\ngo: cannot find main module", true))
	}
	// the api-request-blob echo of the same results doesn't count twice
	lines = append(lines, `{"type":"api-request-blob","message":{"content":[{"type":"tool_result","tool_use_id":"4","content":"x","is_error":true}]}}`)
	a, tr := healthJob(t, lines, now)
	want := "same Bash error 5×: go: cannot find main module"
	if got := Health(a, now); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// appended lines are read incrementally: a success breaks the run
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0)
	fmt.Fprintln(f, toolUse("9", "Bash"))
	fmt.Fprintln(f, toolResult("9", "PASS", false))
	f.Close()
	if got := Health(a, now); got != "" {
		t.Fatalf("after a success: %q", got)
	}

	// different errors, or fewer than ErrorRepeats, aren't a loop
	b, _ := healthJob(t, []string{
		toolUse("a", "Bash"), toolResult("a", "Exit code 1\nerr one", true),
		toolUse("b", "Bash"), toolResult("b", "Exit code 1\nerr two", true),
		toolUse("c", "Read"), toolResult("c", "<tool_use_error>File does not exist.</tool_use_error>", true),
		toolUse("d", "Read"), toolResult("d", "<tool_use_error>File does not exist.</tool_use_error>", true),
	}, now)
	if got := Health(b, now); got != "" {
		t.Fatalf("mixed errors: %q", got)
	}
}

func TestErrorLine(t *testing.T) {
	for in, want := range map[string]string{
		`"Exit code 2\n\nboom"`: "boom",
		`"Exit code 2"`:         "Exit code 2",
		`[{"type":"text","text":"<tool_use_error>nope</tool_use_error>"}]`: "nope",
	} {
		if got := errorLine([]byte(in)); got != want {
			t.Errorf("errorLine(%s) = %q, want %q", in, got, want)
		}
	}
}
