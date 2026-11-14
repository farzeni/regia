package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/layout"
)

// readyFixture: a Claude config dir with job state files, and a model over
// four agents: an old done one, a question, an old question, a busy one.
type readyFixture struct {
	t   *testing.T
	m   *model
	dir string
}

func (f *readyFixture) job(id, state string, updated time.Time) {
	d := filepath.Join(f.dir, "jobs", id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		f.t.Fatal(err)
	}
	s := fmt.Sprintf(`{"state":%q,"detail":"did %s","updatedAt":%q}`, state, id, updated.UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(d, "state.json"), []byte(s), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *readyFixture) list(states map[string]string) {
	var js []string
	for _, id := range []string{"done0001", "ask00001", "oldask01", "busy0001"} {
		js = append(js, fmt.Sprintf(`{"id":%q,"cwd":"/x/Projects/p","kind":"background","name":%q,"state":%q,"startedAt":1794645285576}`, id, id, states[id]))
	}
	f.m.setAgents([]byte("[" + strings.Join(js, ",") + "]"))
}

var baseStates = map[string]string{"done0001": "done", "ask00001": "blocked", "oldask01": "blocked", "busy0001": "working"}

func newReady(t *testing.T) *readyFixture {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	f := &readyFixture{t: t, dir: t.TempDir()}
	t.Setenv("CLAUDE_CONFIG_DIR", f.dir)
	now := time.Now()
	f.job("done0001", "done", now.Add(-time.Hour))
	f.job("ask00001", "blocked", now.Add(-10*time.Minute))
	f.job("oldask01", "blocked", now.Add(-50*time.Hour))
	f.job("busy0001", "working", now)
	f.m = newModel()
	f.m.w, f.m.h = 140, 30
	f.list(baseStates)
	return f
}

func treeIDs(m *model) []string {
	var ids []string
	for _, r := range m.rows {
		if r.Agent != nil {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

func TestDoneBeforeFirstRunCountsAsSeen(t *testing.T) {
	f := newReady(t)
	if s := f.m.agents["done0001"].State; s != "done" {
		t.Fatalf("an agent already done when regia first records views is %q, want done", s)
	}
	if slices.Contains(treeIDs(f.m), "done0001") {
		t.Fatal("seen done agents stay hidden")
	}
	if l := layout.Load(); l.Seen["done0001"].IsZero() {
		t.Fatal("first-run seen not saved")
	}
}

func TestFinishedTurnIsReadyUntilYouLook(t *testing.T) {
	f := newReady(t)
	m := f.m
	// the busy session finishes a turn: Claude Code says done
	f.job("busy0001", "done", time.Now())
	st := maps(baseStates, "busy0001", "done")
	f.list(st)
	a := m.agents["busy0001"]
	if a.State != "ready" || !a.YourTurn() {
		t.Fatalf("a finished turn you haven't seen: state %q", a.State)
	}
	if !slices.Contains(treeIDs(m), "busy0001") {
		t.Fatalf("ready agent hidden like done: %v", treeIDs(m))
	}
	if head := ansi.Strip(m.header()); !strings.Contains(head, "● 1 need you") || !strings.Contains(head, "✓ 1 ready") {
		t.Fatalf("header %q (the stale question must not count)", head)
	}
	// inbox: the question, then the result to read, then the stale question
	m.Update(tkey("tab"))
	if got := rowIDs(m); !slices.Equal(got, []string{"ask00001", "busy0001", "oldask01"}) {
		t.Fatalf("inbox order %v", got)
	}
	// moving over it doesn't count as looking; staying on it does
	goTo(t, m, "busy0001")
	t0 := time.Now()
	m.dwell(t0)
	m.dwell(t0.Add(time.Second))
	if a.State != "ready" {
		t.Fatal("seen after 1s")
	}
	m.dwell(t0.Add(seenAfter))
	if a.State != "done" {
		t.Fatalf("still %q after %v selected", a.State, seenAfter)
	}
	m.rebuild()
	if !slices.Contains(rowIDs(m), "busy0001") {
		t.Fatal("the agent you're reading vanished from under the cursor")
	}
	m.moveCursor(-1)
	m.rebuild()
	if slices.Contains(rowIDs(m), "busy0001") {
		t.Fatal("seen done agent still in the inbox after moving away")
	}
	// the next refresh keeps it done (seen is saved)
	f.list(st)
	if s := m.agents["busy0001"].State; s != "done" {
		t.Fatalf("after reload: %q", s)
	}
	// it works again and finishes another turn: ready again
	f.job("busy0001", "done", t0.Add(seenAfter+time.Second)) // after you looked (dwell ran on t0+2s)
	f.list(st)
	if s := m.agents["busy0001"].State; s != "ready" {
		t.Fatalf("a new finished turn: %q, want ready", s)
	}
}

func TestAttachMarksSeen(t *testing.T) {
	f := newReady(t)
	f.job("busy0001", "done", time.Now())
	f.list(maps(baseStates, "busy0001", "done"))
	f.m.Update(attachDoneMsg{id: "busy0001"})
	if s := f.m.agents["busy0001"].State; s != "done" {
		t.Fatalf("after attaching: %q", s)
	}
}

func TestStaleQuestion(t *testing.T) {
	f := newReady(t)
	m := f.m
	old, fresh := m.agents["oldask01"], m.agents["ask00001"]
	if !old.Stale || old.YourTurn() || fresh.Stale || !fresh.YourTurn() {
		t.Fatalf("stale: old %v fresh %v", old.Stale, fresh.Stale)
	}
	if !slices.Contains(treeIDs(m), "oldask01") {
		t.Fatal("stale questions stay in the tree")
	}
	if g, c := glyph(old); g != "●" || c != cIdle {
		t.Fatalf("stale glyph %s %v", g, c)
	}
	goTo(t, m, "oldask01")
	if !strings.Contains(ansi.Strip(m.View()), "WAITING ON YOU · STALE") {
		t.Fatal("panel doesn't say stale")
	}
}

func maps(base map[string]string, k, v string) map[string]string {
	out := map[string]string{}
	for a, b := range base {
		out[a] = b
	}
	out[k] = v
	return out
}
