// Package spawns records which Claude Code session started which. Claude Code
// doesn't record it (forkParentSessionId only covers real forks, and
// `claude --bg` hands the new session to a daemon, so no env var carries
// over), so regia learns it two ways:
//
//   - live: the regia plugin's PostToolUse hook on Bash runs `regia hook spawn`
//     in every session; when a command printed "backgrounded · <id>" it appends
//     {parent, child} to ~/.config/regia/spawns.jsonl. The command may be
//     `claude --bg` itself or a wrapper script that runs it (then the output
//     must carry claude's own banner or footer line, see spawnedIDs).
//   - backfill: `regia setup` scans existing transcripts for the same thing.
//
// The coordinator needs to know nothing about regia.
package spawns

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/claude"
)

type Link struct {
	Parent        string `json:"parent"`                  // short id of the spawning session (job id when known)
	ParentSession string `json:"parentSession,omitempty"` // full session id
	Child         string `json:"child"`                   // short id printed by `claude --bg`
	At            string `json:"at,omitempty"`
	Source        string `json:"source,omitempty"`  // hook | transcript
	Wrapper       bool   `json:"wrapper,omitempty"` // the command didn't say `claude --bg` itself (a script did): weaker, never overrides a link
}

func Path() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "regia", "spawns.jsonl")
}

var (
	reBgCmd     = regexp.MustCompile(`claude\b[^\n|;&]*\s--bg\b`)
	reBgOut     = regexp.MustCompile(`backgrounded · ([0-9a-f]{8})\b`)
	reAttachOut = regexp.MustCompile(`claude (?:attach|logs|stop|rm) ([0-9a-f]{8})\b`)

	// For commands that don't say `claude --bg` (a wrapper script runs it),
	// only claude's own lines count, whole: the banner, or the footer's stop
	// hint (what `| tail -1` keeps).
	reBgBanner = regexp.MustCompile(`(?m)^\s*backgrounded · ([0-9a-f]{8})(?: ·|\s*$)`)
	reBgFooter = regexp.MustCompile(`(?m)^\s*claude stop ([0-9a-f]{8})\s+stop this session\s*$`)
	// Commands that replay another session's output, which can hold that
	// session's own spawns.
	reShowsOutput = regexp.MustCompile(`\bclaude\s+(?:logs|attach)\b`)
	reSegment     = regexp.MustCompile(`&&|\|\||;|\n`)
	viewers       = map[string]bool{"cat": true, "grep": true, "rg": true, "head": true, "tail": true, "less": true, "more": true, "sed": true, "awk": true, "jq": true, "bat": true, "zcat": true}
)

// showsOutput reports whether cmd may print text recorded elsewhere: it runs
// `claude logs|attach`, or one of its steps (not a pipe filter) starts with a
// file viewer that isn't writing a file (`cat > brief.md <<EOF` is fine) or
// reading the session's own background task output (.../<session>/tasks/*,
// where a spawn run with run_in_background prints its banner).
func showsOutput(cmd, session string) bool {
	if reShowsOutput.MatchString(cmd) {
		return true
	}
	ownTasks := "\x00"
	if session != "" {
		ownTasks = "/" + session + "/tasks/"
	}
	for _, seg := range reSegment.Split(cmd, -1) {
		head := strings.SplitN(seg, "|", 2)[0]
		if f := strings.Fields(seg); len(f) > 0 && viewers[f[0]] && !strings.ContainsAny(head, "><") && !strings.Contains(head, ownTasks) {
			return true
		}
	}
	return false
}

// spawnedIDs returns the session ids a Bash command started, from its output,
// and whether it went through a wrapper. When the command looks like
// `claude ... --bg`, the ids come from the "backgrounded · <id>" line, or the
// "claude attach|logs|stop|rm <id>" hints when the first lines were cut (e.g.
// `| tail -2` keeps only logs and stop). Any other command (a script such as
// `./launch.sh impl 152 ...` that runs `claude --bg` inside) counts only by
// whole banner or "claude stop <id>   stop this session" lines, and never when
// it may be printing recorded text instead (see showsOutput).
func spawnedIDs(cmd, out, session string) (ids []string, wrapper bool) {
	res := []*regexp.Regexp{reBgOut, reAttachOut}
	if !reBgCmd.MatchString(cmd) {
		if showsOutput(cmd, session) {
			return nil, false
		}
		res, wrapper = []*regexp.Regexp{reBgBanner, reBgFooter}, true
	}
	seen := map[string]bool{}
	for _, re := range res {
		for _, m := range re.FindAllStringSubmatch(out, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				ids = append(ids, m[1])
			}
		}
	}
	return ids, wrapper
}

// mayBeSpawn is a cheap byte check before parsing a hook payload or a
// transcript line.
func mayBeSpawn(b []byte) bool {
	return bytes.Contains(b, []byte("--bg")) || bytes.Contains(b, []byte("backgrounded · ")) || bytes.Contains(b, []byte("stop this session"))
}

