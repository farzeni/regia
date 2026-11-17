package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// toolStep is one tool call and its result as transcript lines.
type toolStep struct {
	name   string
	in     map[string]any
	result any            // the tool_result's content: a string or blocks
	use    map[string]any // toolUseResult
	err    bool
}

// stepsModel gives alpha a transcript: "do it", the steps in one run, "Done.".
// The run and every step are unfolded; alpha's cwd is returned.
func stepsModel(t *testing.T, steps ...toolStep) (*model, string) {
	t.Helper()
	m := testModel(t)
	m.Update(tkey("esc"))
	goTo(t, m, "aaaa1111")
	m.h = 60
	cwd := m.agents["aaaa1111"].Cwd
	line := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	ls := []string{`{"type":"user","timestamp":"2026-11-14T09:00:00Z","message":{"content":"do it"}}`}
	for i, s := range steps {
		id := fmt.Sprintf("t%d", i)
		ts := fmt.Sprintf("2026-11-14T09:00:%02dZ", i+1)
		ls = append(ls, line(map[string]any{"type": "assistant", "timestamp": ts, "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use", "id": id, "name": s.name, "input": s.in}}}}))
		res := map[string]any{"type": "user", "timestamp": ts, "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": id, "content": s.result, "is_error": s.err}}}}
		if s.use != nil {
			res["toolUseResult"] = s.use
		}
		ls = append(ls, line(res))
	}
	ls = append(ls, `{"type":"assistant","timestamp":"2026-11-14T09:01:00Z","message":{"content":[{"type":"text","text":"Done."}]}}`)
	writeConv(t, m, ls...)
	m.View()
	st := m.convOf("aaaa1111")
	for _, b := range m.conv.blocks {
		if b.kind == blockRun {
			st.open[b.key] = true
			for _, s := range b.run.steps {
				st.open[s.ID] = true
			}
		}
	}
	st.ver++
	return m, cwd
}

// opened replaces openThing for the test: what enter asked to open.
func opened(t *testing.T) *[]openTarget {
	var got []openTarget
	prev := openThing
	openThing = func(o openTarget) tea.Cmd { got = append(got, o); return nil }
	t.Cleanup(func() { openThing = prev })
	return &got
}

// enterOn focuses the conversation and presses enter on the step of tool name.
func enterOn(t *testing.T, m *model, name string) {
	t.Helper()
	if !m.convFocus {
		m.Update(tkey("o"))
	}
	selectWhere(t, m, func(b convBlock) bool { return b.kind == blockStep && b.step.Name == name })
	m.Update(tkey("enter"))
}

func wantScreen(t *testing.T, m *model, wants ...string) string {
	t.Helper()
	s := convScreen(m)
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("screen lacks %q", w)
		}
	}
	if t.Failed() {
		t.Fatalf("screen:\n%s", s)
	}
	return s
}

// An edit of 40 changed lines: folded as path:line +20 −18, unfolded as a
// numbered diff cut at 20 lines; enter opens the file at the first change.
func TestEditStep(t *testing.T) {
	got := opened(t)
	var lines []string
	lines = append(lines, " ctx a", " ctx b")
	for i := range 18 {
		lines = append(lines, fmt.Sprintf("-old %d", i))
	}
	for i := range 20 {
		lines = append(lines, fmt.Sprintf("+new %d", i))
	}
	m, cwd := stepsModel(t, toolStep{})
	path := filepath.Join(cwd, "internal", "x.go")
	m, _ = stepsModel(t, toolStep{
		name:   "Edit",
		in:     map[string]any{"file_path": path, "old_string": "a", "new_string": "b"},
		result: "The file has been updated successfully.",
		use: map[string]any{"filePath": path, "structuredPatch": []any{
			map[string]any{"oldStart": 10, "oldLines": 20, "newStart": 10, "newLines": 22, "lines": lines}}},
	})
	m.agents["aaaa1111"].Cwd = cwd
	wantScreen(t, m,
		"✎ internal/x.go:12 +20 −18", // relative to the agent's directory
		"10   ctx a", "12 - old 0", "29 - old 17",
		"… +20 more")
	if s := convScreen(m); strings.Contains(s, "+ new 0") {
		t.Fatalf("the diff should stop at 20 lines:\n%s", s)
	}
	enterOn(t, m, "Edit")
	if len(*got) != 1 || (*got)[0] != (openTarget{Kind: "file", Path: path, Line: 12, Dir: cwd}) {
		t.Fatalf("enter should open %s at 12, got %+v", path, *got)
	}
}

