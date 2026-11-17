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
//   - while regia runs: a Scanner reads what transcripts appended since its
//     last scan, for sessions whose hook never runs (started before the
//     plugin was installed, they never load its hooks).
//
// A session counts as spawned by a command when its job file says it was
// created while the command ran; only when that can't be told (job file
// gone) does the shape of the command decide (see spawnedIDs).
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
	cmd = commandText(cmd)
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
//
// at is when the command ran (zero: unknown). When the child's job file is
// still there, its creation time decides instead of guessing from the
// command: a session created while the command ran was started by it, one
// created long before was only printed by it (see fresh).
func spawnedIDs(cmd, out, session string, at time.Time) (ids []string, wrapper bool) {
	res := []*regexp.Regexp{reBgOut, reAttachOut}
	guessOK := true
	if !reBgCmd.MatchString(commandText(cmd)) {
		guessOK = !showsOutput(cmd, session)
		res, wrapper = []*regexp.Regexp{reBgBanner, reBgFooter}, true
	}
	seen := map[string]bool{}
	for _, re := range res {
		for _, m := range re.FindAllStringSubmatch(out, -1) {
			id := m[1]
			if seen[id] {
				continue
			}
			seen[id] = true
			if ok, known := fresh(id, at); known && ok || !known && guessOK {
				ids = append(ids, id)
			}
		}
	}
	return ids, wrapper
}

// spawnWindow is how long before the end of a command a session it started
// may have been created (a Bash call runs for 10 minutes at most), and slack
// after it for clocks and late writes.
const (
	spawnWindow = 11 * time.Minute
	spawnSlack  = 2 * time.Minute
)

// fresh says whether session id was created while a command ending at at
// ran. known is false when that can't be told: at is unknown, or the job
// file is gone (an old session since removed) or has no creation time.
func fresh(id string, at time.Time) (ok, known bool) {
	if at.IsZero() {
		return false, false
	}
	c, found := JobCreated(id)
	if !found {
		return false, false
	}
	return !c.Before(at.Add(-spawnWindow)) && !c.After(at.Add(spawnSlack)), true
}

// JobCreated reads when a background session was created, from its job
// file (~/.claude/jobs/<id>/state.json). A variable for tests.
var JobCreated = func(id string) (time.Time, bool) {
	b, err := os.ReadFile(filepath.Join(claude.Dir(), "jobs", id, "state.json"))
	if err != nil {
		return time.Time{}, false
	}
	var j struct {
		CreatedAt time.Time `json:"createdAt"`
	}
	if json.Unmarshal(b, &j) != nil || j.CreatedAt.IsZero() {
		return time.Time{}, false
	}
	return j.CreatedAt, true
}

var reHeredoc = regexp.MustCompile(`<<-?\s*(?:'([^']+)'|"([^"]+)"|\\?([A-Za-z_][A-Za-z0-9_]*))`)

// commandText is the part of a command that runs as steps: heredoc bodies
// and quoted strings (a brief, a prompt) are dropped, so a "cat this; grep
// that" line inside a prompt isn't taken for a step.
func commandText(cmd string) string {
	var b strings.Builder
	var ends []string // heredoc terminators still to come
	for _, line := range strings.SplitAfter(cmd, "\n") {
		if len(ends) > 0 {
			if strings.TrimSpace(line) == ends[0] {
				ends = ends[1:]
			}
			continue
		}
		for _, m := range reHeredoc.FindAllStringSubmatch(line, -1) {
			ends = append(ends, m[1]+m[2]+m[3])
		}
		b.WriteString(line)
	}
	return dropQuoted(b.String())
}

// dropQuoted empties '…' and "…" strings, keeping the quotes.
func dropQuoted(s string) string {
	var b strings.Builder
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == 0:
			if c == '\'' || c == '"' {
				q = c
			}
			b.WriteByte(c)
		case q == '"' && c == '\\' && i+1 < len(s):
			i++
		case c == q:
			q = 0
			b.WriteByte(c)
		}
	}
	return b.String()
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
	ids, wrapper := spawnedIDs(in.ToolInput.Command, toolOutput(in.ToolResponse), in.SessionID, time.Now())
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
	links, _, _ := scanFrom(path, 0, nil, true)
	return links
}

// Pending is a Bash call whose result hasn't been read yet.
type Pending struct {
	ID  string `json:"id"`
	Cmd string `json:"cmd"`
}

// maxPending bounds the Bash calls kept while waiting for their results: a
// result follows its call within a few lines (parallel calls at most), so
// only the latest ones matter.
const maxPending = 64

