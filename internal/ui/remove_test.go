package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/plugins"
)

// a listing fetched while claude rm runs (or just after) still has the agent:
// it stays hidden, and is forgotten once a listing drops it
func TestRemovingHiddenFromStaleListings(t *testing.T) {
	m := testModel(t)
	a := m.agents["aaaa1111"]
	m.hideRemoved("aaaa1111")
	if m.agents["aaaa1111"] != nil || m.visRow["aaaa1111"] {
		t.Fatal("not hidden at once")
	}
	stale := map[string]*claude.Agent{"aaaa1111": a}
	m.dropRemoving(stale)
	if stale["aaaa1111"] != nil || m.removing["aaaa1111"] == nil {
		t.Fatal("stale listing brought it back")
	}
	m.removalDone("aaaa1111")
	stale = map[string]*claude.Agent{"aaaa1111": a}
	m.dropRemoving(stale)
	if stale["aaaa1111"] != nil {
		t.Fatal("listing from before claude rm finished brought it back")
	}
	m.dropRemoving(map[string]*claude.Agent{})
	if len(m.removing) != 0 {
		t.Fatal("not forgotten once the listing dropped it")
	}

	// refused: back in the list
	m.hideRemoved("bbbb2222")
	m.showRemoved("bbbb2222")
	m.refresh()
	if m.agents["bbbb2222"] == nil || len(m.removing) != 0 {
		t.Fatal("refused removal not restored")
	}
}

func TestRemoveAgent(t *testing.T) {
	m := testModel(t)
	var calls []string
	orig := removeAgent
	t.Cleanup(func() { removeAgent = orig })
	removeAgent = func(in plugins.Input) (string, error) {
		id := in.Agent.ID
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
	send(m, tkey("y")) // y no longer confirms: it cancels
	if len(calls) != 0 || m.mode == modeConfirm {
		t.Fatalf("y should cancel: calls=%v mode=%v", calls, m.mode)
	}
	send(m, tkey("x"))
	send(m, tkey("x"))
	if len(calls) != 1 || calls[0] != "aaaa1111" || !strings.Contains(m.status, "removed aaaa1111") {
		t.Fatalf("calls %v status %q", calls, m.status)
	}

	// a refusal from claude rm is shown, nothing forced
	m.cursorID = "bbbb2222"
	m.rebuild()
	send(m, tkey("x"))
	send(m, tkey("x"))
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
