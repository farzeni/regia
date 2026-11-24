package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/plugins"
)

// Plugins with "group" in their "targets" run for groups' rows too: their
// input has "group" (its path, directory, coordinator and agents) in place of
// "agent". What they print shows the same way: the badge on the group's row,
// fields as tabs in the group's panel, actions in the ! menu.

// pluginAgent is what a plugin reads about agent a: regia's name for it, where
// it sits, and the sessions it replaced (regia swap).
func (m *model) pluginAgent(a *claude.Agent) plugins.AgentInfo {
	ai := agentInfo(a)
	ai.Title = m.titleOf(a.ID)
	if m.agents[a.ID] == nil { // a removed session, from the whole history
		ai.Title = a.Name
	}
	ai.Group = m.layout.Trail(a.ID, m.agents)
	for _, s := range m.layout.Swaps[a.ID] {
		ai.SwappedFrom = append(ai.SwappedFrom, plugins.SwapInfo{ID: s.From, SessionID: s.Session, Title: s.Title,
			At: s.At, Reason: s.Reason, Note: s.Note})
	}
	return ai
}

// groupDir is where new agents in group id start (w, or guessed).
func (m *model) groupDir(id string) string {
	d, _ := m.layout.SpawnDir(id, m.agents)
	return d
}

// groupInfo is what a plugin reads about group id.
func (m *model) groupInfo(id string) *plugins.GroupInfo {
	g := &plugins.GroupInfo{ID: id, Name: m.layout.Groups[id], Dir: m.groupDir(id), Agents: []plugins.AgentInfo{}}
	g.Path = append(m.layout.Trail(id, m.agents), g.Name)
	var walk func(p string, top bool)
	walk = func(p string, top bool) {
		for _, k := range m.layout.Children(p, m.agents) {
			if a := m.agents[k]; a != nil {
				ai := m.pluginAgent(a)
				g.Agents = append(g.Agents, ai)
				if top && g.Coordinator == nil && len(m.layout.Children(k, m.agents)) > 0 {
					c := ai
					g.Coordinator = &c
				}
			}
			walk(k, false)
		}
	}
	walk(id, true)
	return g
}

// groupInput builds what plugin p reads about group id, and where it runs.
func (m *model) groupInput(id string, p *plugins.Manifest) func(last *plugins.Output) (plugins.Input, string) {
	return func(last *plugins.Output) (plugins.Input, string) {
		g := m.groupInfo(id)
		return plugins.Input{
			Version: plugins.ProtocolVersion,
			Plugin:  plugins.PluginInfo{Name: p.Name, Dir: p.Dir},
			Group:   g,
			Last:    last,
		}, g.Dir
	}
}

// groupRefreshers: the runs due for group id, one per plugin for groups.
func (m *model) groupRefreshers(id string) []func() {
	if _, ok := m.layout.Groups[id]; !ok {
		return nil
	}
	dir := ""
	var fs []func()
	for _, p := range plugins.Plugins() {
		if len(p.Targets) == 0 {
			continue // agents only: skip the directory guess
		}
		if dir == "" {
			dir = m.groupDir(id)
		}
		if !p.AppliesGroup(dir) {
			continue
		}
		if f := plugins.Refresher(p, id, m.groupInput(id, p)); f != nil {
			fs = append(fs, f)
		}
	}
	return fs
}

// rowPluginCmd refreshes the selected row's plugins that are due.
func (m *model) rowPluginCmd(id string) tea.Cmd {
	if a := m.agents[id]; a != nil {
		return m.pluginCmd(a)
	}
	return tea.Batch(asCmds(m.groupRefreshers(id))...)
}

// inputFor builds the input of plugin p for row id: an agent (listed, or a
// removed one search found) or a group.
func (m *model) inputFor(id string, p *plugins.Manifest) func(last *plugins.Output) (plugins.Input, string) {
	if _, ok := m.layout.Groups[id]; ok && m.agents[id] == nil {
		return m.groupInput(id, p)
	}
	a := m.agents[id]
	if a == nil {
		a = m.searchAgent(id)
	}
	if a == nil {
		a = &claude.Agent{ID: id}
	}
	return m.pluginInput(a, p)
}
