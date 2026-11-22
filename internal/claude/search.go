package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Search inside sessions: the conversation is read from the session
// transcript (~/.claude/projects/…/<session>.jsonl), the internal format
// Claude Code writes. Best effort: unknown lines and fields are skipped.

type Role int

const (
	RoleYou   Role = iota // your messages
	RoleAgent             // the agent's text replies
	RoleTool              // tool calls and their output
)

// Msg is one message of a conversation, in transcript order.
type Msg struct {
	Role Role
	At   time.Time
	Text string
	low  string // Text lowercased, for matching
	// RoleTool messages: a call (or a thought) has Tool, its output has
	// Result (see steps.go; Conversation pairs them).
	Tool   *Tool
	Result *Result
}

// maxToolText caps how much of one tool call or output is kept: tool output
// is most of a transcript and a match deep inside a huge one is rarely useful.
const maxToolText = 16 << 10

type msgCache struct {
	mu   sync.Mutex
	size int64
	msgs []Msg
	meta SessionMeta
}

var (
	mcMu  sync.Mutex
	mcMap = map[string]*msgCache{}
)

// Messages returns a transcript's conversation. The transcript only grows, so
// it's cached by size and only what was appended since is read again.
func Messages(path string) []Msg {
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	mcMu.Lock()
	c := mcMap[path]
	if c == nil {
		c = &msgCache{}
		mcMap[path] = c
	}
	mcMu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if fi.Size() == c.size {
		return c.msgs
	}
	if fi.Size() < c.size { // rewritten: start over
		c.size, c.msgs, c.meta = 0, nil, SessionMeta{}
	}
	f, err := os.Open(path)
	if err != nil {
		return c.msgs
	}
	defer f.Close()
	if _, err := f.Seek(c.size, io.SeekStart); err != nil {
		return c.msgs
	}
	rd := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil { // at EOF, a half-written last line is read next time
			break
		}
		c.msgs = appendMsgs(c.msgs, line, c.size)
		c.meta.read(line)
		c.size += int64(len(line))
	}
	return c.msgs
}

var (
	userType  = []byte(`"type":"user"`)
	agentType = []byte(`"type":"assistant"`)
)

type rawBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
	Source    *rawSource      `json:"source"`
}

// appendMsgs adds a transcript line's messages; off is where the line starts
// in the file (kept on tool steps, to read the line again).
func appendMsgs(msgs []Msg, line []byte, off int64) []Msg {
	if !bytes.Contains(line, userType) && !bytes.Contains(line, agentType) {
		return msgs
	}
	var e struct {
		Type      string `json:"type"`
		IsMeta    bool   `json:"isMeta"`
		Timestamp string `json:"timestamp"`
		Message   struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		UseResult json.RawMessage `json:"toolUseResult"`
	}
	if json.Unmarshal(line, &e) != nil || e.IsMeta || (e.Type != "user" && e.Type != "assistant") {
		return msgs
	}
	at, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
	add := func(r Role, s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if len(s) > maxToolText && r == RoleTool {
			s = s[:maxToolText]
		}
		msgs = append(msgs, Msg{Role: r, At: at, Text: s, low: strings.ToLower(s)})
	}
	// a step is kept even with no text (a silent command, a redacted thought)
	step := func(s string, t *Tool, res *Result) {
		s = strings.TrimSpace(s)
		if len(s) > maxToolText {
			s = cutUTF8(s, maxToolText)
		}
		msgs = append(msgs, Msg{Role: RoleTool, At: at, Text: s, low: strings.ToLower(s), Tool: t, Result: res})
	}
	var s string
	if json.Unmarshal(e.Message.Content, &s) == nil {
		if e.Type == "user" && !metaText(s) {
			add(RoleYou, s)
		}
		return msgs
	}
	var blocks []rawBlock
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return msgs
	}
	results := 0 // toolUseResult describes the line's only result
	for _, b := range blocks {
		if b.Type == "tool_result" {
			results++
		}
	}
	for i, b := range blocks {
		switch b.Type {
		case "text":
			if e.Type == "assistant" {
				add(RoleAgent, b.Text)
			} else if !metaText(b.Text) {
				add(RoleYou, b.Text)
			}
		case "thinking", "redacted_thinking":
			step(b.Thinking, &Tool{ID: thoughtID(off, i), Name: ThinkingTool, Off: off}, nil)
		case "tool_use":
			step(toolCall(b), parseTool(b, off), nil)
		case "tool_result":
			var use json.RawMessage
			if results == 1 {
				use = e.UseResult
			}
			text, parts := toolOutput(b.Content)
			step(text, nil, parseResult(b, parts, use, off))
		}
	}
	return msgs
}

