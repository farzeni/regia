package layout

import (
	"strings"
	"testing"
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/spawns"
)

func fixture(t *testing.T) (*Layout, map[string]*claude.Agent) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := claude.Home()
	base := time.Unix(1_700_000_000, 0)
	mk := func(id, cwd, state string, n int, fork string) *claude.Agent {
		return &claude.Agent{ID: id, Name: "agent-" + id, Cwd: cwd, State: state, StartedAt: base.Add(time.Duration(n) * time.Minute), ForkParent: fork}
	}
	agents := map[string]*claude.Agent{
		"coord": mk("coord", h+"/Projects/alpha", "busy", 0, ""),
		"w1":    mk("w1", h+"/Projects/alpha/.claude/worktrees/x", "blocked", 1, ""),
		"w2":    mk("w2", h+"/Projects/alpha", "done", 2, ""),
		"fork":  mk("fork", h+"/Projects/alpha", "idle", 3, "coord"),
		"b1":    mk("b1", h+"/Projects/beta", "idle", 4, ""),
	}
	l := Load()
	if !l.Place(agents) {
		t.Fatal("Place should change an empty layout")
	}
	return l, agents
}

func render(l *Layout, agents map[string]*claude.Agent, o ViewOpts) string {
	var b strings.Builder
	for _, r := range l.Flatten(agents, o) {
		b.WriteString(strings.Repeat(".", r.Depth) + r.ID + " ")
	}
	return strings.TrimSpace(b.String())
}

func vis(l *Layout, agents map[string]*claude.Agent, o ViewOpts) map[string]bool {
	v := map[string]bool{}
	for _, r := range l.Flatten(agents, o) {
		v[r.ID] = true
	}
	return v
}

func TestAutoPlacement(t *testing.T) {
	l, agents := fixture(t)
	all := ViewOpts{ShowDone: true}
	// worktree cwd folds into its repo's project; fork nests under its parent.
	if got, want := render(l, agents, all), "p:alpha .coord ..fork .w1 .w2 p:beta .b1"; got != want {
		t.Fatalf("tree\n got %q\nwant %q", got, want)
	}
	// finished sessions hidden by default
	if got, want := render(l, agents, ViewOpts{}), "p:alpha .coord ..fork .w1 p:beta .b1"; got != want {
		t.Fatalf("hide done\n got %q\nwant %q", got, want)
	}
	// Place is idempotent
	if l.Place(agents) {
		t.Fatal("second Place should be a no-op")
	}
}

func TestReorderNestMove(t *testing.T) {
	l, agents := fixture(t)
	o := ViewOpts{ShowDone: true}

	// move w1 up above coord
	if !l.Move("w1", -1, agents, vis(l, agents, o)) {
		t.Fatal("move up failed")
	}
	if got, want := render(l, agents, o), "p:alpha .w1 .coord ..fork .w2 p:beta .b1"; got != want {
		t.Fatalf("after move\n got %q\nwant %q", got, want)
	}
	// w2 under coord (coordinator): indent nests under previous visible sibling
	if err := l.Indent("w2", agents, vis(l, agents, o)); err != nil {
		t.Fatal(err)
	}
	if got, want := render(l, agents, o), "p:alpha .w1 .coord ..fork ..w2 p:beta .b1"; got != want {
		t.Fatalf("after indent\n got %q\nwant %q", got, want)
	}
	// outdent puts it right after its old parent
	if err := l.Outdent("w2", agents); err != nil {
		t.Fatal(err)
	}
	if got, want := render(l, agents, o), "p:alpha .w1 .coord ..fork .w2 p:beta .b1"; got != want {
		t.Fatalf("after outdent\n got %q\nwant %q", got, want)
	}
	// move b1 across groups, inside coord
	if err := l.Reparent("b1", "coord", "", agents); err != nil {
		t.Fatal(err)
	}
	if got, want := render(l, agents, o), "p:alpha .w1 .coord ..fork ..b1 .w2"; got != want {
		t.Fatalf("after reparent\n got %q\nwant %q", got, want)
	}
	// can't move a node inside its own descendant
	if err := l.Reparent("coord", "fork", "", agents); err == nil {
		t.Fatal("cycle should be refused")
	}
}

