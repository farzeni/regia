package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

const inA, inB, inC = "c0000002", "c0000001", "c0000000"

// inboxABC: three waiting agents in the inbox, A on top (the most recent),
// then B and C. list(states) is the next fetch: those agents changed, the
// others still waiting.
func inboxABC(t *testing.T) (*model, func(states map[string]string)) {
	m := manyAgents(t, 3)
	list := func(states map[string]string) {
		var js []string
		for _, id := range []string{inC, inB, inA} {
			a := m.agents[id]
			st := "blocked"
			if s, ok := states[id]; ok {
				st = s
			}
			js = append(js, fmt.Sprintf(`{"id":%q,"cwd":%q,"kind":"background","name":%q,"state":%q,"startedAt":%d}`,
				id, a.Cwd, a.Name, st, a.StartedAt.UnixMilli()))
		}
		m.setAgents([]byte("[" + strings.Join(js, ",") + "]"))
	}
	m.Update(tkey("tab"))
	if got := rowIDs(m); !slices.Equal(got, []string{inA, inB, inC}) || m.cursorID != inA {
		t.Fatalf("inbox %v cursor %s, want A B C with A selected", got, m.cursorID)
	}
	return m, list
}

func wantCursor(t *testing.T, m *model, id string, at int) {
	t.Helper()
	if m.cursorID != id || m.cursor != at {
		t.Fatalf("rows %v cursor %s@%d, want %s@%d", rowIDs(m), m.cursorID, m.cursor, id, at)
	}
}

// Reply to the top item and come back: the next one is selected, on top.
func TestInboxReplyMovesOn(t *testing.T) {
	m, list := inboxABC(t)
	m.Update(attachDoneMsg{id: inA})
	list(map[string]string{inA: "working"}) // the fetch after you come back
	if got := rowIDs(m); !slices.Equal(got, []string{inB, inC}) {
		t.Fatalf("rows %v, want B C", got)
	}
	wantCursor(t, m, inB, 0)
	// and again: one by one down the inbox
	m.Update(attachDoneMsg{id: inB})
	list(map[string]string{inA: "working", inB: "working"})
	wantCursor(t, m, inC, 0)
}

// Leave without replying: it still waits on you, the cursor stays on it.
func TestInboxLeaveWithoutReplyStays(t *testing.T) {
	m, list := inboxABC(t)
	m.Update(attachDoneMsg{id: inA})
	list(nil)
	list(nil)
	if len(m.rows) != 3 {
		t.Fatalf("rows %v, want A B C", rowIDs(m))
	}
	wantCursor(t, m, inA, 0)
}

// The reply flips the state a moment later: the first fetch still says
// blocked, a later one working; the cursor moves on then.
func TestInboxReplyLateStateFlip(t *testing.T) {
	m, list := inboxABC(t)
	m.Update(attachDoneMsg{id: inA})
	list(nil)
	wantCursor(t, m, inA, 0)
	list(map[string]string{inA: "working"})
	wantCursor(t, m, inB, 0)

	// you moved away and back before it flipped: now it's the one you're
	// reading, kept until you move away, as before
	m, list = inboxABC(t)
	m.Update(attachDoneMsg{id: inA})
	list(nil)
	m.moveCursor(1)
	m.moveCursor(-1)
	list(map[string]string{inA: "working"})
	if m.cursorID != inA || !slices.Contains(rowIDs(m), inA) {
		t.Fatalf("rows %v cursor %s, want A kept", rowIDs(m), m.cursorID)
	}
}

// The tree is unchanged: the agent you came back from stays selected and
// listed, even when it's done and done ones are hidden.
func TestTreeReplyKeepsSelection(t *testing.T) {
	m, list := inboxABC(t)
	m.setView(viewTree)
	m.opts.ShowDone = false
	goTo(t, m, inA)
	m.Update(attachDoneMsg{id: inA})
	list(map[string]string{inA: "done"})
	if m.cursorID != inA || !slices.Contains(rowIDs(m), inA) {
		t.Fatalf("tree: rows %v cursor %s, want A kept", rowIDs(m), m.cursorID)
	}
}
