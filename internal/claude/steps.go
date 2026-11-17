package claude

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif" // image sizes
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Tool calls as structured steps: what the agent panel shows of each call
// (name, the small input fields, its result's error flag, diff hunks, output
// tail, images) is parsed with the messages, kept small. Anything bigger (a
// whole Bash output, an image's bytes) is read again from the transcript when
// asked for, at the line's offset (RawLine, ImageData).

// ThinkingTool is the name a thinking block gets: thoughts are kept as tool
// calls, so a run of steps shows them in order with the rest.
const ThinkingTool = "thinking"

const (
	maxInField    = 1 << 10 // an input string kept in Tool.In (longer ones are cut)
	maxInfoField  = 256     // a result string kept in Result.Info (longer ones are content: skipped)
	maxPatchLines = 200     // diff lines kept per step (Added/Removed still count them all)
	maxTailLines  = 15      // Bash stdout / stderr lines kept, from the end
	maxTailBytes  = 4 << 10
)

// bigInput are input fields never kept in Tool.In: the edit itself is in the
// result's patch, and the text is in the message.
var bigInput = map[string]bool{"old_string": true, "new_string": true, "content": true}

// Tool is a tool call (or a thought, Name ThinkingTool).
type Tool struct {
	ID   string
	Name string
	// In has the input's top-level strings, numbers and bools as text
	// (file_path, command, pattern, url, query, description, subagent_type,
	// offset, limit…), strings cut to 1 KB; old_string, new_string and
	// content are left out. The whole call is the Msg's Text (cut at 16 KB).
	In    map[string]string
	Todos []Todo // TodoWrite: the list as written
	Off   int64  // byte offset of the transcript line (RawLine)
}

type Todo struct{ Content, Status string } // Status: pending, in_progress, completed

// Result is what a tool call returned. Its text is the Msg's Text.
type Result struct {
	ID      string // the call's
	IsError bool
	// Edit/Write: the file and its hunks (a created file is one hunk of
	// added lines); lines are cut at maxPatchLines in all, Added and Removed
	// count all of them.
	FilePath       string
	Patch          []Hunk
	Added, Removed int
	// Bash: the last lines of each stream, and whether it was interrupted.
	Stdout, Stderr string
	Interrupted    bool
	Images         []Image // image blocks in the result (screenshots, Read of an image)
	// Info has toolUseResult's other short fields as text: its top-level
	// strings (up to 256 bytes), numbers and bools ("type", "status",
	// "agentId", "numFiles", "code"…) and those of objects one level down as
	// "file.numLines", "gitOperation.pr"…
	Info map[string]string
	Off  int64 // byte offset of the transcript line (RawLine, ImageData)
}

// Hunk is one hunk of a structured patch; Lines start with ' ', '+' or '-'.
type Hunk struct {
	OldStart, OldLines, NewStart, NewLines int
	Lines                                  []string
}

// Image is an image block of a result: not its bytes (ImageData decodes them
// from the transcript), only what a line about it needs.
type Image struct {
	MediaType string // image/png…
	W, H      int    // 0 when the header couldn't be read
	Size      int    // bytes, decoded
	N         int    // which image of the line, for ImageData
}

// --- parsing ---------------------------------------------------------------

func parseTool(b rawBlock, off int64) *Tool {
	t := &Tool{ID: b.ID, Name: b.Name, Off: off}
	var in map[string]json.RawMessage
	if json.Unmarshal(b.Input, &in) != nil {
		return t
	}
	for k, v := range in {
		if bigInput[k] {
			continue
		}
		if k == "todos" {
			_ = json.Unmarshal(v, &t.Todos)
			continue
		}
		if s, ok := scalar(v, maxInField, true); ok {
			if t.In == nil {
				t.In = make(map[string]string, len(in))
			}
			t.In[k] = s
		}
	}
	return t
}

// scalar renders a JSON string, number or bool as text. A longer string is
// cut to max bytes, or skipped (ok false) unless cut is set.
func scalar(v json.RawMessage, max int, cut bool) (string, bool) {
	if len(v) == 0 {
		return "", false
	}
	switch c := v[0]; {
	case c == '"':
		if !cut && len(v) > 6*max+2 { // even all escapes would decode longer than max
			return "", false
		}
		var s string
		if json.Unmarshal(v, &s) != nil {
			return "", false
		}
		if len(s) > max {
			if !cut {
				return "", false
			}
			s = cutUTF8(s, max)
		}
		return s, true
	case c == 't' || c == 'f' || c == '-' || c >= '0' && c <= '9':
		return string(v), true
	}
	return "", false
}

