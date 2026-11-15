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
		wrapper  bool
	}{
		{`claude --bg --name "x" "$(cat brief.md)" 2>&1 | tail -5`, bgOut, "a12f674f", false},
		// first line cut by tail: falls back to the attach hint
		{`claude --bg -p "do it" | tail -3`, "  claude attach 92d454d4    open\n  claude logs 92d454d4", "92d454d4", false},
		// `| tail -2` keeps only the logs and stop hints
		{`for t in 4 8; do claude --bg --name "f-$t" "$(cat t$t.txt)" 2>&1 | tail -2; done`, "  claude logs a5156a41      show recent output\n  claude stop a5156a41      stop this session\n  claude logs 46b9564c      show recent output\n  claude stop 46b9564c      stop this session", "a5156a41,46b9564c", false},
		// several spawns in one command
		{"cd x && claude --bg a\nclaude --bg b", "backgrounded · 11111111 · a\nbackgrounded · 22222222 · b", "11111111,22222222", false},
		// a wrapper script runs `claude --bg ... | tail -1` (the SKL coordinator's launch.sh)
		{`cd /x/tmp && ./launch.sh impl 152 "title" "" x-s3`, "  claude stop 8db43fb7      stop this session", "8db43fb7", true},
		{`bash launch.sh val 3 t`, bgOut, "a12f674f", true},
		{`./launch.sh a; ./launch.sh b`, "  claude stop 11111111      stop this session\nfailed to determine base repo\n  claude stop 22222222      stop this session", "11111111,22222222", true},
		// not a spawn: the output merely mentions ids, not as claude's own lines
		{`claude agents --json`, `[{"id":"a12f674f","hint":"claude stop a12f674f"}]`, "", false},
		{`grep -- --bg x`, bgOut, "", false},
		{`grep -rn stop x.jsonl`, `x.jsonl:3:{"stdout": "  claude stop 8db43fb7      stop this session"}`, "", false},
		{`./run.sh`, "  claude logs a5156a41      show recent output", "", false},
		// another session's output replayed: its spawns aren't ours
		{`claude logs f9170743 | tail -20`, bgOut, "", false},
		{`claude attach f9170743`, bgOut, "", false},
		{`cat /tmp/out.log`, bgOut, "", false},
		{`cd x && tail -5 out.log`, bgOut, "", false},
		// the session reads its own background task's output (the spawn ran with run_in_background)
		{`cd /x; cat /tmp/claude-1000/-x/f9170743-0cfe-4634/tasks/bsah6im8i.output; claude rm b318d6c0`, "removed b318d6c0\n  claude stop 5ba220b4      stop this session", "5ba220b4", true},
		{`cat /tmp/claude-1000/-x/aaaaaaaa-0000-0000/tasks/b1.output`, bgOut, "", false}, // another session's task
		// writing the brief first doesn't hide the spawn
		{"D=/x; cat > $D/x63 <<'EOF'\nnotes\nEOF\n./launch.sh impl 63 t \"\" $D/x63 2>&1 | tail -1", "  claude stop 0653437d      stop this session", "0653437d", true},
	}
	for _, c := range cases {
		ids, wrapper := spawnedIDs(c.cmd, c.out, "f9170743-0cfe-4634")
		if got := strings.Join(ids, ","); got != c.want || (got != "" && wrapper != c.wrapper) {
			t.Errorf("spawnedIDs(%q)\n got %q wrapper=%v\nwant %q wrapper=%v", c.cmd, got, wrapper, c.want, c.wrapper)
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

// A spawn through a wrapper script links like a direct one, but never takes
// over a link already recorded (the same footer read again later is not a spawn).
func TestHookSpawnThroughWrapper(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	hook := func(session, job, cmd, out string) {
		HandleHook(strings.NewReader(`{"session_id":"`+session+`-1111-2222-3333-444444444444","tool_name":"Bash",
		"tool_input":{"command":`+jsonStr(cmd)+`},"tool_response":{"stdout":`+jsonStr(out)+`,"stderr":""}}`), job)
	}
	foot := "  claude stop 8db43fb7      stop this session"
	hook("f9170743", "/home/u/.claude/jobs/f9170743", `cd /x && ./launch.sh impl 152 "t" "" x-s3`, foot)
	l := loadSpawns()["8db43fb7"]
	if l.Parent != "f9170743" || !l.Wrapper || l.Source != "hook" {
		t.Fatalf("link = %+v", l)
	}
	hook("aaaaaaaa", "/home/u/.claude/jobs/aaaaaaaa", `./show.sh`, foot) // not written: already linked
	if got := loadSpawns()["8db43fb7"].Parent; got != "f9170743" {
		t.Fatalf("wrapper link replaced the first one: parent %s", got)
	}
	_ = appendSpawns([]Link{{Parent: "bbbbbbbb", Child: "8db43fb7", Wrapper: true}}) // written by hand: still ignored on load
	if got := loadSpawns()["8db43fb7"].Parent; got != "f9170743" {
		t.Fatalf("loaded wrapper link replaced the first one: parent %s", got)
	}
	hook("cccccccc", "/home/u/.claude/jobs/cccccccc", `claude --bg --resume 8db43fb7 x`, foot) // a direct spawn still wins
	if got := loadSpawns()["8db43fb7"].Parent; got != "cccccccc" {
		t.Fatalf("direct link didn't win: parent %s", got)
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
		// spawn through a wrapper script
		`{"sessionId":"c0ffee00-aaaa","message":{"content":[{"type":"tool_use","id":"t3","name":"Bash","input":{"command":"cd tmp && ./launch.sh impl 152 \"t\""}}]}}`,
		`{"sessionId":"c0ffee00-aaaa","toolUseResult":{"stdout":"  claude stop 8db43fb7      stop this session"},"message":{"content":[{"type":"tool_result","tool_use_id":"t3","content":"..."}]}}`,
		// an ordinary command: kept until its result, no link
		`{"sessionId":"c0ffee00-aaaa","message":{"content":[{"type":"tool_use","id":"t4","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"sessionId":"c0ffee00-aaaa","toolUseResult":{"stdout":"a b"},"message":{"content":[{"type":"tool_result","tool_use_id":"t4","content":"a b"}]}}`,
	}
	_ = os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	got := scanTranscripts(dir)
	if len(got) != 2 || got[0].Parent != "c0ffee00" || got[0].Child != "a12f674f" || got[0].Wrapper ||
		got[1].Child != "8db43fb7" || !got[1].Wrapper {
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