// scanFrom reads a transcript from offset off and returns the spawn links
// found and the offset to resume from. pending carries Bash calls seen
// before off whose results come after; the returned slice is the new one.
// With partial false a last line without its newline (still being written)
// is left for the next scan.
func scanFrom(path string, off int64, pending []Pending, partial bool) ([]Link, int64, []Pending) {
	f, err := os.Open(path)
	if err != nil {
		return nil, off, pending
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, off, pending
	}
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
	cmd := func(id string) (string, int) {
		for i, p := range pending {
			if p.ID == id {
				return p.Cmd, i
			}
		}
		return "", -1
	}
	seen := map[string]bool{}
	var links []Link
	end := off
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && !partial {
			break // an unfinished line: read it whole next time
		}
		end += int64(len(line))
		// Bash calls are kept (a wrapper's command doesn't say --bg), and
		// results are parsed only when they may hold a spawn.
		if len(line) > 0 && (bytes.Contains(line, []byte(`"name":"Bash"`)) || len(pending) > 0 && bytes.Contains(line, []byte("tool_result")) && mayBeSpawn(line)) {
			var e entry
			var blocks []block
			if json.Unmarshal(line, &e) == nil && json.Unmarshal(e.Message.Content, &blocks) == nil {
				for _, b := range blocks {
					switch {
					case b.Type == "tool_use" && b.Name == "Bash":
						pending = append(pending, Pending{b.ID, b.Input.Command})
						if len(pending) > maxPending {
							pending = pending[len(pending)-maxPending:]
						}
					case b.Type == "tool_result" && !b.IsError:
						c, i := cmd(b.ToolUseID)
						if i < 0 {
							continue
						}
						at, _ := time.Parse(time.RFC3339, e.Timestamp)
						ids, wrapper := spawnedIDs(c, toolOutput(e.Result), e.SessionID, at)
						for _, id := range ids {
							p := short8(e.SessionID)
							if id != p && !seen[id] {
								seen[id] = true
								links = append(links, Link{Parent: p, ParentSession: e.SessionID, Child: id, At: e.Timestamp, Source: "transcript", Wrapper: wrapper})
							}
						}
						pending = append(pending[:i:i], pending[i+1:]...)
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	return links, end, pending
}

// Scanner finds spawns in transcripts as they grow, reading only what was
// appended since its last scan; its offsets are saved, so a restart doesn't
// read everything again. It covers sessions whose hook never ran (a process
// started before the plugin was installed doesn't load its hooks). Not safe
// for concurrent use: run one Scan at a time.
type Scanner struct {
	Dir   string // Claude's config dir (transcripts under projects/)
	State string // where offsets are saved
	files map[string]*scanFile
}

type scanFile struct {
	Off     int64     `json:"off"`
	Pending []Pending `json:"pending,omitempty"`
}

func NewScanner() *Scanner {
	d, _ := os.UserCacheDir()
	return &Scanner{Dir: claude.Dir(), State: filepath.Join(d, "regia", "spawnscan.json")}
}

// Scan reads what was appended to every transcript and appends the links
// for children not known yet to spawns.jsonl. Returns how many it added.
// The first scan reads everything (seconds with hundreds of MB).
func (s *Scanner) Scan() (int, error) {
	if s.files == nil {
		s.files = map[string]*scanFile{}
		if b, err := os.ReadFile(s.State); err == nil {
			_ = json.Unmarshal(b, &s.files)
		}
	}
	paths, _ := filepath.Glob(filepath.Join(s.Dir, "projects", "*", "*.jsonl"))
	present := map[string]bool{}
	changed := false
	var found []Link
	for _, p := range paths {
		present[p] = true
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		st := s.files[p]
		if st == nil {
			st = &scanFile{}
			s.files[p] = st
			changed = true
		}
		if fi.Size() < st.Off { // rewritten: start over
			st.Off, st.Pending = 0, nil
		}
		if fi.Size() == st.Off {
			continue
		}
		var links []Link
		var end int64
		links, end, st.Pending = scanFrom(p, st.Off, st.Pending, false)
		if end != st.Off {
			st.Off, changed = end, true
		}
		found = append(found, links...)
	}
	for p := range s.files {
		if !present[p] {
			delete(s.files, p)
			changed = true
		}
	}
	if changed {
		s.save()
	}
	if len(found) == 0 {
		return 0, nil
	}
	known := loadSpawns()
	var add []Link
	for _, l := range found {
		if _, ok := known[l.Child]; !ok {
			known[l.Child] = l
			add = append(add, l)
		}
	}
	return len(add), appendSpawns(add)
}

func (s *Scanner) save() {
	b, err := json.Marshal(s.files)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.State), 0o755)
	tmp := s.State + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, s.State)
	}
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
