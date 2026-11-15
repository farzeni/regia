package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Timeline entries carry no speaker field. Observed shape (claude 2.1.32x):
//   - agent result: has response text (state done/blocked/failed)
//   - agent step:   state "working", no text (the step description)
//   - your message: no text, state carried over from before
// A message sent while the agent was working looks like a step, so entries
// without text are checked against your messages in the session transcript.

type EntryKind int

const (
	KindStep EntryKind = iota
	KindResult
	KindUser
)

type HEntry struct {
	At     time.Time
	State  string
	Detail string
	Kind   EntryKind
}

// Turn is one of your messages and what the agent produced after it.
type Turn struct {
	User    *HEntry // nil for activity before your first visible message
	Results []HEntry
	Steps   int
	Repeats int // same message sent again shortly after
	LastAt  time.Time
}

type History struct {
	Turns  []Turn  // chronological
	Task   string  // your latest message (what it's working on)
	Status *HEntry // latest result
}

var wsRe = regexp.MustCompile(`\s+`)

func norm(s string) string {
	return strings.ToLower(strings.TrimSpace(wsRe.ReplaceAllString(s, " ")))
}

func key(s string) string {
	n := []rune(norm(s))
	if len(n) > 48 { // long enough to get past a "<pasted_content id=…>" prefix
		n = n[:48]
	}
	return string(n)
}

// --- your messages from the transcript (tail only, cached by size) ---------

type userCache struct {
	size int64
	keys map[string]bool
}

var (
	ucMu  sync.Mutex
	ucMap = map[string]userCache{}
)

const (
	transcriptTail = 4 << 20
	taskGap        = 2 * time.Hour
)

func userMessages(path string) map[string]bool {
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	ucMu.Lock()
	c, ok := ucMap[path]
	ucMu.Unlock()
	if ok && c.size == fi.Size() {
		return c.keys
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	// The transcript only grows: read just what was appended since last time.
	keys, start := map[string]bool{}, int64(0)
	if ok && fi.Size() > c.size {
		keys, start = c.keys, c.size
	} else if fi.Size() > transcriptTail {
		start = fi.Size() - transcriptTail
	}
	_, _ = f.Seek(start, io.SeekStart)
	data, _ := io.ReadAll(f)
	// Only complete lines; a half-written last line is read next time.
	end := bytes.LastIndexByte(data, '\n') + 1
	data = data[:end]
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"type":"user"`)) {
			continue
		}
		var e struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil || e.Type != "user" || e.IsMeta {
			continue
		}
		var s string
		if json.Unmarshal(e.Message.Content, &s) != nil || strings.HasPrefix(strings.TrimSpace(s), "<") && !strings.HasPrefix(strings.TrimSpace(s), "<pasted") {
			continue
		}
		keys[key(s)] = true
	}
	ucMu.Lock()
	ucMap[path] = userCache{start + int64(end), keys}
	ucMu.Unlock()
	return keys
}

// --- history ---------------------------------------------------------------

func ReadHistory(a *Agent) History {
	var h History
	if a == nil || a.Interactive {
		return h
	}
	f, err := os.Open(filepath.Join(Dir(), "jobs", a.ID, "timeline.jsonl"))
	if err != nil {
		return h
	}
	defer f.Close()
	users := userMessages(TranscriptPath(a)) // the job file's path goes stale once the session enters a worktree

	var cur *Turn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<24)
	for sc.Scan() {
		var e struct{ At, State, Detail, Text string }
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339Nano, e.At)
		he := HEntry{At: t, State: normState(e.State), Detail: e.Detail}
		switch {
		case strings.TrimSpace(e.Text) != "":
			he.Kind = KindResult
		case users != nil && users[key(e.Detail)]:
			he.Kind = KindUser
		case he.State == "busy":
			he.Kind = KindStep
		default:
			he.Kind = KindUser // no transcript to check: fall back to the rule
		}
		if he.Kind == KindUser {
			// the same message again within a few minutes: a resend or a state
			// echo of it, not a new turn
			if cur != nil && cur.User != nil && key(cur.User.Detail) == key(he.Detail) && t.Sub(cur.User.At) < 10*time.Minute {
				cur.Repeats++
				continue
			}
			h.Turns = append(h.Turns, Turn{User: &he, LastAt: t})
			cur = &h.Turns[len(h.Turns)-1]
			continue
		}
		if cur == nil {
			h.Turns = append(h.Turns, Turn{LastAt: t})
			cur = &h.Turns[len(h.Turns)-1]
		}
		cur.LastAt = t
		if he.Kind == KindStep {
			cur.Steps++
		} else {
			cur.Results = append(cur.Results, he)
		}
	}
	for i := len(h.Turns) - 1; i >= 0 && h.Status == nil; i-- {
		if t := h.Turns[i]; len(t.Results) > 0 {
			r := t.Results[len(t.Results)-1]
			h.Status = &r
		}
	}
	// The task is the message that opened the current piece of work. Walk back
	// from your latest message: a message continues the previous turn's task
	// when that turn ended waiting on you (blocked) and it's less than taskGap
	// later; after a "done" result your next message is a new task.
	for i := len(h.Turns) - 1; i >= 0; i-- {
		t := h.Turns[i]
		if t.User == nil {
			continue
		}
		h.Task = t.User.Detail
		if i == 0 {
			break
		}
		p := h.Turns[i-1]
		if p.User == nil || len(p.Results) == 0 && p.Steps == 0 {
			// back-to-back messages: keep walking
			if p.User != nil && t.User.At.Sub(p.User.At) <= taskGap {
				continue
			}
			break
		}
		last := p.Results
		if len(last) == 0 || last[len(last)-1].State != "blocked" || t.User.At.Sub(p.LastAt) > taskGap {
			break
		}
	}
	if h.Task == "" {
		h.Task = a.Intent
	}
	return h
}

var PasteRe = regexp.MustCompile(`(?s)<pasted_content[^>]*>.*?(</pasted_content>|$)`)

// Tidy makes a one-line, readable version of a message.
func Tidy(s string) string {
	s = PasteRe.ReplaceAllString(s, "[pasted] ")
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}
