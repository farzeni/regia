package group

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
	"github.com/farzeni/regia/internal/spawns"
)

// A tree with every kind of relationship:
//
//	g:1 "dashboards"            hand-made group
//	  coord                     a coordinator in it
//	    w1, w2                  its workers, placed by regia
//	    (w3)                    a worker regia hasn't placed yet: only in spawns.jsonl
//	  a1                        coord's sibling
//	p:x                         an automatic project group
//	  lonely                    in no group
//	  pp                        spawned by coord, but moved to p:x by hand (pinned)
func fixture(t *testing.T) *Tree {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("REGIA_GROUP", "")
	l := layout.Load()
	l.Groups = map[string]string{"g:1": "dashboards", "p:x": "x"}
	l.Parent = map[string]string{"g:1": "", "p:x": "", "coord": "g:1", "a1": "g:1", "w1": "coord", "w2": "coord", "lonely": "p:x", "pp": "p:x"}
	l.Order = map[string][]string{"": {"g:1", "p:x"}, "g:1": {"coord", "a1"}, "coord": {"w1", "w2"}, "p:x": {"lonely", "pp"}}
	l.Pinned = map[string]bool{"pp": true}
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	return &Tree{L: layout.Load(), Spawns: map[string]spawns.Link{
		"w3": {Parent: "coord", Child: "w3"},
		"pp": {Parent: "coord", Child: "pp"},
	}}
}

var names = func(id string) string {
	return map[string]string{"coord": "orchestrator", "w1": "KPI queries", "w2": "layout", "w3": "export", "a1": "copy"}[id]
}

func TestResolve(t *testing.T) {
	tr := fixture(t)
	w1 := tr.Resolve("w1")
	if !slices.Equal(w1.Chain, []string{"coord", "g:1"}) || w1.Family || w1.Nearest() != "coord" {
		t.Fatalf("w1: %+v", w1)
	}
	if got := tr.Siblings(w1); !slices.Equal(got, []string{"w2", "w3"}) {
		t.Fatalf("w1 siblings (w3 only known from spawns): %v", got)
	}
	coord := tr.Resolve("coord")
	if !slices.Equal(coord.Chain, []string{"g:1"}) || !coord.Family || coord.Nearest() != "g:1" {
		t.Fatalf("coord: %+v", coord)
	}
	if !slices.Equal(coord.Visible(), []string{"coord", "g:1"}) || !slices.Equal(tr.Children(coord), []string{"w1", "w2", "w3"}) {
		t.Fatalf("coord visible %v children %v", coord.Visible(), tr.Children(coord))
	}
	if got := tr.Siblings(coord); !slices.Equal(got, []string{"a1"}) {
		t.Fatalf("coord siblings: %v", got)
	}
	if tr.Resolve("lonely").Grouped() {
		t.Fatal("a project group is not a group")
	}
	if tr.Resolve("pp").Grouped() {
		t.Fatal("moved by hand into a project group: the spawn link no longer counts")
	}
	if n := tr.Members("g:1"); n != 5 {
		t.Fatalf("g:1 reaches %d agents", n)
	}
}

func TestMuted(t *testing.T) {
	tr := fixture(t)
	tr.L.Muted = map[string]bool{"g:1": true}
	if w1 := tr.Resolve("w1"); !slices.Equal(w1.Chain, []string{"coord"}) {
		t.Fatalf("a muted group cuts the chain: %+v", w1)
	}
	if c := tr.Resolve("coord"); len(c.Chain) != 0 || !c.Family || c.Nearest() != "coord" {
		t.Fatalf("coord under a muted group keeps its own family: %+v", c)
	}
	tr.L.Muted = map[string]bool{"coord": true}
	if tr.Resolve("w1").Grouped() {
		t.Fatal("under a muted coordinator: not grouped")
	}
	if c := tr.Resolve("coord"); c.Family {
		t.Fatal("a muted coordinator has no family")
	}
	if got := tr.Siblings(tr.Resolve("a1")); len(got) != 0 {
		t.Fatalf("a muted coordinator isn't listed as a sibling: %v", got)
	}
}

