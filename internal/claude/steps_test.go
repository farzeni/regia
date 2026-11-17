package claude

import (
	"bytes"
	"image"
	"os"
	"strings"
	"testing"
	"time"
)

// Real data (REGIA_BENCH_TRANSCRIPT): what a big transcript's steps look like.
func TestConversationRealData(t *testing.T) {
	p := os.Getenv("REGIA_BENCH_TRANSCRIPT")
	if p == "" {
		t.Skip("set REGIA_BENCH_TRANSCRIPT")
	}
	es := Conversation(Messages(p))
	kinds := map[string]int{}
	running, failed, imgs, patched := 0, 0, 0, 0
	for _, e := range es {
		if s := e.Step; s != nil {
			kinds[s.Name]++
			if s.Running() {
				running++
			}
			if s.Failed() {
				failed++
			}
			if s.Res != nil {
				imgs += len(s.Res.Images)
				if len(s.Res.Patch) > 0 {
					patched++
				}
			}
		}
	}
	t.Logf("%d entries · steps by kind %v · running %d · failed %d · images %d · patched %d", len(es), kinds, running, failed, imgs, patched)
}

// testdata/steps.jsonl has the shapes Claude Code writes (keys as in real
// transcripts, content made up): a thought, Read, Edit, a failed Bash, a
// Write that creates a file, a screenshot, TodoWrite, a subagent, and a call
// still running.
func fixtureConv(t *testing.T) []Entry {
	t.Helper()
	return Conversation(Messages("testdata/steps.jsonl"))
}

func TestConversationPairsSteps(t *testing.T) {
	es := fixtureConv(t)
	var got []string
	for _, e := range es {
		if e.Msg != nil {
			got = append(got, "msg:"+e.Msg.Text)
		} else {
			got = append(got, e.Step.Name)
		}
	}
	want := "msg:fix the parser|thinking|msg:Looking at it.|Read|Edit|Bash|Write|mcp__browser__take_screenshot|TodoWrite|Agent|msg:Done: tests fixed.|msg:thanks|Bash"
	if g := strings.Join(got, "|"); g != want {
		t.Fatalf("entries:\n got %s\nwant %s", g, want)
	}
	last := es[len(es)-1].Step
	if !last.Running() || last.Duration() != 0 {
		t.Errorf("a call with no result is running: %+v", last)
	}
}

func step(t *testing.T, name string) *Step {
	t.Helper()
	for _, e := range fixtureConv(t) {
		if e.Step != nil && e.Step.Name == name {
			return e.Step
		}
	}
	t.Fatalf("no %s step", name)
	return nil
}

