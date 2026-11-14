// Package spawns records which Claude Code session started which. Claude Code
// doesn't record it (forkParentSessionId only covers real forks, and
// `claude --bg` hands the new session to a daemon, so no env var carries
// over), so regia learns it two ways:
//
//   - live: the regia plugin's PostToolUse hook on Bash runs `regia hook spawn`
//     in every session; when a command printed "backgrounded · <id>" it appends
//     {parent, child} to ~/.config/regia/spawns.jsonl.
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
	"time"

	"github.com/farzeni/regia/internal/claude"
)

type Link struct {
	Parent        string `json:"parent"`                  // short id of the spawning session (job id when known)
	ParentSession string `json:"parentSession,omitempty"` // full session id
	Child         string `json:"child"`                   // short id printed by `claude --bg`
	At            string `json:"at,omitempty"`
	Source        string `json:"source,omitempty"` // hook | transcript
}

func Path() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "regia", "spawns.jsonl")
}

var (
	reBgCmd     = regexp.MustCompile(`claude\b[^\n|;&]*\s--bg\b`)
	reBgOut     = regexp.MustCompile(`backgrounded · ([0-9a-f]{8})\b`)
	reAttachOut = regexp.MustCompile(`claude attach ([0-9a-f]{8})\b`)
)

// spawnedIDs returns the session ids a Bash command started, from its output.
// The command must look like `claude ... --bg`; the ids come from the
// "backgrounded · <id>" line, or the "claude attach <id>" hint when the first
// line was cut (e.g. `| tail -3`).
func spawnedIDs(cmd, out string) []string {
	if !reBgCmd.MatchString(cmd) {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, re := range []*regexp.Regexp{reBgOut, reAttachOut} {
		for _, m := range re.FindAllStringSubmatch(out, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				ids = append(ids, m[1])
			}
		}
	}
	return ids
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
	if err != nil || !bytes.Contains(b, []byte("--bg")) {
		return
	}
	if json.Unmarshal(b, &in) != nil || (in.ToolName != "" && in.ToolName != "Bash") {
		return
	}
	ids := spawnedIDs(in.ToolInput.Command, toolOutput(in.ToolResponse))
	if len(ids) == 0 {
		return
	}
	parent := short8(in.SessionID)
	if jobDir != "" { // the hook runs inside the session: its job id is what `claude agents` lists
		parent = filepath.Base(jobDir)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var links []Link
	for _, id := range ids {
		if id != parent {
			links = append(links, Link{Parent: parent, ParentSession: in.SessionID, Child: id, At: now, Source: "hook"})
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

// loadSpawns returns child -> link; later lines win.
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
		if len(line) > 0 && (bytes.Contains(line, []byte("--bg")) || len(cmds) > 0 && bytes.Contains(line, []byte("tool_result"))) {
			var e entry
			var blocks []block
			if json.Unmarshal(line, &e) == nil && json.Unmarshal(e.Message.Content, &blocks) == nil {
				for _, b := range blocks {
					switch {
					case b.Type == "tool_use" && b.Name == "Bash" && reBgCmd.MatchString(b.Input.Command):
						cmds[b.ID] = b.Input.Command
					case b.Type == "tool_result" && cmds[b.ToolUseID] != "" && !b.IsError:
						for _, id := range spawnedIDs(cmds[b.ToolUseID], toolOutput(e.Result)) {
							p := short8(e.SessionID)
							if id != p && !seen[id] {
								seen[id] = true
								links = append(links, Link{Parent: p, ParentSession: e.SessionID, Child: id, At: e.Timestamp, Source: "transcript"})
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