func TestCheckFlow(t *testing.T) {
	tr := fixture(t)
	t0 := time.Date(2026, 11, 14, 10, 0, 0, 0, time.Local)

	if msg := Check(tr, "lonely", "PostToolUse", t0, names); msg != "" {
		t.Fatalf("not grouped: silent, got %q", msg)
	}

	AddNote("g:1", Note{By: "a1", Name: "copy", Text: "prod DB is read-only"})
	msg := Check(tr, "w1", "PostToolUse", t0, names)
	for _, want := range []string{`working under coordinator "orchestrator"`, "blackboard (1 unread note on it)", "regia context", "regia note", "something new"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("intro lacks %q:\n%s", want, msg)
		}
	}
	if n := len(strings.Fields(msg)); n > 120 {
		t.Fatalf("intro is %d words: keep it short", n)
	}
	if msg := Check(tr, "w1", "PostToolUse", t0.Add(time.Second), names); msg != "" {
		t.Fatalf("nothing new: silent, got %q", msg)
	}

	AddNote("coord", Note{By: "w2", Name: "layout", Text: "gauges go on top"})
	AddNote("coord", Note{By: "w1", Name: "KPI queries", Text: "my own note"})
	if msg := Check(tr, "w1", "PostToolUse", t0.Add(30*time.Second), names); msg != "" {
		t.Fatalf("within the throttle: silent, got %q", msg)
	}
	msg = Check(tr, "w1", "PostToolUse", t0.Add(90*time.Second), names)
	if msg != "[regia] 1 new note in your group from layout — `regia context` to read." {
		t.Fatalf("news: %q", msg)
	}
	if msg := Check(tr, "w1", "PostToolUse", t0.Add(5*time.Minute), names); msg != "" {
		t.Fatalf("news is told once: %q", msg)
	}

	// a user message checks right away; notes flow down from the group above
	AddNote("g:1", Note{By: FromUser, Name: "you", Text: "ship by friday"})
	if msg := Check(tr, "w1", "UserPromptSubmit", t0.Add(5*time.Minute+time.Second), names); !strings.Contains(msg, "1 new note in your group from the user (in regia)") {
		t.Fatalf("user message: %q", msg)
	}
	// a note the user wrote in regia must not read "you:" to the agent
	if c := Context(tr, tr.Resolve("w1"), nil, false, t0); !strings.Contains(c, "the user (in regia): ship by friday") {
		t.Fatalf("user note in context:\n%s", c)
	}

	// read with regia context: what was there is no news any more
	AddNote("coord", Note{By: "w3", Name: "export", Text: "csv uses ;"})
	MarkRead(tr, tr.Resolve("w1"))
	if msg := Check(tr, "w1", "UserPromptSubmit", t0.Add(6*time.Minute), names); msg != "" {
		t.Fatalf("after reading: %q", msg)
	}

	// moved into the group directly
	tr.L.Parent["w1"] = "g:1"
	msg = Check(tr, "w1", "UserPromptSubmit", t0.Add(7*time.Minute), names)
	if !strings.Contains(msg, `you were moved: you're now in group "dashboards" with`) {
		t.Fatalf("moved: %q", msg)
	}
}

