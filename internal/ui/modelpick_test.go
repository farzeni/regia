package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// modelSpawns stubs both spawn seams and records which one ran and how.
func modelSpawns(t *testing.T) (plain *int, gotModel *string) {
	var n int
	var mod string
	o1, o2 := spawnAgent, spawnAgentModel
	t.Cleanup(func() { spawnAgent, spawnAgentModel = o1, o2 })
	spawnAgent = func(dir, task string) (string, error) {
		n++
		return "started dddd4444\n", nil
	}
	spawnAgentModel = func(dir, model, task string) (string, error) {
		mod = model
		return "started dddd4444\n", nil
	}
	return &n, &mod
}

func TestPickModelForOneTask(t *testing.T) {
	m, _, _ := dirModel(t)
	plain, got := modelSpawns(t)
	goTo(t, m, "aaaa1111")
	if !strings.Contains(screen(m), "default · ctrl+g ── ") {
		t.Fatalf("the border should show the model left of the dir:\n%s", screen(m))
	}

	m.Update(ctrl(tea.KeyCtrlG))
	if m.mode != modeModel || len(m.modelPick.items) != len(modelAliases) {
		t.Fatalf("picker should open with the aliases: mode %v %+v", m.mode, m.modelPick)
	}
	typeText(m, "son")
	if it := m.modelPick.items; len(it) != 2 || it[0].name != "sonnet" || it[1].name != "son" {
		t.Fatalf("filter son: %+v", it)
	}
	m.Update(tkey("enter"))
	if m.mode != modeNormal || m.draftModel != "sonnet" || !m.typing {
		t.Fatalf("pick: mode %v model %q typing %v", m.mode, m.draftModel, m.typing)
	}
	if s := screen(m); !strings.Contains(s, "sonnet · ctrl+g ── ") || !strings.Contains(s, " · ctrl+o ─╯") {
		t.Fatalf("border should show the picked model and the dir:\n%s", s)
	}

	typeText(m, "do it")
	send(m, tkey("enter"))
	if *got != "sonnet" || *plain != 0 {
		t.Fatalf("spawn should pass --model sonnet: model %q plain %d", *got, *plain)
	}
	if m.draftModel != "" {
		t.Fatalf("the pick should last one task, still %q", m.draftModel)
	}

	// without a pick, claude picks: no --model
	typeText(m, "again")
	send(m, tkey("enter"))
	if *plain != 1 {
		t.Fatalf("a task without a pick should spawn without --model")
	}
}

func TestPickModelTypedID(t *testing.T) {
	m, _, _ := dirModel(t)
	_, got := modelSpawns(t)
	m.Update(ctrl(tea.KeyCtrlG))
	typeText(m, "claude-opus-5-5")
	if it := m.modelPick.items; len(it) != 1 || it[0].name != "claude-opus-5-5" {
		t.Fatalf("a typed id should be offered: %+v", it)
	}
	m.Update(tkey("enter"))
	typeText(m, "task")
	send(m, tkey("enter"))
	if *got != "claude-opus-5-5" {
		t.Fatalf("spawned with %q", *got)
	}
}

func TestPickModelEscKeeps(t *testing.T) {
	m, _, _ := dirModel(t)
	m.draftModel = "haiku"
	m.Update(ctrl(tea.KeyCtrlG))
	m.Update(tkey("esc"))
	if m.mode != modeNormal || m.draftModel != "haiku" || !m.typing {
		t.Fatalf("esc: mode %v model %q typing %v", m.mode, m.draftModel, m.typing)
	}
	// "default" goes back to claude's choice
	m.Update(ctrl(tea.KeyCtrlG))
	m.Update(tkey("enter"))
	if m.draftModel != "" {
		t.Fatalf("default should clear the pick: %q", m.draftModel)
	}
}
