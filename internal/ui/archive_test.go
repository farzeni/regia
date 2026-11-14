package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/layout"
)

// archiveModel: project one has w1 (waiting), w2 (waiting), busy1 (working);
// project two has f1 (failed). Command keys, everything unfolded.
func archiveModel(t *testing.T) (*model, func(states map[string]string)) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	d1, d2 := t.TempDir()+"/Projects/one", t.TempDir()+"/Projects/two"
	set := func(m *model, st map[string]string) {
		get := func(id, def string) string {
			if s, ok := st[id]; ok {
				return s
			}
			return def
		}
		m.setAgents([]byte(`[
		 {"id":"aaaa0001","cwd":"` + d1 + `","kind":"background","name":"w1","status":"idle","state":"` + get("aaaa0001", "blocked") + `","startedAt":1794645285576},
		 {"id":"aaaa0002","cwd":"` + d1 + `","kind":"background","name":"w2","status":"idle","state":"` + get("aaaa0002", "blocked") + `","startedAt":1794645285577},
		 {"id":"aaaa0003","cwd":"` + d1 + `","kind":"background","name":"busy1","status":"busy","state":"working","startedAt":1794645285578},
		 {"id":"bbbb0001","cwd":"` + d2 + `","kind":"background","name":"f1","status":"idle","state":"failed","startedAt":1794645285579}]`))
	}
	m := newModel()
	m.w, m.h = 140, 30
	set(m, nil)
	return m, func(st map[string]string) { set(m, st) }
}

func goTo(t *testing.T, m *model, id string) {
	t.Helper()
	for i, r := range m.rows {
		if r.ID == id {
			m.moveCursor(i - m.cursor)
			return
		}
	}
	t.Fatalf("%s not in the tree: %v", id, rowIDs(m))
}

func inTree(m *model, id string) bool { return m.visRow[id] }

func TestArchiveOne(t *testing.T) {
	m, _ := archiveModel(t)
	goTo(t, m, "aaaa0001")
	at := m.cursor
	m.Update(tkey("e"))
	if inTree(m, "aaaa0001") {
		t.Fatal("archived agent still in the tree")
	}
	if m.cursor != at || m.cursorID != "aaaa0002" {
		t.Fatalf("selection should stay in place (on w2), got %s", m.cursorID)
	}
	if !strings.Contains(ansi.Strip(m.header()), "1 archived") || strings.Contains(ansi.Strip(m.header()), "4 live") {
		t.Fatalf("header: %q", ansi.Strip(m.header()))
	}
	if l := layout.Load(); !l.IsArchived("aaaa0001") {
		t.Fatal("archive not saved in layout.json")
	}
	m.Update(tkey("tab")) // inbox: archived waiting agents aren't there
	for _, r := range m.rows {
		if r.ID == "aaaa0001" {
			t.Fatal("archived agent in the inbox")
		}
	}
	m.Update(tkey("tab"))
	m.Update(tkey("A"))
	if !inTree(m, "aaaa0001") {
		t.Fatal("A should show archived agents")
	}
	goTo(t, m, "aaaa0001")
	m.Update(tkey("e")) // on an archived agent: bring it back
	m.Update(tkey("A"))
	if !inTree(m, "aaaa0001") || m.layout.IsArchived("aaaa0001") {
		t.Fatal("e on an archived agent should unarchive it")
	}
}

func TestArchiveGroupSkipsBusy(t *testing.T) {
	m, _ := archiveModel(t)
	goTo(t, m, m.layout.Parent["aaaa0001"]) // the project group
	m.Update(tkey("e"))
	if m.mode != modeConfirm {
		t.Fatal("archiving a group should ask first")
	}
	m.Update(tkey("y"))
	for id, want := range map[string]bool{"aaaa0001": true, "aaaa0002": true, "aaaa0003": false, "bbbb0001": false} {
		if m.layout.IsArchived(id) != want {
			t.Errorf("%s archived=%v, want %v", id, !want, want)
		}
	}
	if !inTree(m, "aaaa0003") {
		t.Fatal("the busy agent should stay")
	}
}

