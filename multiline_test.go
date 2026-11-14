package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func stubSpawn(t *testing.T) *string {
	t.Helper()
	var got string
	orig := spawnAgent
	t.Cleanup(func() { spawnAgent = orig })
	spawnAgent = func(dir, task string) (string, error) {
		got = task
		return "started dddd4444\n", nil
	}
	return &got
}

// send delivers a key and then renders, as bubbletea does between keys (the
// text area only scrolls once a render has given its viewport content).
func send(m *model, msg tea.KeyMsg) {
	if _, cmd := m.Update(msg); cmd != nil {
		switch r := runCmd(cmd).(type) {
		case spawnedMsg, removedMsg: // deliver the result; its follow-up refresh isn't run
			m.Update(r)
		}
	}
	m.View()
}

func typeAndRender(m *model, s string) {
	typeText(m, s)
	m.View()
}

func TestMultilineDraft(t *testing.T) {
	m := testModel(t)
	got := stubSpawn(t)

	m.w, m.h = 100, 28
	typeAndRender(m, "first line")
	send(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true}) // alt+enter
	typeAndRender(m, "second")
	// the first line must stay in view as the box grows (it used to scroll off)
	if ls := strings.Split(ansi.Strip(m.View()), "\n"); !strings.Contains(ls[28-3], "❯ first line") || !strings.Contains(ls[28-2], "second") {
		t.Fatalf("box after two lines:\n%s", strings.Join(ls[22:], "\n"))
	}
	send(m, tea.KeyMsg{Type: tea.KeyCtrlJ}) // ctrl+j
	typeText(m, `third \`)
	send(m, tkey("enter")) // "\" + enter: newline, not send
	typeText(m, "fourth")
	if want := "first line\nsecond\nthird \nfourth"; m.draft() != want {
		t.Fatalf("draft %q, want %q", m.draft(), want)
	}

	sel := m.cursorID
	send(m, tkey("up")) // several lines: ↑ moves in the text
	if m.cursorID != sel || m.ta.Line() != 2 {
		t.Fatalf("up: list moved=%v, line=%d", m.cursorID != sel, m.ta.Line())
	}
	send(m, tea.KeyMsg{Type: tea.KeyCtrlE})
	typeText(m, "!")
	if !strings.Contains(m.draft(), "third !\n") {
		t.Fatalf("editing line 3: %q", m.draft())
	}

	m.w, m.h = 100, 28
	ls := strings.Split(ansi.Strip(m.View()), "\n")
	if len(ls) != 28 {
		t.Fatalf("frame has %d lines", len(ls))
	}
	if !strings.HasPrefix(ls[28-6], "╭") || !strings.Contains(ls[28-5], "❯ first line") || !strings.Contains(ls[28-2], "fourth") {
		t.Fatalf("box should grow to 4 rows:\n%s", strings.Join(ls[20:], "\n"))
	}

	send(m, tkey("enter"))
	if *got != "first line\nsecond\nthird !\nfourth" || m.draft() != "" {
		t.Fatalf("sent %q, draft left %q", *got, m.draft())
	}
}

func TestPasteFolding(t *testing.T) {
	m := testModel(t)
	got := stubSpawn(t)

	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("two\r\nlines"), Paste: true})
	if m.draft() != "two\nlines" {
		t.Fatalf("short paste should go in as is: %q", m.draft())
	}
	m.clearDraft()

	var b strings.Builder
	for i := 1; i <= 120; i++ {
		fmt.Fprintf(&b, "log line %d: something happened\n", i)
	}
	big := b.String()
	typeText(m, "look at this: ")
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(big), Paste: true})
	typeText(m, " and fix it")
	if m.draft() != "look at this: [Pasted text #1 +121 lines] and fix it" {
		t.Fatalf("long paste should fold: %q", m.draft())
	}
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("x", 2000)), Paste: true})
	if !strings.HasSuffix(m.draft(), "[Pasted text #2, 2000 chars]") {
		t.Fatalf("long single line: %q", m.draft())
	}

	m.w, m.h = 100, 28
	if ls := strings.Split(ansi.Strip(m.View()), "\n"); len(ls) != 28 {
		t.Fatalf("frame has %d lines", len(ls))
	}

	send(m, tkey("enter"))
	if want := "look at this: " + big + " and fix it" + strings.Repeat("x", 2000); *got != want {
		t.Fatalf("expanded task differs (len %d vs %d)", len(*got), len(want))
	}

	// too long for one argument: refused, draft kept
	*got = ""
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("y\n", 70000)), Paste: true})
	send(m, tkey("enter"))
	if *got != "" || m.draft() == "" || !strings.Contains(m.status, "too long") {
		t.Fatalf("oversized task: sent=%d draft=%q status=%q", len(*got), m.draft(), m.status)
	}
}

func TestInputGrowsToMax(t *testing.T) {
	m := testModel(t)
	m.w, m.h = 100, 28
	for i := 0; i < 20; i++ {
		typeText(m, fmt.Sprintf("line%d", i))
		send(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	}
	ls := strings.Split(ansi.Strip(m.View()), "\n")
	if len(ls) != 28 {
		t.Fatalf("frame has %d lines", len(ls))
	}
	if m.inputRows() != inputMaxRows || !strings.HasPrefix(ls[28-inputMaxRows-2], "╭") {
		t.Fatalf("box rows %d:\n%s", m.inputRows(), strings.Join(ls[14:], "\n"))
	}
}
