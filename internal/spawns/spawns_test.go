package spawns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farzeni/regia/internal/claude"
)

// Real `claude --bg` output, as recorded in a transcript (2.1.3xx).
const bgOut = "backgrounded · a12f674f · skl/docs/skillsets\n  claude agents             list sessions\n  claude attach a12f674f    open in this terminal\n  claude logs a12f674f      show recent output\n  claude stop a12f674f      stop this session"

func TestSpawnedIDs(t *testing.T) {
	cases := []struct {
		cmd, out string
		want     string
	}{
		{`claude --bg --name "x" "$(cat brief.md)" 2>&1 | tail -5`, bgOut, "a12f674f"},
		// first line cut by tail: falls back to the attach hint
		{`claude --bg -p "do it" | tail -3`, "  claude attach 92d454d4    open\n  claude logs 92d454d4", "92d454d4"},
		// several spawns in one command
		{"cd x && claude --bg a\nclaude --bg b", "backgrounded · 11111111 · a\nbackgrounded · 22222222 · b", "11111111,22222222"},
		// not a spawn: the output merely mentions ids
		{`cat README.md`, bgOut, ""},
		{`claude agents --json`, bgOut, ""},
		{`grep -- --bg x`, bgOut, ""},
	}
	for _, c := range cases {
		if got := strings.Join(spawnedIDs(c.cmd, c.out), ","); got != c.want {
			t.Errorf("spawnedIDs(%q)\n got %q\nwant %q", c.cmd, got, c.want)
		}
	}
}

func TestHookSpawnWritesLink(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	payload := `{"session_id":"f9170743-1111-2222-3333-444444444444","hook_event_name":"PostToolUse","tool_name":"Bash",
	"tool_input":{"command":"claude --bg --name \"skl/docs/skillsets\" \"brief\" 2>&1 | tail -5"},
	"tool_response":{"stdout":` + jsonStr(bgOut) + `,"stderr":"","interrupted":false}}`
	HandleHook(strings.NewReader(payload), "/home/u/.claude/jobs/f9170743")
	l := loadSpawns()["a12f674f"]
	if l.Parent != "f9170743" || l.ParentSession == "" || l.Source != "hook" {
		t.Fatalf("link = %+v", l)
	}
	// other tools and non-spawn commands write nothing
	HandleHook(strings.NewReader(`{"session_id":"x","tool_name":"Bash","tool_input":{"command":"ls --bg"},"tool_response":{"stdout":"ok"}}`), "")
	HandleHook(strings.NewReader(`garbage --bg`), "")
	if n := len(loadSpawns()); n != 1 {
		t.Fatalf("want 1 link, got %d", n)
	}
}

func jsonStr(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

func TestScanTranscript(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "projects", "-home-u", "s.jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	lines := []string{
		`{"sessionId":"c0ffee00-aaaa","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"claude --bg --name w \"go\" | tail -5"}}]}}`,
		`{"sessionId":"c0ffee00-aaaa","timestamp":"2026-11-14T10:00:00Z","toolUseResult":{"stdout":` + jsonStr(bgOut) + `},"message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"..."}]}}`,
		// denied spawn: no link
		`{"sessionId":"c0ffee00-aaaa","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"claude --bg x"}}]}}`,
		`{"sessionId":"c0ffee00-aaaa","toolUseResult":"Error: denied","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"denied"}]}}`,
	}
	_ = os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	got := scanTranscripts(dir)
	if len(got) != 1 || got[0].Parent != "c0ffee00" || got[0].Child != "a12f674f" {
		t.Fatalf("scan = %+v", got)
	}
}

// Replays the user's real transcripts read-only: every link found must point
// from a real session to an 8-hex id. Skipped where there are none.
func TestScanRealTranscripts(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	links := scanTranscripts(claude.Dir())
	if len(links) == 0 {
		t.Skip("no real transcripts with spawns")
	}
	for _, l := range links {
		if len(l.Child) != 8 || len(l.Parent) != 8 || l.Child == l.Parent {
			t.Errorf("bad link %+v", l)
		}
	}
	t.Logf("%d real spawn links", len(links))
}