func TestArchivedComesBackWhenBusy(t *testing.T) {
	m, set := archiveModel(t)
	goTo(t, m, "aaaa0001")
	m.Update(tkey("e"))
	set(map[string]string{"aaaa0001": "blocked"}) // still waiting: stays archived
	if inTree(m, "aaaa0001") {
		t.Fatal("came back without activity")
	}
	set(map[string]string{"aaaa0001": "working"}) // resumed
	if !inTree(m, "aaaa0001") || m.layout.IsArchived("aaaa0001") {
		t.Fatal("a resumed agent should leave the archive")
	}
}

func TestSelectModeArchive(t *testing.T) {
	m, _ := archiveModel(t)
	m.Update(tkey("v"))
	if !m.selecting || !strings.Contains(ansi.Strip(m.View()), "0 marked") {
		t.Fatal("v should enter select mode")
	}
	goTo(t, m, "aaaa0001")
	m.Update(tkey(" ")) // marks w1, moves to w2
	if !m.marked["aaaa0001"] || m.cursorID != "aaaa0002" {
		t.Fatalf("space: marked %v, cursor %s", m.marked, m.cursorID)
	}
	goTo(t, m, m.layout.Parent["bbbb0001"])
	m.Update(tkey(" ")) // a group: all of it
	if !m.marked["bbbb0001"] {
		t.Fatal("space on a group should mark its agents")
	}
	if s := ansi.Strip(m.View()); !strings.Contains(s, "2 marked") || !strings.Contains(s, "◉") {
		t.Fatalf("marks not shown:\n%s", s)
	}
	m.Update(tkey("e"))
	if m.selecting || !m.layout.IsArchived("aaaa0001") || !m.layout.IsArchived("bbbb0001") || m.layout.IsArchived("aaaa0002") {
		t.Fatalf("e should archive exactly the marked: %v", m.layout.Archived)
	}
}

func TestSelectModeRemove(t *testing.T) {
	m, _ := archiveModel(t)
	var ran []string
	orig := removeAgent
	t.Cleanup(func() { removeAgent = orig })
	removeAgent = func(id string) (string, error) {
		ran = append(ran, id)
		if id == "aaaa0002" {
			return "has unpushed commits", errors.New("exit status 1")
		}
		return "", nil
	}
	m.Update(tkey("v"))
	m.Update(tkey("*")) // everything shown
	if len(m.markedIDs()) != 4 {
		t.Fatalf("* marked %v", m.markedIDs())
	}
	goTo(t, m, "aaaa0003")
	m.Update(tkey(" ")) // unmark the busy one
	_, cmd := m.Update(tkey("x"))
	if m.mode != modeConfirm || len(ran) > 0 {
		t.Fatal("x should ask before removing anything")
	}
	_, cmd = m.Update(tkey("y"))
	msg := cmd()
	if len(ran) != 3 {
		t.Fatalf("claude rm ran for %v", ran)
	}
	m.Update(msg)
	if !strings.Contains(m.status, "removed 2 sessions") || !strings.Contains(m.status, "1 refused") || !strings.Contains(m.status, "unpushed") {
		t.Fatalf("status %q", m.status)
	}
	_ = tea.Quit
}

func TestSelectModeEscAndCancel(t *testing.T) {
	m, _ := archiveModel(t)
	m.Update(tkey("v"))
	m.Update(tkey(" "))
	m.Update(tkey("x"))
	m.Update(tkey("n")) // cancelled: nothing removed, still selecting
	if !m.selecting {
		t.Fatal("cancelling should keep select mode")
	}
	if _, cmd := m.Update(tkey("esc")); quits(cmd) || m.selecting || len(m.marked) != 0 {
		t.Fatal("esc should leave select mode, not quit")
	}
}
