package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// quits says whether cmd is tea.Quit.
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestEscQuits(t *testing.T) {
	m := testModel(t)
	typeText(m, "draft")
	if _, cmd := m.Update(tkey("esc")); quits(cmd) || m.draft() != "" || !m.typing {
		t.Fatal("esc on a draft should only clear it")
	}
	if _, cmd := m.Update(tkey("esc")); quits(cmd) || m.typing {
		t.Fatal("esc on an empty input should switch to command keys")
	}
	if _, cmd := m.Update(tkey("q")); quits(cmd) {
		t.Fatal("q must not quit any more")
	}
	m.opts.Filter = "alp"
	m.rebuild()
	if _, cmd := m.Update(tkey("esc")); quits(cmd) || m.opts.Filter != "" {
		t.Fatal("esc should clear the filter first, not quit")
	}
	if _, cmd := m.Update(tkey("esc")); !quits(cmd) {
		t.Fatal("esc with nothing to clear should quit")
	}
}

func agentsJSON(dir string, extra string) []byte {
	return []byte(`[
	 {"id":"aaaa1111","cwd":"` + dir + `","kind":"background","name":"alpha","status":"busy","state":"working","startedAt":1794645285576},
	 {"id":"bbbb2222","cwd":"` + dir + `","kind":"background","name":"beta","status":"idle","state":"done","startedAt":1794645285577}` + extra + `]`)
}

func TestSpawnedAgentGetsSelected(t *testing.T) {
	m := testModel(t)
	stubSpawn(t) // returns dddd4444
	dir := m.agents["aaaa1111"].Cwd
	m.cursorID = "aaaa1111"
	m.layout.Collapsed[m.layout.Parent["aaaa1111"]] = true // its group folded
	m.rebuild()

	typeText(m, "new task")
	send(m, tkey("enter"))
	if m.cursorID == "dddd4444" {
		t.Fatal("selected before claude agents lists it")
	}
	if m.selectID != "dddd4444" {
		t.Fatalf("pending select %q", m.selectID)
	}
	if m.layout.Collapsed[m.layout.Parent["dddd4444"]] {
		t.Fatal("the new agent's group should be unfolded")
	}
	m.Update(agentsMsg{b: agentsJSON(dir, `,
	 {"id":"dddd4444","cwd":"`+dir+`","kind":"background","name":"new task","status":"busy","state":"working","startedAt":1794645285578}`)})
	if m.cursorID != "dddd4444" || m.selectID != "" {
		t.Fatalf("cursor %q, pending %q: new agent not selected", m.cursorID, m.selectID)
	}
}

func TestMovingCancelsPendingSelect(t *testing.T) {
	m := testModel(t)
	stubSpawn(t)
	dir := m.agents["aaaa1111"].Cwd
	typeText(m, "new task")
	send(m, tkey("enter"))
	send(m, tkey("down")) // the user moved on
	moved := m.cursorID
	m.Update(agentsMsg{b: agentsJSON(dir, `,
	 {"id":"dddd4444","cwd":"`+dir+`","kind":"background","name":"new task","status":"busy","state":"working","startedAt":1794645285578}`)})
	if m.cursorID != moved {
		t.Fatalf("cursor jumped to %q after the user moved", m.cursorID)
	}
}
