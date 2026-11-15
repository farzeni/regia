package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// dirModel: testModel with a fake home holding two project folders, and a
// spawnAgent stub that records the directory.
func dirModel(t *testing.T) (m *model, home string, gotDir *string) {
	home = t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{"Projects/regia", "Projects/spalla/sub"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m = testModel(t)
	var got string
	orig := spawnAgent
	t.Cleanup(func() { spawnAgent = orig })
	spawnAgent = func(dir, task string) (string, error) {
		got = dir
		return "started dddd4444\n", nil
	}
	return m, home, &got
}

func ctrl(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func screen(m *model) string { return ansi.Strip(m.View()) }

func TestPickDirForOneTask(t *testing.T) {
	m, home, got := dirModel(t)
	goTo(t, m, "aaaa1111")
	agentDir := m.agents["aaaa1111"].Cwd
	if !strings.Contains(screen(m), tildify(agentDir)+" · ctrl+o ─╯") {
		t.Fatalf("the box's border should say where the task runs:\n%s", screen(m))
	}

	m.Update(ctrl(tea.KeyCtrlO))
	if m.mode != modeDir || len(m.dirPick.items) == 0 || m.dirPick.items[0].path != agentDir {
		t.Fatalf("picker should open with the current dir first: mode %v items %+v", m.mode, m.dirPick)
	}
	typeText(m, "spal")
	if it := m.dirPick.items; len(it) != 1 || it[0].path != home+"/Projects/spalla" {
		t.Fatalf("filter spal: %+v", it)
	}
	m.Update(tkey("tab")) // into spalla: itself, then sub
	if it := m.dirPick.items; m.dirPick.query != "~/Projects/spalla/" || len(it) != 2 || it[1].path != home+"/Projects/spalla/sub" {
		t.Fatalf("tab into folder: %q %+v", m.dirPick.query, it)
	}
	m.Update(tkey("down"))
	m.Update(tkey("enter"))
	if m.mode != modeNormal || m.draftDir != home+"/Projects/spalla/sub" || !m.typing {
		t.Fatalf("pick: mode %v draftDir %q typing %v", m.mode, m.draftDir, m.typing)
	}
	if !strings.Contains(screen(m), " ~/Projects/spalla/sub picked · ctrl+o ─╯") {
		t.Fatalf("box should show the picked dir:\n%s", screen(m))
	}

	// moving the cursor keeps the choice; sending uses it, then forgets it
	m.Update(tkey("up"))
	typeText(m, "do it")
	send(m, tkey("enter"))
	if *got != home+"/Projects/spalla/sub" {
		t.Fatalf("spawned in %q", *got)
	}
	if m.draftDir != "" {
		t.Fatalf("the pick should last one task, still %q", m.draftDir)
	}
	if _, ok := m.layout.Dirs[m.nearestGroup("aaaa1111")]; ok {
		t.Fatal("enter must not save a group dir")
	}
}

func TestPickTypedPathAndEsc(t *testing.T) {
	m, home, _ := dirModel(t)
	m.Update(ctrl(tea.KeyCtrlO))
	m.Update(tkey("esc"))
	if m.mode != modeNormal || m.draftDir != "" || !m.typing {
		t.Fatalf("esc: mode %v draftDir %q typing %v", m.mode, m.draftDir, m.typing)
	}
	m.Update(ctrl(tea.KeyCtrlO))
	typeText(m, "~/Projects/nope")
	m.Update(tkey("enter"))
	if m.mode != modeDir || !strings.Contains(m.status, "no such directory") {
		t.Fatalf("missing path: mode %v status %q", m.mode, m.status)
	}
	m.Update(ctrl(tea.KeyCtrlU))
	typeText(m, home+"/Projects/regia")
	m.Update(tkey("enter"))
	if m.draftDir != home+"/Projects/regia" {
		t.Fatalf("typed path: %q", m.draftDir)
	}
}

func TestCtrlSSavesForGroup(t *testing.T) {
	m, home, got := dirModel(t)
	goTo(t, m, "aaaa1111")
	g := m.nearestGroup("aaaa1111")
	m.Update(ctrl(tea.KeyCtrlO))
	typeText(m, "regia")
	m.Update(ctrl(tea.KeyCtrlS))
	if m.layout.Dirs[g] != home+"/Projects/regia" || m.draftDir != home+"/Projects/regia" {
		t.Fatalf("ctrl+s: group dir %q draft %q", m.layout.Dirs[g], m.draftDir)
	}
	// the agent's own dir still wins next to it; the group gets the saved one
	m.clearDraft()
	goTo(t, m, g)
	typeText(m, "x")
	send(m, tkey("enter"))
	if *got != home+"/Projects/regia" {
		t.Fatalf("new agent in the group started in %q", *got)
	}
}

func TestWSetsAndClearsGroupDir(t *testing.T) {
	m, home, _ := dirModel(t)
	m.Update(tkey("esc")) // command keys
	goTo(t, m, "bbbb2222")
	g := m.nearestGroup("bbbb2222")
	m.Update(tkey("w")) // on an agent: its group
	if m.mode != modeDir || m.dirPick.group != g {
		t.Fatalf("w on an agent should edit its group's dir: %+v", m.dirPick)
	}
	if !strings.Contains(screen(m), "dir for group ") {
		t.Fatalf("prompt:\n%s", screen(m))
	}
	typeText(m, "~/Projects/regia")
	m.Update(tkey("enter"))
	if m.layout.Dirs[g] != home+"/Projects/regia" {
		t.Fatalf("w: saved %q", m.layout.Dirs[g])
	}
	goTo(t, m, g)
	if s := screen(m); !strings.Contains(s, "new agents start in ~/Projects/regia · saved") {
		t.Fatalf("group panel should show the saved dir:\n%s", s)
	}

	// w again starts from the saved dir; the first item forgets it
	m.Update(tkey("w"))
	if m.dirPick.query != "~/Projects/regia" {
		t.Fatalf("w should start from the saved dir: %q", m.dirPick.query)
	}
	m.Update(ctrl(tea.KeyCtrlU))
	if !m.dirPick.items[0].clear {
		t.Fatalf("first item should forget the saved dir: %+v", m.dirPick.items[0])
	}
	m.Update(tkey("enter"))
	if _, ok := m.layout.Dirs[g]; ok {
		t.Fatal("clear kept the dir")
	}
	if s := screen(m); !strings.Contains(s, "guessed from 2 agents") {
		t.Fatalf("group panel should say the dir is guessed again:\n%s", s)
	}
}

func TestMissingDirIsRefused(t *testing.T) {
	m, _, got := dirModel(t)
	gone := filepath.Join(t.TempDir(), "gone")
	m.draftDir = gone
	typeText(m, "task")
	send(m, tkey("enter"))
	if *got != "" || m.draft() != "task" || !strings.Contains(m.status, "no longer exists") {
		t.Fatalf("missing dir: spawned in %q, draft %q, status %q", *got, m.draft(), m.status)
	}
	if !strings.Contains(screen(m), "gone missing · ctrl+o ─╯") {
		t.Fatalf("box should flag the missing dir:\n%s", screen(m))
	}
}

func TestWOutsideGroup(t *testing.T) {
	m, _, _ := dirModel(t)
	m.Update(tkey("esc"))
	m.cursor, m.cursorID = 0, ""
	m.rows = nil
	m.Update(tkey("w"))
	if m.mode != modeNormal || !strings.Contains(m.status, "not in a group") {
		t.Fatalf("w with nothing selected: mode %v status %q", m.mode, m.status)
	}
}