func TestGroupsAndPersistence(t *testing.T) {
	l, agents := fixture(t)
	o := ViewOpts{ShowDone: true}
	g := l.NewGroup("release", "p:alpha", agents)
	if err := l.Reparent("w1", g, "", agents); err != nil {
		t.Fatal(err)
	}
	l.Collapsed[g] = true
	l.Alias["coord"] = "Coordinator"
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	l2 := Load()
	rows := l2.Flatten(agents, o)
	if got, want := render(l2, agents, o), "p:alpha .coord ..fork .w2 "+g+" p:beta .b1"; got != want {
		t.Fatalf("reloaded (collapsed group)\n got %q\nwant %q", got, want)
	}
	for _, r := range rows {
		if r.ID == g && (r.Count != 1 || r.Blocked != 1 || !r.Collapsed) {
			t.Fatalf("group counts wrong: %+v", r)
		}
		if r.ID == "coord" && r.Title != "Coordinator" {
			t.Fatalf("alias not applied: %q", r.Title)
		}
	}
	// deleting the group lifts its child into its place
	l2.DeleteGroup(g, agents)
	if got, want := render(l2, agents, o), "p:alpha .coord ..fork .w2 w1 p:beta .b1"; got != want {
		t.Fatalf("after delete\n got %q\nwant %q", got, want)
	}
}

func TestFilterShowsAncestors(t *testing.T) {
	l, agents := fixture(t)
	if got, want := render(l, agents, ViewOpts{Filter: "agent-fork"}), "p:alpha .coord ..fork"; got != want {
		t.Fatalf("filter\n got %q\nwant %q", got, want)
	}
}

func TestNewAgentsAppearWithoutReshuffle(t *testing.T) {
	l, agents := fixture(t)
	o := ViewOpts{ShowDone: true}
	l.Move("b1", -1, agents, vis(l, agents, o)) // no-op: only child
	l.Move("w2", -1, agents, vis(l, agents, o))
	before := render(l, agents, o)
	agents["new"] = &claude.Agent{ID: "new", Name: "n", Cwd: claude.Home() + "/Projects/beta", State: "busy", StartedAt: time.Now()}
	l.Place(agents)
	after := render(l, agents, o)
	if !strings.HasPrefix(after, strings.TrimSuffix(before, "")[:len("p:alpha .coord ..fork .w2 .w1")]) || !strings.HasSuffix(after, ".b1 .new") {
		t.Fatalf("new agent placement\nbefore %q\n after %q", before, after)
	}
}

func TestPlaceUsesSpawnLinks(t *testing.T) {
	l, agents := fixture(t)
	all := ViewOpts{ShowDone: true}
	// The hook writes its link after both agents are already placed.
	l.Spawns = map[string]spawns.Link{"w1": {Parent: "coord", Child: "w1"}, "b1": {Parent: "gone", ParentSession: "coord-full", Child: "b1"}}
	agents["coord"].SessionID = "coord-full"
	if !l.Place(agents) {
		t.Fatal("re-link should change the layout")
	}
	// w1 by job id; b1 via its spawner's full session id (resumed session)
	if got, want := render(l, agents, all), "p:alpha .coord ..fork ..w1 ..b1 .w2"; got != want {
		t.Fatalf("tree\n got %q\nwant %q", got, want)
	}
	if l.Place(agents) {
		t.Fatal("second Place should be a no-op")
	}
	// Moving an agent by hand pins it: the link no longer pulls it back.
	if err := l.Outdent("w1", agents); err != nil {
		t.Fatal(err)
	}
	if l.Place(agents) || l.Parent["w1"] != "p:alpha" {
		t.Fatalf("pinned agent re-linked: parent %q", l.Parent["w1"])
	}
}

func TestTrail(t *testing.T) {
	l, agents := fixture(t)
	l.Alias["coord"] = "Coordinator"
	if got := strings.Join(l.Trail("fork", agents), " › "); got != "alpha › Coordinator" {
		t.Fatalf("trail of a fork: %q", got)
	}
	if got := l.Trail("p:alpha", agents); len(got) != 0 {
		t.Fatalf("a top group has no trail: %q", got)
	}
}

func TestOrphansLiftWhenCoordinatorRemoved(t *testing.T) {
	l, agents := fixture(t)
	delete(agents, "coord") // claude rm
	if got, want := render(l, agents, ViewOpts{ShowDone: true}), "p:alpha .w1 .w2 .fork p:beta .b1"; got != want {
		t.Fatalf("tree\n got %q\nwant %q", got, want)
	}
}