// cutUTF8 cuts s to at most n bytes without splitting a rune.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// parseResult reads a tool_result block (its content already split into
// parts, when it isn't a plain string) and the line's toolUseResult.
func parseResult(b rawBlock, parts []rawBlock, use json.RawMessage, off int64) *Result {
	r := &Result{ID: b.ToolUseID, IsError: b.IsError, Off: off}
	for _, p := range parts {
		if p.Type == "image" && p.Source != nil {
			r.Images = append(r.Images, imageInfo(p.Source, len(r.Images)))
		}
	}
	if len(use) > 0 && use[0] == '{' {
		r.useResult(use)
	}
	return r
}

// useResult reads a line's toolUseResult (Claude Code's own record of the
// call: patch, stdout, file info…).
func (r *Result) useResult(use json.RawMessage) {
	var f map[string]json.RawMessage
	if json.Unmarshal(use, &f) != nil {
		return
	}
	info := func(k string, v json.RawMessage) {
		if s, ok := scalar(v, maxInfoField, false); ok {
			if r.Info == nil {
				r.Info = map[string]string{}
			}
			r.Info[k] = s
		}
	}
	for k, v := range f {
		switch k {
		case "structuredPatch":
			var hs []struct {
				OldStart, OldLines, NewStart, NewLines int
				Lines                                  []string
			}
			_ = json.Unmarshal(v, &hs)
			for _, h := range hs {
				r.addHunk(Hunk{h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Lines})
			}
		case "stdout":
			r.Stdout = tail(str(v))
		case "stderr":
			r.Stderr = tail(str(v))
		case "interrupted":
			r.Interrupted = string(v) == "true"
		case "filePath":
			r.FilePath = str(v)
		case "content", "originalFile", "oldString", "newString":
			// content: kept only for a created file, below
		default:
			if len(v) > 0 && v[0] == '{' { // one level down, unless it holds content (a Read image's base64)
				if len(v) > 16<<10 {
					continue
				}
				var sub map[string]json.RawMessage
				if json.Unmarshal(v, &sub) == nil {
					for sk, sv := range sub {
						info(k+"."+sk, sv)
					}
				}
				continue
			}
			info(k, v)
		}
	}
	// Write of a new file: no patch, the content is all added lines
	if r.Info["type"] == "create" && len(r.Patch) == 0 {
		if c := str(f["content"]); c != "" {
			ls := strings.Split(strings.TrimSuffix(c, "\n"), "\n")
			for i := range ls {
				ls[i] = "+" + ls[i]
			}
			r.addHunk(Hunk{NewStart: 1, NewLines: len(ls), Lines: ls})
		}
	}
	if r.FilePath == "" {
		r.FilePath = r.Info["file.filePath"]
	}
}

// addHunk counts a hunk's lines and keeps them up to maxPatchLines in all.
func (r *Result) addHunk(h Hunk) {
	kept := 0
	for _, x := range r.Patch {
		kept += len(x.Lines)
	}
	for _, l := range h.Lines {
		if strings.HasPrefix(l, "+") {
			r.Added++
		} else if strings.HasPrefix(l, "-") {
			r.Removed++
		}
	}
	h.Lines = h.Lines[:min(len(h.Lines), max(0, maxPatchLines-kept))]
	r.Patch = append(r.Patch, h)
}

func str(v json.RawMessage) string {
	var s string
	_ = json.Unmarshal(v, &s)
	return s
}

// tail keeps the last maxTailLines lines of s (and at most maxTailBytes).
func tail(s string) string {
	s = strings.TrimRight(s, "\n")
	if len(s) > maxTailBytes {
		s = s[len(s)-maxTailBytes:]
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	n := 0
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			if n++; n == maxTailLines {
				return strings.Clone(s[i+1:])
			}
		}
	}
	return strings.Clone(s)
}