// toolOutput flattens a Bash tool_response (object with stdout/stderr, or a
// plain string) into text.
func toolOutput(raw json.RawMessage) string {
	var o struct{ Stdout, Stderr string }
	if json.Unmarshal(raw, &o) == nil && (o.Stdout != "" || o.Stderr != "") {
		return o.Stdout + "\n" + o.Stderr
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func short8(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// HandleHook handles the PostToolUse hook payload on stdin. It never fails
// loudly: a hook error would show up in every session.
func HandleHook(stdin io.Reader, jobDir string) {
	var in struct {
		SessionID    string                   `json:"session_id"`
		ToolName     string                   `json:"tool_name"`
		ToolInput    struct{ Command string } `json:"tool_input"`
		ToolResponse json.RawMessage          `json:"tool_response"`
	}
	b, err := io.ReadAll(io.LimitReader(stdin, 32<<20))
	if err != nil || !mayBeSpawn(b) {
		return
	}
	if json.Unmarshal(b, &in) != nil || (in.ToolName != "" && in.ToolName != "Bash") {
		return
	}
	ids, wrapper := spawnedIDs(in.ToolInput.Command, toolOutput(in.ToolResponse), in.SessionID)
	if len(ids) == 0 {
		return
	}
	parent := short8(in.SessionID)
	if jobDir != "" { // the hook runs inside the session: its job id is what `claude agents` lists
		parent = filepath.Base(jobDir)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var known map[string]Link
	if wrapper {
		known = loadSpawns()
	}
	var links []Link
	for _, id := range ids {
		if _, linked := known[id]; id != parent && !linked {
			links = append(links, Link{Parent: parent, ParentSession: in.SessionID, Child: id, At: now, Source: "hook", Wrapper: wrapper})
		}
	}
	_ = appendSpawns(links)
}

func appendSpawns(links []Link) error {
	if len(links) == 0 {
		return nil
	}
	p := Path()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	var buf bytes.Buffer
	for _, l := range links {
		j, _ := json.Marshal(l)
		buf.Write(j)
		buf.WriteByte('\n')
	}
	_, err = f.Write(buf.Bytes()) // one write: O_APPEND keeps concurrent hooks from interleaving
	return err
}

// loadSpawns returns child -> link; later lines win, except that a wrapper
// link never replaces one already there.
func loadSpawns() map[string]Link {
	out := map[string]Link{}
	f, err := os.Open(Path())
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var l Link
		if json.Unmarshal(sc.Bytes(), &l) == nil && l.Child != "" && l.Parent != "" {
			if _, had := out[l.Child]; had && l.Wrapper {
				continue
			}
			out[l.Child] = l
		}
	}
	return out
}

var spawnCache struct {
	mtime time.Time
	size  int64
	links map[string]Link
}

// Cached re-reads spawns.jsonl only when it changed.
func Cached() map[string]Link {
	fi, err := os.Stat(Path())
	if err != nil {
		return nil
	}
	if spawnCache.links == nil || !fi.ModTime().Equal(spawnCache.mtime) || fi.Size() != spawnCache.size {
		spawnCache.links, spawnCache.mtime, spawnCache.size = loadSpawns(), fi.ModTime(), fi.Size()
	}
	return spawnCache.links
}

// scanTranscripts finds past `claude --bg` spawns in session transcripts
// under <claudeDir>/projects. Read-only; lines without a hint are skipped
// without being parsed.
func scanTranscripts(dir string) []Link {
	files, _ := filepath.Glob(filepath.Join(dir, "projects", "*", "*.jsonl"))
	var links []Link
	for _, f := range files {
		links = append(links, scanTranscript(f)...)
	}
	return links
}

func scanTranscript(path string) []Link {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	type entry struct {
		SessionID string          `json:"sessionId"`
		Timestamp string          `json:"timestamp"`
		Result    json.RawMessage `json:"toolUseResult"`
		Message   struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	type block struct {
		Type      string                   `json:"type"`
		ID        string                   `json:"id"`
		Name      string                   `json:"name"`
		ToolUseID string                   `json:"tool_use_id"`
		IsError   bool                     `json:"is_error"`
		Input     struct{ Command string } `json:"input"`
	}
	cmds := map[string]string{} // tool_use id -> command
	seen := map[string]bool{}
	var links []Link
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		// Bash calls are kept (a wrapper's command doesn't say --bg), and
		// results are parsed only when they may hold a spawn.
		if len(line) > 0 && (bytes.Contains(line, []byte(`"name":"Bash"`)) || len(cmds) > 0 && bytes.Contains(line, []byte("tool_result")) && mayBeSpawn(line)) {
			var e entry
			var blocks []block
			if json.Unmarshal(line, &e) == nil && json.Unmarshal(e.Message.Content, &blocks) == nil {
				for _, b := range blocks {
					switch {
					case b.Type == "tool_use" && b.Name == "Bash":
						cmds[b.ID] = b.Input.Command
					case b.Type == "tool_result" && cmds[b.ToolUseID] != "" && !b.IsError:
						ids, wrapper := spawnedIDs(cmds[b.ToolUseID], toolOutput(e.Result), e.SessionID)
						for _, id := range ids {
							p := short8(e.SessionID)
							if id != p && !seen[id] {
								seen[id] = true
								links = append(links, Link{Parent: p, ParentSession: e.SessionID, Child: id, At: e.Timestamp, Source: "transcript", Wrapper: wrapper})
							}
						}
						delete(cmds, b.ToolUseID)
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	return links
}

// Backfill appends transcript links for children not yet known. Returns how
// many were added.
func Backfill() (int, error) {
	known := loadSpawns()
	var add []Link
	for _, l := range scanTranscripts(claude.Dir()) {
		if _, ok := known[l.Child]; !ok {
			known[l.Child] = l
			add = append(add, l)
		}
	}
	return len(add), appendSpawns(add)
}