// metaText: what Claude Code injects as a user message (command output,
// reminders, caveats), not something you wrote. A paste is yours.
func metaText(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "<") && !strings.HasPrefix(t, "<pasted")
}

// toolCall renders a tool call as "Name: what it does": a Bash command, a
// file path, a pattern; anything else as its JSON input.
func toolCall(b rawBlock) string {
	var in map[string]any
	_ = json.Unmarshal(b.Input, &in)
	for _, k := range []string{"command", "file_path", "pattern", "url", "query", "prompt", "description"} {
		if v, ok := in[k].(string); ok && v != "" {
			return b.Name + ": " + v
		}
	}
	return b.Name + ": " + string(b.Input)
}

// toolOutput is a tool_result's text, and its parts when it has several
// (text, images…).
func toolOutput(c json.RawMessage) (string, []rawBlock) {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return s, nil
	}
	var parts []rawBlock
	if json.Unmarshal(c, &parts) != nil {
		return "", nil
	}
	var out []string
	for _, p := range parts {
		if p.Type == "text" {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, "\n"), parts
}

// --- transcript paths ------------------------------------------------------

var (
	tpMu  sync.Mutex
	tpMap = map[string]string{}
)

// TranscriptPath is where a session's transcript is. The job file names it
// for background sessions, but the file moves when the session enters a
// worktree (its project folder changes), so when it isn't there any more, or
// there's no job file, it's looked up by session id.
func TranscriptPath(a *Agent) string {
	if a.Transcript != "" {
		if _, err := os.Stat(a.Transcript); err == nil {
			return a.Transcript
		}
	}
	if a.SessionID == "" {
		return ""
	}
	tpMu.Lock()
	defer tpMu.Unlock()
	if p, ok := tpMap[a.SessionID]; ok && p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	ms, _ := filepath.Glob(filepath.Join(Dir(), "projects", "*", a.SessionID+".jsonl"))
	p := ""
	if len(ms) > 0 {
		p = ms[0]
	}
	tpMap[a.SessionID] = p
	return p
}

// --- queries ---------------------------------------------------------------

// Query is what you typed: words, "quoted phrases" or /regular expressions/,
// that must all appear in the same message, ignoring case.
type Query struct {
	Terms []string
	Res   []*regexp.Regexp
	Err   error // a pattern that doesn't compile: nothing matches
}

// regexEnd is where a /pattern/ starting s ends: the index of the closing
// slash, which must be followed by a space or the end (so a path such as
// /home/me is a word), not escaped and not right after the opening one; -1
// when there's none.
func regexEnd(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '/':
			if i > 1 && (i+1 == len(s) || unicode.IsSpace(rune(s[i+1]))) {
				return i
			}
		}
	}
	return -1
}

func ParseQuery(s string) Query {
	var q Query
	for len(s) > 0 {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)
		if s == "" {
			break
		}
		var t string
		if s[0] == '/' {
			if end := regexEnd(s); end > 0 {
				re, err := regexp.Compile("(?i)" + s[1:end])
				if err != nil && q.Err == nil {
					q.Err = fmt.Errorf("/%s/: %w", s[1:end], err)
				}
				if re != nil {
					q.Res = append(q.Res, re)
				}
				s = s[end+1:]
				continue
			}
		}
		if s[0] == '"' {
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				t, s = s[1:], ""
			} else {
				t, s = s[1:1+end], s[2+end:]
			}
		} else {
			end := strings.IndexFunc(s, unicode.IsSpace)
			if end < 0 {
				end = len(s)
			}
			t, s = s[:end], s[end:]
		}
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			q.Terms = append(q.Terms, t)
		}
	}
	return q
}