func TestStepFields(t *testing.T) {
	r := step(t, "Read")
	if r.In["file_path"] != "/repo/a.go" || r.In["offset"] != "10" || r.In["limit"] != "40" || r.ShortInput() != "/repo/a.go" {
		t.Errorf("Read input: %v", r.In)
	}
	if r.Res.Info["file.numLines"] != "40" || r.Res.Info["file.startLine"] != "10" || r.Res.FilePath != "/repo/a.go" {
		t.Errorf("Read info: %v %q", r.Res.Info, r.Res.FilePath)
	}

	e := step(t, "Edit")
	if _, ok := e.In["old_string"]; ok {
		t.Error("old_string kept in the input")
	}
	if e.Res.FilePath != "/repo/a.go" || e.Res.Added != 2 || e.Res.Removed != 1 || len(e.Res.Patch) != 1 || e.Res.Patch[0].NewStart != 3 {
		t.Errorf("Edit result: %+v", e.Res)
	}
	if e.Duration() != 2*time.Second {
		t.Errorf("Edit duration %v", e.Duration())
	}

	b := step(t, "Bash")
	if !b.Failed() || b.In["command"] != "go test ./..." || b.In["description"] != "Run the tests" {
		t.Errorf("Bash: failed %v in %v", b.Failed(), b.In)
	}
	if n := strings.Count(b.Res.Stdout, "\n") + 1; n != maxTailLines || !strings.HasSuffix(b.Res.Stdout, "ok line 29") {
		t.Errorf("stdout tail: %d lines %q", n, b.Res.Stdout)
	}
	if b.Res.Stderr != "FAIL a_test.go:3" || !strings.Contains(b.Out, "Exit code 1") {
		t.Errorf("stderr %q out %q", b.Res.Stderr, b.Out)
	}

	w := step(t, "Write")
	if w.Res.Info["type"] != "create" || w.Res.Added != 3 || len(w.Res.Patch) != 1 || w.Res.Patch[0].Lines[0] != "+package a" {
		t.Errorf("Write create: %+v", w.Res)
	}

	td := step(t, "TodoWrite")
	if len(td.Todos) != 2 || td.Todos[1].Status != "in_progress" {
		t.Errorf("todos: %+v", td.Todos)
	}

	ag := step(t, "Agent")
	if ag.ShortInput() != "review panel" || ag.Out != "The panel looks fine." || ag.Res.Info["status"] != "completed" {
		t.Errorf("Agent: %q %q %v", ag.ShortInput(), ag.Out, ag.Res.Info)
	}
	if ag.Res.Info["prompt"] != "Review the panel code." {
		t.Errorf("short result strings kept: %v", ag.Res.Info)
	}

	th := step(t, ThinkingTool)
	if th.Text != "" || th.ID == "" || th.Duration() != 0 {
		t.Errorf("thought: %+v", th)
	}
}

func TestStepImage(t *testing.T) {
	s := step(t, "mcp__browser__take_screenshot")
	if len(s.Res.Images) != 1 {
		t.Fatalf("images: %+v", s.Res.Images)
	}
	im := s.Res.Images[0]
	if im.MediaType != "image/png" || im.W != 4 || im.H != 3 || im.Size == 0 {
		t.Errorf("image: %+v", im)
	}
	if s.Out != "Took a screenshot" {
		t.Errorf("out %q", s.Out)
	}
	b, err := ImageData("testdata/steps.jsonl", s.Res, im)
	if err != nil {
		t.Fatal(err)
	}
	c, f, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || f != "png" || c.Width != 4 {
		t.Errorf("decoded %v %s %v", c, f, err)
	}
}

func TestTailAndCut(t *testing.T) {
	if got := tail("a\nb\n"); got != "a\nb" {
		t.Errorf("tail = %q", got)
	}
	if got := cutUTF8("héllo", 2); got != "h" {
		t.Errorf("cutUTF8 split a rune: %q", got)
	}
	long := strings.Repeat("x", maxTailBytes*2)
	if got := tail(long); len(got) > maxTailBytes {
		t.Errorf("tail kept %d bytes", len(got))
	}
}

// A big patch keeps its counts but not all its lines.
func TestPatchCap(t *testing.T) {
	var r Result
	ls := make([]string, maxPatchLines+50)
	for i := range ls {
		ls[i] = "+x"
	}
	r.addHunk(Hunk{Lines: ls})
	r.addHunk(Hunk{Lines: []string{"-y"}})
	if r.Added != maxPatchLines+50 || r.Removed != 1 || len(r.Patch[0].Lines) != maxPatchLines || len(r.Patch[1].Lines) != 0 {
		t.Errorf("added %d removed %d kept %d+%d", r.Added, r.Removed, len(r.Patch[0].Lines), len(r.Patch[1].Lines))
	}
}

// Search still sees tool calls and output as text.
func TestSearchFindsSteps(t *testing.T) {
	res := Search([]Source{{"s", "testdata/steps.jsonl"}}, ParseQuery("exit code"), true, 10)
	if len(res) != 1 {
		t.Fatalf("output not searchable: %+v", res)
	}
	if res := Search([]Source{{"s", "testdata/steps.jsonl"}}, ParseQuery("go test"), true, 10); len(res) != 1 || res[0].Total != 1 {
		t.Fatalf("call not searchable: %+v", res)
	}
}