// A new file reads "+N new"; its diff is all added lines.
func TestWriteStep(t *testing.T) {
	m, _ := stepsModel(t, toolStep{
		name:   "Write",
		in:     map[string]any{"file_path": "/repo/b.go", "content": "package a\n\nfunc B() {}\n"},
		result: "File created successfully at: /repo/b.go",
		use:    map[string]any{"type": "create", "filePath": "/repo/b.go", "content": "package a\n\nfunc B() {}\n", "structuredPatch": []any{}},
	})
	wantScreen(t, m, "✎ /repo/b.go:1 +3 new", "1 + package a", "3 + func B() {}")
}

func bashOut(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "out %d\n", i)
	}
	return b.String()
}

// A failed command: `$ cmd ✗`; unfolded the whole command, the last 15
// lines of stdout, stderr, the interrupted mark; enter pages all of it.
func TestBashStep(t *testing.T) {
	got := opened(t)
	m, _ := stepsModel(t, toolStep{
		name:   "Bash",
		in:     map[string]any{"command": "go test ./...\n  && echo ok", "description": "Run the tests"},
		result: "Exit code 1\n" + bashOut(20),
		use:    map[string]any{"stdout": bashOut(20), "stderr": "FAIL x\n", "interrupted": true},
		err:    true,
	})
	s := wantScreen(t, m, "$ go test ./... && echo ok ✗ ⏹", "$ go test ./...", "&& echo ok", "out 5", "out 19", "FAIL x", "⏹ interrupted", "enter: the whole output")
	if strings.Contains(s, "out 4\n") || strings.Contains(s, "out 4 ") {
		t.Fatalf("only the last 15 lines show:\n%s", s)
	}
	enterOn(t, m, "Bash")
	if len(*got) != 1 || (*got)[0].Kind != "pager" {
		t.Fatalf("enter should page the output, got %+v", *got)
	}
	text := (*got)[0].Text
	for _, want := range []string{"$ go test ./...", "out 0\n", "out 19\n", "FAIL x"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the pager should get the whole output (%q missing):\n%s", want, text)
		}
	}
}

// Output Claude Code saved to a file (too big for the transcript): enter
// pages that file.
func TestBashPersistedOutput(t *testing.T) {
	got := opened(t)
	p := filepath.Join(t.TempDir(), "out.txt")
	os.WriteFile(p, []byte("the whole big output\n"), 0o644)
	m, _ := stepsModel(t, toolStep{
		name: "Bash", in: map[string]any{"command": "make"}, result: "…",
		use: map[string]any{"stdout": "tail", "stderr": "", "persistedOutputPath": p},
	})
	enterOn(t, m, "Bash")
	if len(*got) != 1 || !strings.Contains((*got)[0].Text, "the whole big output") {
		t.Fatalf("enter should page the saved output, got %+v", *got)
	}
}

// `claude --bg` started beta: the step names it with its live state, and
// enter moves the list's cursor to it.
func TestSpawnStep(t *testing.T) {
	opened(t)
	m, _ := stepsModel(t, toolStep{
		name: "Bash", in: map[string]any{"command": `claude --bg "fix the inbox"`},
		result: "backgrounded · bbbb2222 · beta",
		use:    map[string]any{"stdout": "backgrounded · bbbb2222 · beta\n  claude attach bbbb2222  open it\n", "stderr": ""},
	})
	wantScreen(t, m, "⇢ spawned beta (done)")
	m.agents["bbbb2222"].State = "busy"
	m.noteLive()
	wantScreen(t, m, "⇢ spawned beta (working)")
	enterOn(t, m, "Bash")
	if m.cursorID != "bbbb2222" || m.convFocus {
		t.Fatalf("enter should select beta and leave the conversation (at %s, focus %v)", m.cursorID, m.convFocus)
	}
}

// Output that only quotes a banner isn't a spawn.
func TestBannerInOutputIsNoSpawn(t *testing.T) {
	m, _ := stepsModel(t, toolStep{
		name: "Bash", in: map[string]any{"command": "cat notes.md"}, result: "backgrounded · bbbb2222",
		use: map[string]any{"stdout": "backgrounded · bbbb2222\n", "stderr": ""},
	})
	s := wantScreen(t, m, "$ cat notes.md")
	if strings.Contains(s, "spawned") {
		t.Fatalf("a cat isn't a spawn:\n%s", s)
	}
}