func TestHandleHookAndSwitches(t *testing.T) {
	fixture(t) // HandleHook loads the tree itself: w3 is unknown there (no spawns.jsonl)
	run := func(event string) string {
		var out bytes.Buffer
		in := fmt.Sprintf(`{"session_id":"0123456789abcdef","hook_event_name":%q,"tool_name":"Bash","tool_response":{"stdout":"`+strings.Repeat("x", 2<<20)+`"}}`, event)
		HandleHook(strings.NewReader(in), &out, "/home/u/.claude/jobs/w2", time.Now())
		return out.String()
	}
	var got struct {
		HookSpecificOutput struct{ HookEventName, AdditionalContext string }
	}
	out := run("PostToolUse")
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("hook output %q: %v", out, err)
	}
	if got.HookSpecificOutput.HookEventName != "PostToolUse" || !strings.Contains(got.HookSpecificOutput.AdditionalContext, "You are part of a group") {
		t.Fatalf("hook output: %+v", got)
	}
	if out := run("PostToolUse"); out != "" {
		t.Fatalf("second call: %q", out)
	}

	// turned off: silent even with news
	AddNote("coord", Note{By: "w1", Name: "KPI queries", Text: "x"})
	if err := SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if Enabled() || run("UserPromptSubmit") != "" {
		t.Fatal("regia group off should silence the hook")
	}
	SetEnabled(true)
	t.Setenv("REGIA_GROUP", "off")
	if Enabled() || run("UserPromptSubmit") != "" {
		t.Fatal("REGIA_GROUP=off should silence the hook")
	}
	t.Setenv("REGIA_GROUP", "")
	if out := run("UserPromptSubmit"); !strings.Contains(out, "1 new note") || !strings.Contains(out, `"hookEventName":"UserPromptSubmit"`) {
		t.Fatalf("back on: %q", out)
	}
}

func TestContext(t *testing.T) {
	tr := fixture(t)
	now := time.Now()
	agents := map[string]*claude.Agent{
		"coord": {ID: "coord", Name: "orchestrator", State: "busy", Detail: "spawned 3 workers", UpdatedAt: now.Add(-2 * time.Minute)},
		"w1":    {ID: "w1", Name: "KPI queries", State: "busy", StartedAt: now.Add(-4 * time.Minute)},
		"w2":    {ID: "w2", Name: "layout", State: "done", Detail: "moved gauges\nto the top", UpdatedAt: now.Add(-10 * time.Minute)},
	}
	for i := 1; i <= 12; i++ {
		AddNote("coord", Note{At: now, By: "w2", Name: "layout", Text: fmt.Sprintf("note %d", i)})
	}
	AddNote("g:1", Note{At: now, By: "w1", Name: "KPI queries", Text: "mine"})
	out := Context(tr, tr.Resolve("w1"), agents, false, now)
	for _, want := range []string{
		`regia · you are "KPI queries" (w1)`,
		`coordinator: ◆ orchestrator — working 2m — spawned 3 workers`,
		"siblings (2):",
		"✓ layout — done 10m — moved gauges to the top",
		"· w3 — not listed any more",
		"[orchestrator]", "(+2 older: regia context --all)", "layout: note 12",
		"[dashboards]", "you: mine",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("context lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "note 2\n") {
		t.Fatalf("older notes should be cut:\n%s", out)
	}
	if all := Context(tr, tr.Resolve("w1"), agents, true, now); !strings.Contains(all, "layout: note 1\n") {
		t.Fatalf("--all shows every note:\n%s", all)
	}
	if out := Context(tr, tr.Resolve("lonely"), agents, false, now); !strings.Contains(out, "not in a group") {
		t.Fatalf("lonely: %s", out)
	}
}

func TestAddNote(t *testing.T) {
	fixture(t)
	if err := AddNote("g:1", Note{By: "w1", Text: "two\nlines   here"}); err != nil {
		t.Fatal(err)
	}
	AddNote("g:1", Note{By: "w1", Text: strings.Repeat("é", 900)})
	if AddNote("g:1", Note{By: "w1", Text: "  \n "}) == nil || AddNote("", Note{Text: "x"}) == nil {
		t.Fatal("empty note or no group should fail")
	}
	ns := ReadNotes("g:1")
	if len(ns) != 2 || ns[0].Text != "two lines here" || len([]rune(ns[1].Text)) != maxNote+1 {
		t.Fatalf("notes %+v", ns)
	}
	if _, err := os.Stat(filepath.Join(notesDir(), "g_1.jsonl")); err != nil {
		t.Fatal(err)
	}
	DeleteNotes("g:1")
	if ReadNotes("g:1") != nil {
		t.Fatal("not deleted")
	}
}

func TestSelfID(t *testing.T) {
	if SelfID("/x/jobs/ab12cd34", "ffff") != "ab12cd34" || SelfID("", "0123456789") != "s:01234567" || SelfID("", "") != "" {
		t.Fatal("SelfID")
	}
}