type rawSource struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// imageInfo reads an image's size from the start of its base64 data.
func imageInfo(s *rawSource, n int) Image {
	im := Image{MediaType: s.MediaType, N: n, Size: base64.StdEncoding.DecodedLen(len(s.Data))}
	head := s.Data[:min(len(s.Data), 64<<10)] // a JPEG's size may come after its metadata
	head = head[:len(head)/4*4]
	if b, err := base64.StdEncoding.DecodeString(head); err == nil || len(b) > 0 {
		if c, _, err := image.DecodeConfig(bytes.NewReader(b)); err == nil {
			im.W, im.H = c.Width, c.Height
		}
	}
	return im
}

// --- reading back ------------------------------------------------------------

// RawLine reads the transcript line at off again (a Tool's or Result's Off):
// for what isn't kept in memory, like a whole Bash output.
func RawLine(path string, off int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	rd := bufio.NewReaderSize(f, 1<<20)
	line, err := rd.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, err
	}
	return line, nil
}

// ImageData decodes a result's image from the transcript.
func ImageData(path string, r *Result, im Image) ([]byte, error) {
	line, err := RawLine(path, r.Off)
	if err != nil {
		return nil, err
	}
	var e struct {
		Message struct {
			Content []rawBlock `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return nil, err
	}
	for _, b := range e.Message.Content {
		if b.Type != "tool_result" || b.ToolUseID != r.ID {
			continue
		}
		var parts []rawBlock
		_ = json.Unmarshal(b.Content, &parts)
		n := 0
		for _, p := range parts {
			if p.Type == "image" && p.Source != nil {
				if n == im.N {
					return base64.StdEncoding.DecodeString(p.Source.Data)
				}
				n++
			}
		}
	}
	return nil, errors.New("image not found in the transcript")
}

// --- the conversation --------------------------------------------------------

// Step is a tool call with its result, as the conversation shows it.
type Step struct {
	*Tool
	Res     *Result   // nil while the call runs (or if it never returned)
	At, End time.Time // when it was called and when the result came
	Text    string    // the call as text ("Bash: go test"), or the thought
	Out     string    // the result's text (cut at 16 KB)
}

// Running: no result yet (a thought is never running).
func (s *Step) Running() bool { return s.Res == nil && s.Name != ThinkingTool }

// Failed: the result is an error.
func (s *Step) Failed() bool { return s.Res != nil && s.Res.IsError }

// Duration from the call to its result (0 while it runs).
func (s *Step) Duration() time.Duration {
	if s.End.IsZero() || s.At.IsZero() {
		return 0
	}
	return s.End.Sub(s.At)
}

// Entry is one item of a conversation: a message (yours or the agent's), or
// a step (a tool call with its result, or a thought). One is set.
type Entry struct {
	Msg  *Msg
	Step *Step
}

// Conversation pairs each tool call in msgs with its result: your messages,
// the agent's replies and the steps, in order. The steps point into msgs'
// Tool and Result (shared, never changed).
func Conversation(msgs []Msg) []Entry {
	out := make([]Entry, 0, len(msgs))
	open := map[string]*Step{}
	for i := range msgs {
		m := &msgs[i]
		switch {
		case m.Tool != nil:
			s := &Step{Tool: m.Tool, At: m.At, Text: m.Text}
			if m.Tool.ID != "" {
				open[m.Tool.ID] = s
			}
			if m.Tool.Name == ThinkingTool {
				s.End = m.At
			}
			out = append(out, Entry{Step: s})
		case m.Result != nil:
			if s := open[m.Result.ID]; s != nil {
				s.Res, s.End, s.Out = m.Result, m.At, m.Text
				delete(open, m.Result.ID)
			}
		case m.Role != RoleTool:
			out = append(out, Entry{Msg: m})
		}
	}
	return out
}

// ShortInput is the input field that says most about a call, for a one-line
// summary: the command, path, pattern, url, query or description.
func (t *Tool) ShortInput() string {
	for _, k := range []string{"command", "file_path", "path", "pattern", "url", "query", "description", "prompt", "skill", "subagent_type"} {
		if v := t.In[k]; v != "" {
			return v
		}
	}
	return ""
}

// thoughtID names a thought (thinking blocks have no id), stable across reads.
func thoughtID(off int64, i int) string {
	return "thought:" + strconv.FormatInt(off, 10) + ":" + strconv.Itoa(i)
}
