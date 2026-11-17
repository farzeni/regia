package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func tkey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "alt+up":
		return tea.KeyMsg{Type: tea.KeyUp, Alt: true}
	case "alt+down":
		return tea.KeyMsg{Type: tea.KeyDown, Alt: true}
	case "ctrl+up":
		return tea.KeyMsg{Type: tea.KeyCtrlUp}
	case "ctrl+down":
		return tea.KeyMsg{Type: tea.KeyCtrlDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+w":
		return tea.KeyMsg{Type: tea.KeyCtrlW}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeText(m *model, s string) {
	for _, w := range strings.SplitAfter(s, " ") {
		if t := strings.TrimSuffix(w, " "); t != "" {
			m.Update(tkey(t)) // a word arrives as one coalesced message
		}
		if strings.HasSuffix(w, " ") {
			m.Update(tkey(" "))
		}
	}
}

func testModel(t *testing.T) *model {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	m := newModel()
	m.w, m.h = 140, 30
	m.setAgents([]byte(`[
	 {"id":"aaaa1111","cwd":"` + dir + `","kind":"background","name":"alpha","status":"busy","state":"working","startedAt":1794645285576},
	 {"id":"bbbb2222","cwd":"` + dir + `","kind":"background","name":"beta","status":"idle","state":"done","startedAt":1794645285577}]`))
	m.opts.ShowDone = true
	m.rebuild()
	if m.typing {
		t.Fatal("regia should open on the list, not the input")
	}
	m.Update(tkey("i")) // most tests start from the input
	return m
}

func TestInputTypingAndSpawn(t *testing.T) {
	m := testModel(t)
	var gotDir, gotTask string
	orig := spawnAgent
	defer func() { spawnAgent = orig }()
	spawnAgent = func(dir, task string) (string, error) {
		gotDir, gotTask = dir, task
		return "started cccc3333\n", nil
	}

	if !m.typing {
		t.Fatal("i should focus the input")
	}
	sel := m.cursorID
	typeText(m, "jj fix the kq bug")
	if m.draft() != "jj fix the kq bug" {
		t.Fatalf("draft %q", m.draft())
	}
	if m.cursorID != sel || m.mode != modeNormal {
		t.Fatalf("letters acted as commands: cursor %s→%s mode %v", sel, m.cursorID, m.mode)
	}
	m.Update(tkey("ctrl+w"))
	m.Update(tkey("backspace"))
	typeText(m, "rs bug")
	// ctrl+w drops "bug", backspace the space: "jj fix the kq" + "rs bug"
	if m.draft() != "jj fix the kqrs bug" {
		t.Fatalf("editing: draft %q", m.draft())
	}

	m.Update(tkey("down")) // arrows still move the list while typing
	if m.cursorID == sel {
		t.Fatal("down did not move the selection")
	}
	target := m.cursorID
	if l := m.inputBox(); !strings.Contains(l, "jj fix the kqrs bug") {
		t.Fatalf("input line %q", l)
	}

	_, cmd := m.Update(tkey("enter"))
	if cmd == nil || m.draft() != "" {
		t.Fatalf("enter did not start: cmd=%v draft=%q", cmd != nil, m.draft())
	}
	msg := runCmd(cmd)
	if gotTask != "jj fix the kqrs bug" || gotDir == "" {
		t.Fatalf("spawned %q in %q", gotTask, gotDir)
	}
	m.Update(msg)
	if m.layout.Parent["cccc3333"] != m.layout.Parent[target] {
		t.Fatalf("new agent not placed next to %s: parent %q", target, m.layout.Parent["cccc3333"])
	}
	if o := m.layout.Order[m.layout.Parent[target]]; slices.Index(o, "cccc3333") != slices.Index(o, target)+1 {
		t.Fatalf("new agent not right after the selection: %v", o)
	}
}

func TestInputEscGivesCommandKeys(t *testing.T) {
	m := testModel(t)
	typeText(m, "draft")
	m.Update(tkey("esc"))
	if m.draft() != "" || !m.typing {
		t.Fatalf("first esc should clear the draft only: %q typing=%v", m.draft(), m.typing)
	}
	m.Update(tkey("esc"))
	if m.typing {
		t.Fatal("esc on an empty input should switch to command keys")
	}
	sel := m.cursorID
	m.Update(tkey("j"))
	if m.cursorID == sel {
		t.Fatal("j does not move in command mode")
	}
	m.Update(tkey("C"))
	if !m.typing || !m.draftInside {
		t.Fatal("C should focus the input aimed inside the selection")
	}
	if l := m.inputBox(); !strings.Contains(l, "inside ") {
		t.Fatalf("hint should say inside: %q", l)
	}
	m.Update(tkey("?"))
	if !m.help || m.draft() != "" {
		t.Fatal("? on an empty input toggles help")
	}
}

// runCmd runs c, finding the action's result inside a batch.
func runCmd(c tea.Cmd) tea.Msg {
	msg := c()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			if c == nil {
				continue
			}
			switch m := c().(type) {
			case spawnedMsg, removedMsg, removedBatchMsg:
				return m
			}
		}
	}
	return msg
}

func TestEnterSendsAndBlurs(t *testing.T) {
	m := testModel(t)
	orig := spawnAgent
	defer func() { spawnAgent = orig }()
	spawnAgent = func(dir, task string) (string, error) { return "started cccc3333\n", nil }

	typeText(m, "fix it")
	_, cmd := m.Update(tkey("enter"))
	if cmd == nil || m.draft() != "" {
		t.Fatalf("enter did not start: cmd=%v draft=%q", cmd != nil, m.draft())
	}
	if m.typing {
		t.Fatal("enter that sends a task should leave the input")
	}
	m.Update(runCmd(cmd))
	if m.typing {
		t.Fatal("the spawned agent showing up should not refocus the input")
	}
	m.Update(tkey("i"))
	if !m.typing {
		t.Fatal("i should focus the input again")
	}
}

func TestAttachAndReturnBlurs(t *testing.T) {
	for _, from := range []string{"typing", "list"} {
		m := testModel(t)
		if from == "list" {
			m.Update(tkey("esc"))
		}
		id := "aaaa1111"
		m.cursorID = id
		m.rebuild()
		_, cmd := m.Update(tkey("enter")) // empty input: attach
		if cmd == nil {
			t.Fatalf("%s: enter did not attach", from)
		}
		if m.typing {
			t.Fatalf("%s: attaching should leave the input", from)
		}
		m.typing = true // whatever happened meanwhile, coming back lands on the list
		m.Update(attachDoneMsg{id: id})
		if m.typing {
			t.Fatalf("%s: coming back from a session should land on the list", from)
		}
	}
}
