package group

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
)

// toolsFixture is fixture's tree with every agent listed, plus "boss", an
// agent at the top that the user may grant things to.
func toolsFixture(t *testing.T) {
	t.Helper()
	fixture(t)
	listed := map[string]string{"coord": "orchestrator", "w1": "KPI queries", "w2": "layout", "a1": "copy", "lonely": "lonely", "pp": "pp", "boss": "boss"}
	old := Agents
	Agents = func() map[string]*claude.Agent {
		out := map[string]*claude.Agent{}
		for id, n := range listed {
			out[id] = &claude.Agent{ID: id, Name: n, State: "idle"}
		}
		return out
	}
	t.Cleanup(func() { Agents = old })
	layout.Update(func(l *layout.Layout) error { l.Parent["boss"] = ""; l.Pinned["boss"] = true; return nil })
}

func grant(t *testing.T, id, scope string) {
	t.Helper()
	if _, _, err := layout.Update(func(l *layout.Layout) error { l.Grants[id] = scope; return nil }); err != nil {
		t.Fatal(err)
	}
}

func refused(t *testing.T, err error, why string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), why) {
		t.Fatalf("want a refusal saying %q, got %v", why, err)
	}
}

func ids(n Node) []string {
	var out []string
	for _, c := range n.Children {
		out = append(out, c.ID)
	}
	return out
}

func TestWorkerReadsOnly(t *testing.T) {
	toolsFixture(t)
	me, err := Whoami("w1")
	if err != nil || me.Role != RoleWorker || me.Group != "coord" || me.Scope != "" {
		t.Fatalf("w1: %+v %v", me, err)
	}
	tr, _ := TreeOf("w1", TreeOpts{})
	if tr.ID != "coord" || strings.Join(ids(tr), ",") != "w1,w2" {
		t.Fatalf("a worker reads its group: %+v", tr)
	}
	if _, err := AgentInfo("w1", "w2"); err != nil {
		t.Fatalf("a sibling is readable: %v", err)
	}
	_, err = AgentInfo("w1", "lonely")
	refused(t, err, "outside what you may read")
	_, err = Move("w1", []string{"w2"}, "coord", "")
	refused(t, err, "call claim")
	if len(Actions()) != 0 {
		t.Fatal("a refused action is not recorded")
	}
}

