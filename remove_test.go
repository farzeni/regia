package main

import (
	"errors"
	"strings"
	"testing"
)

func TestRemoveAgent(t *testing.T) {
	m := testModel(t)
	var calls []string
	orig := removeAgent
	t.Cleanup(func() { removeAgent = orig })
	removeAgent = func(id string) (string, error) {
		calls = append(calls, id)
		if id == "bbbb2222" {
			return "Not removed: worktree has 2 unpushed commits.\nRun: claude rm bbbb2222 --discard-unpushed abc@wt1\n", errors.New("exit status 1")
		}
		return "removed\n", nil
	}

	send(m, tkey("esc")) // command keys
	m.cursorID = "aaaa1111"
	m.rebuild()
	send(m, tkey("x"))
	if m.mode != modeConfirm || !strings.Contains(m.confirmMsg, "alpha") {
		t.Fatalf("x should ask first: mode %v %q", m.mode, m.confirmMsg)
	}
	send(m, tkey("n"))
	if len(calls) != 0 {
		t.Fatal("removed without confirmation")
	}
	send(m, tkey("x"))
	send(m, tkey("y"))
	if len(calls) != 1 || calls[0] != "aaaa1111" || !strings.Contains(m.status, "removed aaaa1111") {
		t.Fatalf("calls %v status %q", calls, m.status)
	}

	// a refusal from claude rm is shown, nothing forced
	m.cursorID = "bbbb2222"
	m.rebuild()
	send(m, tkey("x"))
	send(m, tkey("y"))
	if !strings.Contains(m.status, "not removed: Not removed: worktree has 2 unpushed commits.") {
		t.Fatalf("refusal not shown: %q", m.status)
	}

	// in the input, x is just a letter
	send(m, tkey("i"))
	send(m, tkey("x"))
	if m.mode != modeNormal || m.draft() != "x" || len(calls) != 2 {
		t.Fatalf("x while typing: mode %v draft %q calls %v", m.mode, m.draft(), calls)
	}
}
