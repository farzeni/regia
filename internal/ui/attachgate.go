package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/plugins"
)

// enter on an agent asks your attach gates first, when you have any for its
// directory (plugins with "gates": ["attach"]): one may keep you out, or ask
// regia to run one of its actions first, e.g. one that starts a fresh session
// from a handoff, which you then land in instead of a large cold one. A gate
// that fails or takes more than 2 s lets you through, with a warning:
// attaching destroys nothing.

type attachGateMsg struct {
	id  string
	ans plugins.AttachAnswer
}

// attachGate is plugins.Attach; a var so tests can answer for the gates.
var attachGate = plugins.Attach

// attachGated is enter on agent id: through the attach gates, then attach.
func (m *model) attachGated(id string) tea.Cmd {
	a := m.agents[id]
	if a == nil || !plugins.AttachGating(a.Cwd) {
		return m.attachNow(id)
	}
	in, dir := m.pluginInput(a, nil)(nil)
	m.flash("checking %s before attaching…", m.titleOf(id))
	return func() tea.Msg { return attachGateMsg{id, attachGate(in, dir)} }
}

// attachNow attaches to agent id, no questions asked.
func (m *model) attachNow(id string) tea.Cmd {
	m.typing = false
	return attachSession(id)
}

// attachGateDone acts on what the gates said.
func (m *model) attachGateDone(msg attachGateMsg) tea.Cmd {
	ans := msg.ans
	switch {
	case ans.Refuse != "":
		m.flash("not attaching: %s", ans.Refuse)
		return nil
	case ans.Run != "" && ans.Plugin != nil:
		label := ans.Run
		for _, r := range plugins.Results(msg.id) {
			if r.Plugin.Name != ans.Plugin.Name {
				continue
			}
			for _, act := range r.Out.Actions {
				if act.ID == ans.Run {
					label = act.Label
				}
			}
		}
		return m.pluginActionCmd(msg.id, ans.Plugin, ans.Run, label, true)
	}
	if ans.Warn != "" {
		m.flash("attaching anyway: %s", ans.Warn)
	}
	return m.attachNow(msg.id)
}