func TestCoordinatorOrganizesItsAgents(t *testing.T) {
	toolsFixture(t)
	me, _ := Whoami("coord")
	if me.Role != RoleCoordinator || me.Scope != "coord" || me.Group != "g:1" {
		t.Fatalf("coord: %+v", me)
	}

	s, err := CreateGroup("coord", "frontend", "", []string{"w1"})
	if err != nil || !strings.Contains(s, `created group "frontend"`) {
		t.Fatalf("create: %q %v", s, err)
	}
	tr, _ := TreeOf("coord", TreeOpts{})
	if len(tr.Children) != 2 || tr.Children[1].Title != "frontend" || strings.Join(ids(tr.Children[1]), ",") != "w1" {
		t.Fatalf("the group is under coord with w1 in it: %+v", tr)
	}
	gid := tr.Children[1].ID
	if sub, err := TreeOf("coord", TreeOpts{Under: gid, Brief: true}); err != nil || sub.ID != gid || len(sub.Children) != 1 ||
		sub.Children[0].ID != "w1" || sub.Children[0].State == "" || sub.Children[0].Question != "" || sub.Children[0].Updated != "" {
		t.Fatalf("under the group, brief: %+v %v", sub, err)
	}
	if _, err := TreeOf("w2", TreeOpts{Under: "nope"}); err == nil {
		t.Fatal("under something out of reach is refused")
	}
	if _, err := Move("coord", []string{"w2"}, gid, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := RenameGroup("coord", gid, "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := Archive("coord", []string{"w2"}, true); err != nil || !layout.Load().IsArchived("w2") {
		t.Fatalf("archive: %v", err)
	}

	_, err = Move("coord", []string{"a1"}, gid, "")
	refused(t, err, "outside what you may organize (the agents under you)")
	_, err = Move("coord", []string{"w1"}, "g:1", "")
	refused(t, err, "outside what you may organize")
	_, err = Move("coord", []string{"w1"}, "", "")
	refused(t, err, "top level is outside")
	_, err = Archive("coord", []string{"coord"}, true)
	refused(t, err, "yourself")
	_, err = AgentInfo("coord", "a1")
	refused(t, err, "outside what you may read")

	// every change is in the log, newest last, and can be undone in order
	as := Actions()
	if len(as) != 4 || as[0].Tool != "create_group" || as[3].Summary != "archived 1 agent" || as[3].By != "coord" {
		t.Fatalf("actions: %+v", as)
	}
	if err := Undo(as[3]); err != nil || layout.Load().IsArchived("w2") {
		t.Fatalf("undo archive: %v", err)
	}
	refused(t, Undo(as[3]), "already undone")
	if err := Undo(as[2]); err != nil || layout.Load().Groups[gid] != "frontend" {
		t.Fatalf("undo rename: %v", err)
	}
	// undoing the create while w2's move into the group still stands is refused
	refused(t, Undo(as[0]), "changed again since")
}

func TestOrchestratorGrants(t *testing.T) {
	toolsFixture(t)
	if me, _ := Whoami("boss"); me.Role != RoleWorker {
		t.Fatalf("no grant, no children: a worker, got %+v", me)
	}
	grant(t, "boss", "g:1")
	if me, _ := Whoami("boss"); me.Role != RoleOrchestrator || me.ScopeTitle != `group "dashboards"` {
		t.Fatalf("granted g:1: %+v", me)
	}
	if _, err := Move("boss", []string{"a1"}, "coord", ""); err != nil {
		t.Fatalf("a1 into coord, both in g:1: %v", err)
	}
	_, err := Move("boss", []string{"lonely"}, "g:1", "")
	refused(t, err, "outside what you may organize")

	// a muted part is out of reach
	layout.Update(func(l *layout.Layout) error { l.Muted["coord"] = true; return nil })
	_, err = Archive("boss", []string{"w1"}, true)
	refused(t, err, "muted")

	grant(t, "boss", Everything)
	if _, err := Move("boss", []string{"lonely"}, "g:1", ""); err != nil {
		t.Fatalf("whole tree: %v", err)
	}
	if _, err := Archive("boss", []string{"w1"}, true); err != nil {
		t.Fatalf("whole tree reaches muted parts: %v", err)
	}
	if _, err := CreateGroup("boss", "triage", "", nil); err != nil {
		t.Fatalf("a top-level group: %v", err)
	}
	tr, _ := TreeOf("boss", TreeOpts{})
	if tr.Title != "regia" || !strings.Contains(strings.Join(ids(tr), ","), "g:1") {
		t.Fatalf("whole tree from the top: %+v", tr)
	}
	// a grant to a group that is gone gives nothing
	grant(t, "boss", "g:gone")
	if me, _ := Whoami("boss"); me.Role != RoleWorker {
		t.Fatalf("grant to a missing group: %+v", me)
	}
}

func TestToolsOffAndUnknown(t *testing.T) {
	toolsFixture(t)
	t.Setenv("REGIA_GROUP", "off")
	if _, err := TreeOf("w1", TreeOpts{}); !errors.Is(err, ErrOff) {
		t.Fatalf("off: %v", err)
	}
	if _, err := Move("coord", []string{"w1"}, "coord", ""); !errors.Is(err, ErrOff) {
		t.Fatalf("off: %v", err)
	}
	t.Setenv("REGIA_GROUP", "")
	_, err := Whoami("")
	refused(t, err, "can't tell which agent")
	_, err = RenameGroup("coord", "coord", "x")
	refused(t, err, "not a group")
	_, err = AgentInfo("coord", "nobody")
	refused(t, err, "no agent")
}

// The hook tells agents that may organize the tree how; workers aren't told.
func TestIntroMentionsTools(t *testing.T) {
	toolsFixture(t)
	tr := LoadTree()
	now := time.Now()
	if msg := Check(tr, "w1", "SessionStart", now, names); !strings.Contains(msg, "When the user asks you to look at or organize") {
		t.Fatalf("a worker is told the tools are there when asked:\n%s", msg)
	}
	if msg := Check(tr, "coord", "SessionStart", now, names); !strings.Contains(msg, "organize the agents under you") || !strings.Contains(msg, "regia:organize") {
		t.Fatalf("a coordinator is told:\n%s", msg)
	}
	if msg := Check(tr, "boss", "SessionStart", now, names); msg != "" {
		t.Fatalf("boss is in no group and has no grant: %q", msg)
	}
	grant(t, "boss", "g:1")
	tr = LoadTree()
	msg := Check(tr, "boss", "SessionStart", now, names)
	if !strings.Contains(msg, "You run under regia") || !strings.Contains(msg, `granted you group "dashboards"`) || strings.Contains(msg, "moved to another group") {
		t.Fatalf("a granted agent outside any group:\n%s", msg)
	}
}
