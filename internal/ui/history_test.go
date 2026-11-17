package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sendText(t *testing.T, m *model, s string) {
	t.Helper()
	orig := spawnAgent
	defer func() { spawnAgent = orig }()
	spawnAgent = func(dir, task string) (string, error) { return "started cccc3333\n", nil }
	m.typing = true
	m.ta.SetValue(s)
	m.Update(tkey("enter"))
	if m.draft() != "" {
		t.Fatalf("%q not sent", s)
	}
	m.typing = true
}

func TestHistoryUpDown(t *testing.T) {
	m := testModel(t)
	sendText(t, m, "first task")
	sendText(t, m, "second task")
	typeText(m, "half typed")

	m.Update(tkey("up"))
	if m.draft() != "second task" {
		t.Fatalf("up: %q", m.draft())
	}
	m.Update(tkey("up"))
	if m.draft() != "first task" {
		t.Fatalf("up up: %q", m.draft())
	}
	m.Update(tkey("up")) // nothing older: stays
	if m.draft() != "first task" {
		t.Fatalf("past the oldest: %q", m.draft())
	}
	m.Update(tkey("down"))
	if m.draft() != "second task" {
		t.Fatalf("down: %q", m.draft())
	}
	sel := m.cursorID
	m.Update(tkey("down"))
	if m.draft() != "half typed" {
		t.Fatalf("down past the newest should bring back the draft: %q", m.draft())
	}
	if m.cursorID != sel {
		t.Fatal("coming back to the draft should not move the list")
	}
	m.Update(tkey("down")) // not browsing: the list moves, as before
	if m.cursorID == sel {
		t.Fatal("down on the draft should move the list")
	}
}

func TestHistoryKeepsEditsWhileBrowsing(t *testing.T) {
	m := testModel(t)
	sendText(t, m, "one")
	sendText(t, m, "two")
	m.Update(tkey("up"))
	typeText(m, " more")
	m.Update(tkey("up"))
	if m.draft() != "one" {
		t.Fatalf("%q", m.draft())
	}
	m.Update(tkey("down"))
	if m.draft() != "two more" {
		t.Fatalf("the edit was lost: %q", m.draft())
	}
}

func TestEscAndCtrlCKeepTheDraft(t *testing.T) {
	for _, key := range []string{"esc", "ctrl+c"} {
		m := testModel(t)
		typeText(m, "precious words")
		if key == "esc" {
			m.Update(tkey("esc"))
		} else {
			m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		}
		if m.draft() != "" {
			t.Fatalf("%s should still clear the input", key)
		}
		m.Update(tkey("up"))
		if m.draft() != "precious words" {
			t.Fatalf("%s: ↑ did not bring back the cleared draft: %q", key, m.draft())
		}
	}
}

func TestHistoryMultiLineAndPastes(t *testing.T) {
	m := testModel(t)
	m.paste(strings.Repeat("line\n", 10))
	tok := m.draft()
	typeText(m, " go")
	sendText(t, m, m.draft()) // keeps m.pastes: sendText only sets the value
	m.pastes = nil
	sendText(t, m, "a\nb\nc")

	m.Update(tkey("up"))
	if m.draft() != "a\nb\nc" {
		t.Fatalf("%q", m.draft())
	}
	m.Update(tkey("up")) // unchanged multi-line entry: ↑ keeps going
	if !strings.HasPrefix(m.draft(), tok) {
		t.Fatalf("%q", m.draft())
	}
	if got := m.expandPastes(m.draft()); !strings.Contains(got, "line\nline") {
		t.Fatalf("paste token no longer expands: %q", got)
	}
	m.paste(strings.Repeat("x\n", 10))
	if strings.Count(m.draft(), "#1 ") != 1 {
		t.Fatalf("new paste reused the recalled token's number: %q", m.draft())
	}
}

func TestDraftFileRecovered(t *testing.T) {
	m := testModel(t)
	typeText(m, "typed before the crash")
	m.saveDraftFile()
	p := draftPath(os.Getpid())
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "typed before the crash") {
		t.Fatalf("draft file: %v %s", err, b)
	}
	// pretend it was a regia that died
	dead := filepath.Join(filepath.Dir(p), "999999999.json")
	if err := os.Rename(p, dead); err != nil {
		t.Fatal(err)
	}
	e := recoverDrafts()
	if e == nil || e.Text != "typed before the crash" {
		t.Fatalf("recovered %+v", e)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatal("the draft file should be gone once it is in the history")
	}
	list := loadHist()
	if len(list) != 1 || !list[0].Unsent {
		t.Fatalf("history %+v", list)
	}

	m.clearDraft()
	m.saveDraftFile()
	if _, err := os.Stat(draftPath(os.Getpid())); !os.IsNotExist(err) {
		t.Fatal("an empty draft should remove the file")
	}
}

func TestFinishDraftKeepsIt(t *testing.T) {
	m := testModel(t)
	typeText(m, "still in the box")
	m.finishDraft()
	l := loadHist()
	if len(l) != 1 || l[0].Text != "still in the box" {
		b, _ := json.Marshal(l)
		t.Fatalf("history %s", b)
	}
}