func TestReadGrepGlobSteps(t *testing.T) {
	got := opened(t)
	m, cwd := stepsModel(t)
	a := filepath.Join(cwd, "x.go")
	m, _ = stepsModel(t,
		toolStep{name: "Read", in: map[string]any{"file_path": a}, result: "1\tpackage x",
			use: map[string]any{"type": "text", "file": map[string]any{"filePath": a, "numLines": 200, "startLine": 1, "totalLines": 300}}},
		toolStep{name: "Grep", in: map[string]any{"pattern": "func New", "output_mode": "content", "path": cwd},
			result: a + ":12:func New() {}\n" + a + ":40:func NewX() {}",
			use:    map[string]any{"mode": "content", "numFiles": 1, "numLines": 12}},
		toolStep{name: "Glob", in: map[string]any{"pattern": "**/*.go"}, result: a + "\n" + filepath.Join(cwd, "y.go"),
			use: map[string]any{"numFiles": 2, "durationMs": 3}},
	)
	m.agents["aaaa1111"].Cwd = cwd
	wantScreen(t, m,
		"◦ read x.go 1-200", "lines 1-200 of 300",
		`◦ grep "func New" 12 hits`, "x.go:12:func New() {}", "x.go:40:func NewX() {}",
		`◦ glob "**/*.go" 2 files`, "y.go")
	enterOn(t, m, "Read")
	enterOn(t, m, "Grep")
	want := []openTarget{{Kind: "file", Path: a, Line: 1, Dir: cwd}, {Kind: "file", Path: a, Line: 12, Dir: cwd}}
	if len(*got) != 2 || (*got)[0] != want[0] || (*got)[1] != want[1] {
		t.Fatalf("enter should open the read file and the first hit, got %+v", *got)
	}
}

// A 4×3 PNG, as the fixture's screenshot.
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAQAAAADCAIAAAA7ljmRAAAADElEQVR4nGNgIAoAAAAnAAHKRxGqAAAAAElFTkSuQmCC"

