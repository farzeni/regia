package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseQuery(t *testing.T) {
	cases := map[string][]string{
		"":                       nil,
		"   ":                    nil,
		"Hello":                  {"hello"},
		"foo  Bar":               {"foo", "bar"},
		`"exact phrase" x`:       {"exact phrase", "x"},
		`a "unterminated phrase`: {"a", "unterminated phrase"},
		`""`:                     nil,
	}
	for in, want := range cases {
		got := ParseQuery(in).Terms
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("ParseQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegexQuery(t *testing.T) {
	cases := []struct {
		in    string
		terms []string
		res   []string // the patterns, as compiled
	}{
		{`/retr(y|ies)/`, nil, []string{"(?i)retr(y|ies)"}},
		{`webhook /fail\w+/ now`, []string{"webhook", "now"}, []string{`(?i)fail\w+`}},
		{`/a b/`, nil, []string{"(?i)a b"}},     // spaces inside a pattern
		{`/home/me`, []string{"/home/me"}, nil}, // a path is a word
		{`/usr/bin /tmp`, []string{"/usr/bin", "/tmp"}, nil},
		{`/a\/b/`, nil, []string{`(?i)a\/b`}}, // an escaped slash
		{`/retr`, []string{"/retr"}, nil},     // not closed yet: a word
		{`//`, []string{"//"}, nil},
	}
	for _, c := range cases {
		q := ParseQuery(c.in)
		var res []string
		for _, re := range q.Res {
			res = append(res, re.String())
		}
		if strings.Join(q.Terms, "|") != strings.Join(c.terms, "|") || strings.Join(res, "|") != strings.Join(c.res, "|") || q.Err != nil {
			t.Errorf("ParseQuery(%q) = terms %q res %q err %v, want %q %q", c.in, q.Terms, res, q.Err, c.terms, c.res)
		}
	}

	q := ParseQuery(`deploy /retr(y|ies)/`)
	m := Msg{Text: "Deploy RETRIES twice", low: "deploy retries twice"}
	if !q.Match(&m) {
		t.Fatalf("no match")
	}
	if got := q.Spans(m.Text); len(got) != 2 || m.Text[got[1][0]:got[1][1]] != "RETRIES" {
		t.Fatalf("spans %v", got)
	}
	if m2 := (Msg{Text: "deploy retried", low: "deploy retried"}); q.Match(&m2) {
		t.Fatalf("matched without the pattern")
	}
	// a pattern that matches the empty string highlights nothing empty
	if got := ParseQuery(`/x*/`).Spans("abc"); len(got) != 0 {
		t.Fatalf("empty spans %v", got)
	}

	bad := ParseQuery(`/retr(y/`)
	if bad.Err == nil || bad.Empty() || bad.Match(&m) {
		t.Fatalf("bad pattern: err %v empty %v", bad.Err, bad.Empty())
	}
}

func TestSpans(t *testing.T) {
	q := ParseQuery("ab cd")
	s := "xxAB cd abcd"
	got := q.Spans(s)
	// AB, cd, then "abcd" is two touching matches merged into one
	want := [][2]int{{2, 4}, {5, 7}, {8, 12}}
	if len(got) != len(want) {
		t.Fatalf("Spans = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Spans = %v, want %v", got, want)
		}
	}
	// case folding that changes byte length must still point into the original
	s = "Kelvin K and k" // KELVIN SIGN lowercases to a 1-byte k
	for _, sp := range ParseQuery("k").Spans(s) {
		if c := strings.ToLower(s[sp[0]:sp[1]]); c != "k" {
			t.Errorf("span %v covers %q", sp, s[sp[0]:sp[1]])
		}
	}
}

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMessages(t *testing.T) {
	p := writeTranscript(t,
		`{"type":"user","timestamp":"2026-11-15T10:00:00Z","message":{"role":"user","content":"find the bug in search"}}`,
		`{"type":"user","isMeta":true,"message":{"content":"meta stuff"}}`,
		`{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}`,
		`{"type":"assistant","timestamp":"2026-11-15T10:00:05Z","message":{"content":[{"type":"thinking","thinking":"secret"},{"type":"text","text":"Looking at search.go"},{"type":"tool_use","name":"Bash","input":{"command":"grep -n search"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"search.go:1: package"}]}]}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"<pasted_content>long paste</pasted_content> and more"}]}}`,
		`{"type":"summary","summary":"user said search"}`,
	)
	ms := Messages(p)
	type want struct {
		r Role
		s string
	}
	exp := []want{
		{RoleYou, "find the bug in search"},
		{RoleTool, "secret"}, // a thought, kept as a step
		{RoleAgent, "Looking at search.go"},
		{RoleTool, "Bash: grep -n search"},
		{RoleTool, "search.go:1: package"},
		{RoleYou, "<pasted_content>long paste</pasted_content> and more"},
	}
	if len(ms) != len(exp) {
		t.Fatalf("got %d messages: %+v", len(ms), ms)
	}
	for i, e := range exp {
		if ms[i].Role != e.r || ms[i].Text != e.s {
			t.Errorf("msg %d = %v %q, want %v %q", i, ms[i].Role, ms[i].Text, e.r, e.s)
		}
	}
	if ms[0].At.IsZero() {
		t.Error("timestamp not parsed")
	}

	// appended lines are read incrementally; a half-written line waits
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}` + "\n" + `{"type":"assistant","mess`)
	f.Close()
	ms = Messages(p)
	if len(ms) != 7 || ms[6].Text != "done" {
		t.Fatalf("after append: %d messages, last %q", len(ms), ms[len(ms)-1].Text)
	}
}

func TestSearch(t *testing.T) {
	a := writeTranscript(t,
		`{"type":"user","timestamp":"2026-11-15T09:00:00Z","message":{"content":"deploy the server"}}`,
		`{"type":"assistant","timestamp":"2026-11-15T09:01:00Z","message":{"content":[{"type":"text","text":"Server deployed"},{"type":"tool_use","name":"Bash","input":{"command":"ssh server deploy"}}]}}`,
	)
	b := writeTranscript(t,
		`{"type":"user","timestamp":"2026-11-15T11:00:00Z","message":{"content":"is the SERVER up? deploy again"}}`,
	)
	srcs := []Source{{"a", a}, {"b", b}, {"c", ""}}

	res := Search(srcs, ParseQuery("server deploy"), false, 10)
	if len(res) != 2 || res[0].ID != "b" || res[1].ID != "a" {
		t.Fatalf("order: %+v", res)
	}
	if res[1].Total != 2 || res[1].Hits[0].Msg.Role != RoleAgent { // newest first, tool call left out
		t.Fatalf("session a: %+v", res[1])
	}
	if got := Search(srcs, ParseQuery("ssh"), false, 10); len(got) != 0 {
		t.Fatalf("tool output searched without tools: %+v", got)
	}
	if got := Search(srcs, ParseQuery("ssh"), true, 10); len(got) != 1 || got[0].Hits[0].Msg.Role != RoleTool {
		t.Fatalf("tools on: %+v", got)
	}
	if got := Search(srcs, ParseQuery("server"), false, 1); got[1].Total != 2 || len(got[1].Hits) != 1 {
		t.Fatalf("limit: %+v", got[1])
	}
}

// Real data: every listed session (skipped unless REGIA_REAL_SEARCH is set to a query).
func TestSearchRealData(t *testing.T) {
	qs := os.Getenv("REGIA_REAL_SEARCH")
	if qs == "" {
		t.Skip("set REGIA_REAL_SEARCH")
	}
	b, err := FetchAgents()
	if err != nil {
		t.Fatal(err)
	}
	var srcs []Source
	for _, a := range ParseAgents(b) {
		Enrich(a)
		srcs = append(srcs, Source{a.ID, TranscriptPath(a)})
	}
	for _, tools := range []bool{false, true} {
		for round := range 2 {
			t0 := time.Now()
			res := Search(srcs, ParseQuery(qs), tools, 200)
			n := 0
			for _, r := range res {
				n += r.Total
			}
			t.Logf("tools=%v round %d: %d sessions searched, %d hits in %d, %v", tools, round, len(srcs), n, len(res), time.Since(t0))
		}
	}
}

// A session that entered a worktree: its transcript moved to the worktree's
// project folder, and the job file still names the old path.
func TestTranscriptPathMoved(t *testing.T) {
	cd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cd)
	moved := filepath.Join(cd, "projects", "-repo--claude-worktrees-x", "sid-1.jsonl")
	_ = os.MkdirAll(filepath.Dir(moved), 0o755)
	_ = os.WriteFile(moved, []byte("{}\n"), 0o644)
	a := &Agent{SessionID: "sid-1", Transcript: filepath.Join(cd, "projects", "-repo", "sid-1.jsonl")}
	if got := TranscriptPath(a); got != moved {
		t.Fatalf("TranscriptPath = %q, want %q", got, moved)
	}
	a.Transcript = moved
	if got := TranscriptPath(a); got != moved {
		t.Fatalf("existing path: %q", got)
	}
}

