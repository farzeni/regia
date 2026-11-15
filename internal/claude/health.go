package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Stuck / unhealthy detection, for busy agents only (a session that ended
// its turn isn't stuck). Two cheap heuristics:
//
//   - quiet: nothing was written for StuckAfter: not the job's state or
//     timeline, not the transcript, not a subagent's transcript. Only file
//     mtimes are looked at.
//   - looping: the last ErrorRepeats tool results in the transcript were
//     errors from the same tool with the same message. The transcript is read
//     incrementally (only what was appended since the last tick, the first
//     time only its last healthTail bytes), so a tick costs a few stats.
//
// Both are guesses: a long build is quiet too. The UI shows them dim.

const (
	StuckAfter   = 10 * time.Minute
	ErrorRepeats = 3
	healthTail   = 256 << 10
)

// errStreak is the incremental scan state of one transcript.
type errStreak struct {
	size  int64             // bytes of the transcript scanned
	tools map[string]string // tool_use id → tool name, for the results that follow
	key   string            // tool + error of the current run of identical errors
	tool  string
	msg   string
	n     int
}

var (
	hsMu  sync.Mutex
	hsMap = map[string]*errStreak{}
)

// Health says why a busy agent looks stuck, or "" when it looks fine (or
// isn't busy). now is passed in so tests can move time.
func Health(a *Agent, now time.Time) string {
	if a == nil || a.Interactive || a.State != "busy" {
		return ""
	}
	tr := TranscriptPath(a)
	if s := errorRun(tr); s != nil && s.n >= ErrorRepeats {
		return fmt.Sprintf("same %s error %d×: %s", s.tool, s.n, s.msg)
	}
	last := lastActivity(a, tr)
	if last.IsZero() {
		return ""
	}
	if d := now.Sub(last); d >= StuckAfter {
		return "no activity for " + quietFor(d)
	}
	return ""
}

func quietFor(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// lastActivity is the newest mtime among the agent's job files, its
// transcript and its subagents' transcripts.
func lastActivity(a *Agent, tr string) time.Time {
	var last time.Time
	see := func(p string) {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(last) {
			last = fi.ModTime()
		}
	}
	job := filepath.Join(Dir(), "jobs", a.ID)
	see(filepath.Join(job, "state.json"))
	see(filepath.Join(job, "timeline.jsonl"))
	if tr != "" {
		see(tr)
		sub := filepath.Join(strings.TrimSuffix(tr, ".jsonl"), "subagents")
		if es, err := os.ReadDir(sub); err == nil {
			for _, e := range es {
				if fi, err := e.Info(); err == nil && fi.ModTime().After(last) {
					last = fi.ModTime()
				}
			}
		}
	}
	return last
}

// errorRun brings the transcript's error streak up to date and returns it
// (nil without a transcript). The result is shared: read it only.
func errorRun(path string) *errStreak {
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	hsMu.Lock()
	defer hsMu.Unlock()
	s := hsMap[path]
	if s != nil && s.size == fi.Size() {
		return s
	}
	start := int64(0)
	if s != nil && fi.Size() > s.size {
		start = s.size
	} else {
		s = &errStreak{tools: map[string]string{}} // new, or rewritten (shorter): start over
		if fi.Size() > healthTail {
			start = fi.Size() - healthTail
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()
	_, _ = f.Seek(start, io.SeekStart)
	data, _ := io.ReadAll(f)
	end := bytes.LastIndexByte(data, '\n') + 1 // a half-written last line is read next time
	s.scan(data[:end])
	s.size = start + int64(end)
	hsMap[path] = s
	return s
}

type trLine struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type trBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

func (s *errStreak) scan(data []byte) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		// cheap filters before parsing: only tool calls and their results matter
		if !bytes.Contains(line, []byte(`"tool_use"`)) && !bytes.Contains(line, []byte(`"tool_result"`)) {
			continue
		}
		var l trLine
		if json.Unmarshal(line, &l) != nil || l.IsSidechain || (l.Type != "user" && l.Type != "assistant") {
			continue
		}
		var blocks []trBlock
		if json.Unmarshal(l.Message.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			switch b.Type {
			case "tool_use":
				if len(s.tools) > 4096 { // bounded: old ids are never answered again
					clear(s.tools)
				}
				s.tools[b.ID] = b.Name
			case "tool_result":
				if !b.IsError {
					s.key, s.tool, s.msg, s.n = "", "", "", 0
					continue
				}
				tool := s.tools[b.ToolUseID]
				if tool == "" {
					tool = "tool"
				}
				msg := errorLine(b.Content)
				if k := tool + "\x00" + msg; k == s.key {
					s.n++
				} else {
					s.key, s.tool, s.msg, s.n = k, tool, msg, 1
				}
			}
		}
	}
}

// errorLine is the first telling line of a tool error: past "Exit code N"
// (every failing command says that) and the <tool_use_error> wrapper.
func errorLine(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		var parts []struct{ Type, Text string }
		if json.Unmarshal(raw, &parts) == nil {
			for _, p := range parts {
				if p.Type == "text" {
					text += p.Text + "\n"
				}
			}
		}
	}
	text = strings.NewReplacer("<tool_use_error>", "", "</tool_use_error>", "").Replace(text)
	first := ""
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if first == "" {
			first = l
		}
		if strings.HasPrefix(l, "Exit code ") {
			continue
		}
		first = l
		break
	}
	if r := []rune(first); len(r) > 120 {
		first = string(r[:120]) + "…"
	}
	return first
}
