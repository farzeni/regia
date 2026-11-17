package spawns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeJobs makes JobCreated answer from a map for the test.
func fakeJobs(t *testing.T, created map[string]time.Time) {
	old := JobCreated
	JobCreated = func(id string) (time.Time, bool) { c, ok := created[id]; return c, ok }
	t.Cleanup(func() { JobCreated = old })
}

// When the child's job file says when it was created, that decides, not the
// shape of the command: a coordinator's launch with grep/head steps counts,
// an old session printed by a `claude --bg` command doesn't.
func TestSpawnedIDsByCreation(t *testing.T) {
	at := time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC)
	fakeJobs(t, map[string]time.Time{
		"8db43fb7": at.Add(-30 * time.Second),   // started by this command
		"11111111": at.Add(-3 * 24 * time.Hour), // an old session
	})
	foot := "  claude stop 8db43fb7      stop this session"
	cases := []struct {
		cmd, out string
		at       time.Time
		want     string
	}{
		// the guess would refuse it (grep, head steps); the job file says new
		{`grep -n "round" $P | head; swarm.sh launch x`, foot, at, "8db43fb7"},
		{`cat /tmp/out.log`, foot, at, "8db43fb7"},
		// old session: never a spawn, even in a `claude --bg` command
		{`claude --bg x; cat old.log`, "backgrounded · 11111111 · x", at, ""},
		{`./launch.sh`, "  claude stop 11111111      stop this session", at, ""},
		// time of the call unknown: back to the guess
		{`grep x y; swarm.sh launch x`, foot, time.Time{}, ""},
		// job file gone: back to the guess
		{`./launch.sh`, "  claude stop 22222222      stop this session", at, "22222222"},
		{`cat x.log`, "  claude stop 22222222      stop this session", at, ""},
	}
	for _, c := range cases {
		ids, _ := spawnedIDs(c.cmd, c.out, "f9170743-0cfe-4634", c.at)
		if got := strings.Join(ids, ","); got != c.want {
			t.Errorf("spawnedIDs(%q, at=%v) = %q, want %q", c.cmd, c.at, got, c.want)
		}
	}
}

// Heredoc bodies and quoted prompts aren't steps: "cat this; grep that" in a
// brief doesn't hide the launch after it.
func TestCommandText(t *testing.T) {
	fakeJobs(t, nil)
	foot := "  claude stop 0653437d      stop this session"
	for _, cmd := range []string{
		"cat > /x/brief <<'EOF'\nfirst cat this; grep that\nhead -5 x\nEOF\n./launch.sh impl 63 /x/brief | tail -1",
		"cat > b <<EOF\ncat x\nEOF\ncat > c <<-\"END\"\n\tgrep y\n\tEND\n./launch.sh b c",
		`swarm.sh launch w "read it: cat notes.md; grep TODO src" | tail -1`,
		`swarm.sh launch w 'cat a; head b' | tail -1`,
	} {
		if ids, _ := spawnedIDs(cmd, foot, "s", time.Time{}); strings.Join(ids, ",") != "0653437d" {
			t.Errorf("hidden launch in %q: %v", cmd, ids)
		}
	}
	// a real viewer step outside the heredoc still counts as one
	if ids, _ := spawnedIDs("cat > b <<EOF\nx\nEOF\ncat old.log", foot, "s", time.Time{}); len(ids) != 0 {
		t.Errorf("cat after a heredoc: %v", ids)
	}
	if got := commandText(`a "b; c \" d" 'e; f' g`); got != `a "" '' g` {
		t.Errorf("dropQuoted = %q", got)
	}
}

// The scanner reads only what was appended, keeps a Bash call whose result
// comes in a later scan, leaves an unfinished line for later, and resumes
// from its saved offsets after a restart.
func TestScannerIncremental(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fakeJobs(t, nil)
	dir := t.TempDir()
	p := filepath.Join(dir, "projects", "-home-u", "s.jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	state := filepath.Join(t.TempDir(), "scan.json")
	call := `{"sessionId":"c0ffee00-aaaa","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"./launch.sh w"}}]}}` + "\n"
	result := `{"sessionId":"c0ffee00-aaaa","timestamp":"2026-11-17T10:00:00.5Z","toolUseResult":{"stdout":"  claude stop 8db43fb7      stop this session"},"message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"..."}]}}` + "\n"
	app := func(s string) {
		f, _ := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		_, _ = f.WriteString(s)
		_ = f.Close()
	}
	s := &Scanner{Dir: dir, State: state}
	app(call)
	if n, err := s.Scan(); n != 0 || err != nil {
		t.Fatalf("call alone: %d %v", n, err)
	}
	app(result[:40]) // the result is still being written
	if n, _ := s.Scan(); n != 0 {
		t.Fatalf("half a line linked: %d", n)
	}
	// restart: a new scanner resumes from the saved offset and pending call
	s = &Scanner{Dir: dir, State: state}
	app(result[40:])
	if n, _ := s.Scan(); n != 1 {
		t.Fatalf("want 1 link once the result is complete, got %d", n)
	}
	if l := loadSpawns()["8db43fb7"]; l.Parent != "c0ffee00" || l.Source != "transcript" {
		t.Fatalf("link = %+v", l)
	}
	if n, _ := s.Scan(); n != 0 {
		t.Fatalf("nothing new, still %d", n)
	}
	// rewritten shorter: read again from the start, known links not repeated
	_ = os.WriteFile(p, []byte(call), 0o644)
	if n, _ := s.Scan(); n != 0 {
		t.Fatalf("rewritten file: %d", n)
	}
	app(result)
	if n, _ := (&Scanner{Dir: dir, State: state}).Scan(); n != 0 {
		t.Fatalf("known link appended again: %d", n)
	}
	if b, _ := os.ReadFile(Path()); strings.Count(string(b), "8db43fb7") != 1 {
		t.Fatalf("spawns.jsonl:\n%s", b)
	}
}

// Bash calls waiting for a result are bounded: results never seen (calls
// interrupted) don't pile up in the saved state.
func TestPendingBounded(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	var b strings.Builder
	for i := range 500 {
		b.WriteString(`{"sessionId":"c0ffee00","message":{"content":[{"type":"tool_use","id":"t` + string(rune('a'+i%26)) + strings.Repeat("x", i) + `","name":"Bash","input":{"command":"ls"}}]}}` + "\n")
	}
	_ = os.WriteFile(p, []byte(b.String()), 0o644)
	_, end, pending := scanFrom(p, 0, nil, false)
	if len(pending) != maxPending || end != int64(b.Len()) {
		t.Fatalf("pending %d end %d of %d", len(pending), end, b.Len())
	}
}