// TestQueryCheatSheet: the examples in the search view's regex cheat sheet
// (ctrl+r, internal/ui/regexhelp.go) match what it says they match.
func TestQueryCheatSheet(t *testing.T) {
	cases := []struct {
		query   string
		yes, no []string
	}{
		{"deploy failed", []string{"Failed to DEPLOY"}, []string{"deploy ok"}},
		{`"deploy failed"`, []string{"the Deploy Failed again"}, []string{"failed to deploy"}},
		{"/retr(y|ies)/", []string{"Retry later", "two retries"}, []string{"retro"}},
		{`pr /#\d+/`, []string{"PR #42 merged"}, []string{"PR merged", "#42"}},
		{"/a b/", []string{"a b"}, []string{"ab"}},
		{`/\b[0-9a-f]{8}\b/`, []string{"job 3f2a9c1e done"}, []string{"3f2a9c1e5"}},
		{`/https?:\/\/\S+/`, []string{"see https://x.dev/a"}, []string{"https:"}},
		{`/\w+\.go:\d+/`, []string{"at view.go:212"}, []string{"view.go"}},
		{"/(?-i)TODO/", []string{"a TODO"}, []string{"a todo"}},
		{"/fail(ed|ure)?/", []string{"fail", "failure"}, []string{"fai"}},
		{`/(?m)^ok$/`, []string{"first\nok\nlast"}, []string{"not ok here"}},
		{`/^ok$/`, []string{"OK"}, []string{"first\nok"}},
	}
	for _, c := range cases {
		q := ParseQuery(c.query)
		if q.Err != nil {
			t.Fatalf("%s: %v", c.query, q.Err)
		}
		for _, s := range c.yes {
			if !q.Match(&Msg{Text: s, low: strings.ToLower(s)}) {
				t.Errorf("%s should match %q", c.query, s)
			}
		}
		for _, s := range c.no {
			if q.Match(&Msg{Text: s, low: strings.ToLower(s)}) {
				t.Errorf("%s should not match %q", c.query, s)
			}
		}
	}
}