// Empty: nothing to look for. A bad pattern isn't empty: it finds nothing,
// and says why.
func (q Query) Empty() bool { return len(q.Terms) == 0 && len(q.Res) == 0 && q.Err == nil }

// Match: every term and pattern appears in the message.
func (q Query) Match(m *Msg) bool {
	if q.Empty() || q.Err != nil {
		return false
	}
	for _, t := range q.Terms {
		if !strings.Contains(m.low, t) {
			return false
		}
	}
	for _, re := range q.Res {
		if !re.MatchString(m.Text) {
			return false
		}
	}
	return true
}

// Spans finds where the terms occur in s, ignoring case: sorted,
// non-overlapping byte ranges [start, end) of s, for highlighting.
func (q Query) Spans(s string) [][2]int {
	if q.Empty() || s == "" {
		return nil
	}
	low, idx := foldMap(s)
	var sp [][2]int
	for _, t := range q.Terms {
		for off := 0; ; {
			i := strings.Index(low[off:], t)
			if i < 0 {
				break
			}
			a, b := off+i, off+i+len(t)
			sp = append(sp, [2]int{idx[a], idx[b]})
			off = b
		}
	}
	for _, re := range q.Res {
		for _, r := range re.FindAllStringIndex(s, -1) {
			sp = append(sp, [2]int{r[0], r[1]}) // empty matches are dropped below
		}
	}
	sort.Slice(sp, func(i, j int) bool { return sp[i][0] < sp[j][0] })
	var out [][2]int
	for _, r := range sp {
		if r[0] >= r[1] {
			continue
		}
		if n := len(out); n > 0 && r[0] <= out[n-1][1] {
			out[n-1][1] = max(out[n-1][1], r[1])
			continue
		}
		out = append(out, r)
	}
	return out
}

// foldMap lowercases s rune by rune and maps each byte of the result back to
// the offset of the rune it came from in s (plus one entry for the end), so
// matches found in the lowercase text point at the original.
func foldMap(s string) (string, []int) {
	var b strings.Builder
	b.Grow(len(s))
	idx := make([]int, 0, len(s)+1)
	for i, r := range s {
		n := b.Len()
		b.WriteRune(unicode.ToLower(r))
		for range b.Len() - n {
			idx = append(idx, i)
		}
	}
	idx = append(idx, len(s))
	return b.String(), idx
}

// --- search ----------------------------------------------------------------

// Source is a session to search: its id in the list and its transcript.
type Source struct{ ID, Path string }

type Hit struct {
	Index int // in Messages(Path)
	Msg   Msg
}

// SessionHits is what matched in one session: newest first, at most the
// limit passed to Search (Total counts them all).
type SessionHits struct {
	ID, Path string
	Hits     []Hit
	Total    int
	Last     time.Time // the newest match
}

// Search looks for q in every source's messages (tool calls and output only
// with tools) and returns the sessions with a match, newest match first.
func Search(srcs []Source, q Query, tools bool, perSession int) []SessionHits {
	if q.Empty() {
		return nil
	}
	res := make([]*SessionHits, len(srcs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i, s := range srcs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			msgs := Messages(s.Path)
			var sh *SessionHits
			for j := len(msgs) - 1; j >= 0; j-- {
				m := &msgs[j]
				if m.Role == RoleTool && !tools || !q.Match(m) {
					continue
				}
				if sh == nil {
					sh = &SessionHits{ID: s.ID, Path: s.Path, Last: m.At}
				}
				sh.Total++
				if len(sh.Hits) < perSession {
					sh.Hits = append(sh.Hits, Hit{Index: j, Msg: *m})
				}
			}
			res[i] = sh
		}()
	}
	wg.Wait()
	var out []SessionHits
	for _, r := range res {
		if r != nil {
			out = append(out, *r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out
}
