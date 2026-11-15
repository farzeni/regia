package ui

import (
	"os/exec"
	"strings"

	"github.com/farzeni/regia/internal/claude"
)

type removedMsg struct {
	id, out string
	err     error
}

// removeAgent runs `claude rm <id>`: the session leaves the list and its
// worktree is deleted when that's safe; the conversation stays resumable.
// It refuses (non-zero exit, with a reason) e.g. over unpushed commits, and
// regia never passes the --discard/--force flags. A variable for tests.
var removeAgent = func(id string) (string, error) {
	out, err := exec.Command("claude", "rm", id).CombinedOutput()
	return string(out), err
}

// oneLine condenses command output for the status line.
func oneLine(out string, err error) string {
	s := strings.Join(strings.Fields(out), " ")
	if s == "" && err != nil {
		s = err.Error()
	}
	return s
}

// pendingRemoval is an agent taken off the list before `claude rm` finished.
type pendingRemoval struct {
	a    *claude.Agent // put back if claude rm refuses
	done bool          // claude rm succeeded: forget it once a listing drops it
}

// hideRemoved takes agents off the list at once (claude rm can take seconds):
// they stay hidden, even from listings fetched meanwhile, until claude rm
// refuses (showRemoved puts them back) or a listing no longer has them.
func (m *model) hideRemoved(ids ...string) {
	if m.removing == nil {
		m.removing = map[string]*pendingRemoval{}
	}
	for _, id := range ids {
		if a := m.agents[id]; a != nil {
			m.removing[id] = &pendingRemoval{a: a}
			delete(m.agents, id)
		}
	}
	m.refresh()
}

// showRemoved puts back an agent claude rm refused to remove.
func (m *model) showRemoved(id string) {
	if p := m.removing[id]; p != nil {
		if m.agents[id] == nil {
			m.agents[id] = p.a
		}
		delete(m.removing, id)
	}
}

// removalDone marks claude rm as succeeded for id.
func (m *model) removalDone(id string) {
	if p := m.removing[id]; p != nil {
		p.done = true
	}
}

// dropRemoving filters a fresh listing: agents being removed stay hidden.
func (m *model) dropRemoving(next map[string]*claude.Agent) {
	for id, p := range m.removing {
		if next[id] != nil {
			delete(next, id)
		} else if p.done {
			delete(m.removing, id)
		}
	}
}
