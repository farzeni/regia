package layout

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/farzeni/regia/internal/claude"
)

// dirFixture mirrors the real tree that prompted this: a "regia" group whose
// agents all run in ~, a "so101" group mixing two subprojects, and an empty
// group with a name that matches no folder.
func dirFixture(t *testing.T) (*Layout, map[string]*claude.Agent, string) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, d := range []string{"Projects/regia", "Projects/so101/braccio", "Projects/so101/fly", "Git/tools"} {
		if err := os.MkdirAll(filepath.Join(h, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Unix(1_700_000_000, 0)
	mk := func(id, cwd string, n int) *claude.Agent {
		return &claude.Agent{ID: id, Name: "agent-" + id, Cwd: cwd, StartedAt: base.Add(time.Duration(n) * time.Minute), UpdatedAt: base.Add(time.Duration(n) * time.Minute)}
	}
	agents := map[string]*claude.Agent{
		"r1": mk("r1", h, 0),
		"r2": mk("r2", h, 1),
		"b1": mk("b1", h+"/Projects/so101/braccio", 2),
		"b2": mk("b2", h+"/Projects/so101/braccio/.claude/worktrees/x", 3),
		"b3": mk("b3", h+"/Projects/so101/braccio", 4),
		"f1": mk("f1", h+"/Projects/so101/fly", 5),
	}
	l := Load()
	l.Place(agents)
	regia := l.NewGroup("regia", "", agents)
	so := l.NewGroup("so101", "", agents)
	l.NewGroup("misc", "", agents)
	if err := l.Put([]string{"r1", "r2"}, regia, "", agents); err != nil {
		t.Fatal(err)
	}
	if err := l.Put([]string{"b1", "b2", "f1", "b3"}, so, "", agents); err != nil {
		t.Fatal(err)
	}
	return l, agents, h
}

func gid(l *Layout, title string) string {
	for id, t := range l.Groups {
		if t == title && id[:2] == "g:" {
			return id
		}
	}
	return ""
}

func TestSpawnDir(t *testing.T) {
	l, agents, h := dirFixture(t)
	regia, so, misc := gid(l, "regia"), gid(l, "so101"), gid(l, "misc")
	cases := []struct {
		start, dir, kind string
	}{
		{"", h, "home"},                                // nothing selected
		{"r1", h + "/Projects/regia", "folder"},        // agent in ~: the group's folder, not ~
		{regia, h + "/Projects/regia", "folder"},       // group named like a project folder
		{"f1", h + "/Projects/so101/fly", "agent"},     // next to fly: fly, whatever comes first
		{"b2", h + "/Projects/so101/braccio", "agent"}, // a worktree: its repository
		{so, h + "/Projects/so101/braccio", "agents"},  // most agents work in braccio
		{misc, h, "home"},                              // empty, no matching folder
		{"p:tools", h + "/Git/tools", "folder"},        // project group: ~/Git too
	}
	l.Groups["p:tools"] = "tools"
	for _, c := range cases {
		d, src := l.SpawnDir(c.start, agents)
		if d != c.dir || src.Kind != c.kind {
			t.Errorf("SpawnDir(%q) = %s (%s), want %s (%s)", c.start, d, src.Kind, c.dir, c.kind)
		}
	}

	// A saved directory beats guesses, and an agent in ~ inherits it; an
	// agent with its own directory still wins.
	l.Dirs[so] = h + "/Projects/so101"
	l.Dirs[regia] = h + "/Git/tools"
	for _, c := range []struct{ start, dir, kind string }{
		{so, h + "/Projects/so101", "group"},
		{"r2", h + "/Git/tools", "group"},
		{"f1", h + "/Projects/so101/fly", "agent"},
	} {
		if d, src := l.SpawnDir(c.start, agents); d != c.dir || src.Kind != c.kind {
			t.Errorf("with dirs: SpawnDir(%q) = %s (%s), want %s (%s)", c.start, d, src.Kind, c.dir, c.kind)
		}
	}

	// It survives a save; removing the group forgets it.
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	if got := Load().Dirs[so]; got != h+"/Projects/so101" {
		t.Fatalf("saved dir not reloaded: %q", got)
	}
	l.DeleteGroup(so, agents)
	if _, ok := l.Dirs[so]; ok {
		t.Fatal("DeleteGroup kept the group's dir")
	}
}

func TestSpawnDirTieGoesToRecent(t *testing.T) {
	l, agents, h := dirFixture(t)
	so := gid(l, "so101")
	agents["b3"].Cwd = h + "/Projects/so101/fly" // braccio 2, fly 2
	agents["f1"].UpdatedAt = agents["b2"].UpdatedAt.Add(time.Hour)
	if d, _ := l.SpawnDir(so, agents); d != h+"/Projects/so101/fly" {
		t.Fatalf("tie: got %s, want the most recently active fly", d)
	}
}

func TestGroupOfSavesSharedDir(t *testing.T) {
	l, agents, h := dirFixture(t)
	g, err := l.GroupOf("arm", []string{"b1", "b3"}, agents)
	if err != nil {
		t.Fatal(err)
	}
	if l.Dirs[g] != h+"/Projects/so101/braccio" {
		t.Fatalf("shared dir not saved: %q", l.Dirs[g])
	}
	g2, _ := l.GroupOf("mixed", []string{"b2", "f1"}, agents)
	if d, ok := l.Dirs[g2]; ok {
		t.Fatalf("mixed dirs saved %q", d)
	}
	g3, _ := l.GroupOf("home", []string{"r1", "r2"}, agents)
	if d, ok := l.Dirs[g3]; ok {
		t.Fatalf("home saved %q", d)
	}
}