// A browser's screenshot (any tool with images): `▣ screenshot 4×3`; enter
// writes it to a temp file that decodes, and opens it.
func TestImageStep(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	got := opened(t)
	m, _ := stepsModel(t, toolStep{
		name: "mcp__browser__take_screenshot", in: map[string]any{"fullPage": true},
		result: []any{map[string]any{"type": "text", "text": "Took it"},
			map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": tinyPNG}}},
	})
	wantScreen(t, m, "▣ screenshot 4×3", "image/png 4×3")
	enterOn(t, m, "mcp__browser__take_screenshot")
	if len(*got) != 1 || (*got)[0].Kind != "image" || !strings.HasPrefix((*got)[0].Path, imageDir()) {
		t.Fatalf("enter should open the image from %s, got %+v", imageDir(), *got)
	}
	b, err := os.ReadFile((*got)[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if c, _, err := image.DecodeConfig(bytes.NewReader(b)); err != nil || c.Width != 4 || c.Height != 3 {
		t.Fatalf("the temp file should be the 4×3 png: %v %+v", err, c)
	}
}

// A Read of an image is an image step too, named by its file.
func TestReadImageStep(t *testing.T) {
	m, _ := stepsModel(t, toolStep{
		name: "Read", in: map[string]any{"file_path": "/shots/a.png"},
		result: []any{map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": tinyPNG}}},
		use:    map[string]any{"type": "image", "file": map[string]any{"type": "image/png", "originalSize": 70}},
	})
	wantScreen(t, m, "▣ /shots/a.png 4×3")
}

// An async subagent: its report is the last reply of its own transcript
// (outputFile); enter pages that conversation.
func TestSubagentStep(t *testing.T) {
	got := opened(t)
	out := filepath.Join(t.TempDir(), "agent.output")
	os.WriteFile(out, []byte(`{"type":"user","timestamp":"2026-11-14T09:00:05Z","message":{"content":"Review the panel."}}
{"type":"assistant","timestamp":"2026-11-14T09:00:30Z","message":{"content":[{"type":"text","text":"The panel looks fine.\nOne nit in conv.go."}]}}
`), 0o644)
	m, _ := stepsModel(t, toolStep{
		name: "Agent", in: map[string]any{"description": "review panel", "subagent_type": "general-purpose", "prompt": "Review the panel."},
		result: "Async agent launched successfully.",
		use:    map[string]any{"isAsync": true, "status": "async_launched", "agentId": "a1", "outputFile": out},
	})
	wantScreen(t, m, "⇢ subagent: review panel (in background)", "The panel looks fine.", "One nit in conv.go.")
	enterOn(t, m, "Agent")
	if len(*got) != 1 || (*got)[0].Kind != "pager" || !strings.Contains((*got)[0].Text, "Review the panel.") || !strings.Contains((*got)[0].Text, "One nit") {
		t.Fatalf("enter should page the subagent's conversation, got %+v", *got)
	}
}

func TestWebSteps(t *testing.T) {
	got := opened(t)
	m, _ := stepsModel(t,
		toolStep{name: "WebFetch", in: map[string]any{"url": "https://example.com/docs", "prompt": "summarize"},
			result: "The page explains things.\nSecond line.", use: map[string]any{"url": "https://example.com/docs", "code": 200, "bytes": 900}},
		toolStep{name: "WebSearch", in: map[string]any{"query": "bubbletea mouse"},
			result: `Links: [{"title":"x","url":"https://github.com/charmbracelet/bubbletea"}]`, use: map[string]any{"query": "bubbletea mouse"}},
	)
	wantScreen(t, m, "◦ fetch https://example.com/docs", "The page explains things.", `◦ search "bubbletea mouse"`)
	enterOn(t, m, "WebFetch")
	enterOn(t, m, "WebSearch")
	if len(*got) != 2 || (*got)[0].URL != "https://example.com/docs" || (*got)[1].URL != "https://github.com/charmbracelet/bubbletea" {
		t.Fatalf("enter should open the URL, got %+v", *got)
	}
}

func TestTodoStep(t *testing.T) {
	m, _ := stepsModel(t, toolStep{
		name: "TodoWrite", in: map[string]any{"todos": []any{
			map[string]any{"content": "parse", "status": "completed"},
			map[string]any{"content": "render", "status": "in_progress"},
			map[string]any{"content": "ship", "status": "pending"}}},
		result: "Todos have been modified successfully.",
	})
	wantScreen(t, m, "☐ todos 1/3", "✓ parse", "▸ render", "◻ ship")
}

// Other tools keep the generic line; an MCP tool reads server:tool.
func TestMCPStepName(t *testing.T) {
	m, _ := stepsModel(t, toolStep{name: "mcp__browser__navigate", in: map[string]any{"url": "https://x.dev"}, result: "ok"})
	wantScreen(t, m, "• browser:navigate https://x.dev")
}

// Every step line fits its width, folded or not, at any width.
func TestStepLinesFit(t *testing.T) {
	m := convModel(t)
	defer withOpeners(t, "preset = vscode\nosc8 = on")() // the real links: escapes must not count as width
	st := m.convOf("aaaa1111")
	linked := false
	for _, w := range []int{20, 40, 90} {
		_, blocks := m.renderConv(m.agents["aaaa1111"], w)
		for _, b := range blocks {
			if b.kind == blockRun {
				st.open[b.key] = true
				for _, s := range b.run.steps {
					st.open[s.ID] = true
				}
			}
		}
		st.ver++
		ls, blocks := m.renderConv(m.agents["aaaa1111"], w)
		for _, b := range blocks {
			if b.kind != blockStep {
				continue
			}
			for _, l := range ls[b.top : b.top+b.n] {
				linked = linked || strings.Contains(l, "\x1b]8;")
				if ansi.StringWidth(l) > w {
					t.Fatalf("a %s step line is wider than %d: %q", b.step.Name, w, ansi.Strip(l))
				}
			}
		}
	}
	if !linked {
		t.Fatal("no step line carried a link: the test checks nothing")
	}
}

// A run line fits a narrow panel: cut at its width, its count first.
func TestRunLineFitsNarrow(t *testing.T) {
	m := convModel(t)
	for _, w := range []int{12, 20, 25} {
		ls, blocks := m.renderConv(m.agents["aaaa1111"], w)
		seen := false
		for _, b := range blocks {
			if b.kind != blockRun {
				continue
			}
			l := ls[b.top]
			if ansi.StringWidth(l) > w {
				t.Fatalf("a run line is wider than %d: %q", w, ansi.Strip(l))
			}
			seen = seen || strings.Contains(ansi.Strip(l), "▸ ⚙ 7")
		}
		if !seen {
			t.Fatalf("at %d columns the run line should still start with its count:\n%s", w, ansi.Strip(strings.Join(ls, "\n")))
		}
	}
}
