package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/plugins"
)

type removedMsg struct {
	id, out string
	err     error
}

// removeAgent removes the session in.Agent the way every removal does
// (group.RemoveSession): your plugins' remove gates may refuse, then
// `claude rm` takes it off the list and deletes its worktree when that's
// safe; the conversation stays resumable. claude rm refuses too (non-zero
// exit, with a reason), e.g. over unpushed commits. A variable for tests.
var removeAgent = func(in plugins.Input) (string, error) {
	return group.RemoveSession(in, "you", "")
}

// removeInput is what the remove gates read about id: built while it is
// still listed, before it is hidden.
func (m *model) removeInput(id string) plugins.Input {
	in, _ := m.pluginInput(m.agents[id], &plugins.Manifest{})(nil)
	return in
}

// removeNow hides id and removes it in the background.
func (m *model) removeNow(id string) tea.Cmd {
	in, name := m.removeInput(id), m.titleOf(id)
	m.hideRemoved(id)
	m.flash("removing %s…", name)
	return func() tea.Msg {
		out, err := removeAgent(in)
		return removedMsg{id, out, err}
	}
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
