package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// filterModel: three sessions, two of them about payments.
func filterModel(t *testing.T) *model {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	m := newModel()
	m.w, m.h = 140, 30
	m.setAgents([]byte(`[
	 {"id":"aaaa1111","cwd":"` + dir + `","kind":"background","name":"pay-webhook","state":"done","startedAt":1794645285576},
	 {"id":"bbbb2222","cwd":"` + dir + `","kind":"background","name":"docs","state":"done","startedAt":1794645285577},
	 {"id":"cccc3333","cwd":"` + dir + `","kind":"background","name":"pay-refunds","state":"done","startedAt":1794645285578}]`))
	m.opts.ShowDone = true
	m.rebuild()
	return m
}

// matches lists the ids the filter matches, in list order.
func matches(m *model) []string {
	var ids []string
	for i := range m.rows {
		if m.isMatch(i) {
			ids = append(ids, m.rows[i].ID)
		}
	}
	return ids
}

func TestFilterMoveAndOpen(t *testing.T) {
	m := filterModel(t)
	m.Update(tkey("/"))
	typeText(m, "pay")
	got := matches(m)
	if len(got) != 2 {
		t.Fatalf("matches %v", got)
	}
	if m.cursorID != got[0] {
		t.Fatalf("typing should select the first match %s, not %s", got[0], m.cursorID)
	}
	m.Update(tkey("down"))
	if m.cursorID != got[1] || m.mode != modeInput {
		t.Fatalf("down: cursor %s (want %s), mode %v", m.cursorID, got[1], m.mode)
	}
	m.Update(tkey("down")) // no further match: stays
	if m.cursorID != got[1] {
		t.Fatalf("down past the last match moved to %s", m.cursorID)
	}
	_, cmd := m.Update(tkey("enter"))
	if cmd == nil || m.mode != modeNormal || m.opts.Filter != "pay" || m.cursorID != got[1] {
		t.Fatalf("enter: cmd=%v mode=%v filter=%q cursor=%s", cmd != nil, m.mode, m.opts.Filter, m.cursorID)
	}
}

func TestFilterTypingAfterMoving(t *testing.T) {
	m := filterModel(t)
	m.Update(tkey("/"))
	typeText(m, "pay")
	m.Update(tkey("down"))
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP}) // ctrl+p: back up
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	typeText(m, "-ref")
	if got := matches(m); len(got) != 1 || got[0] != "cccc3333" || m.cursorID != "cccc3333" {
		t.Fatalf("after refining: matches %v cursor %s", got, m.cursorID)
	}
	m.Update(tkey("ctrl+w")) // drops the whole word: back to every session
	if m.input != "" || len(matches(m)) != 3 {
		t.Fatalf("ctrl+w: input %q matches %v", m.input, matches(m))
	}
	typeText(m, "docs")
	m.Update(tkey("esc")) // esc gives the old filter back
	if m.mode != modeNormal || m.opts.Filter != "" {
		t.Fatalf("esc: mode %v filter %q", m.mode, m.opts.Filter)
	}
}

func TestFilterFromInput(t *testing.T) {
	m := filterModel(t)
	m.Update(tkey("i"))
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	typeText(m, "pay")
	m.Update(tkey("down"))
	if got := matches(m); len(got) != 2 || m.cursorID != got[1] {
		t.Fatalf("ctrl+f then down: cursor %s, matches %v", m.cursorID, got)
	}
}
