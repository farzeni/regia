package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/plugins"
)

// A plugin may ask regia to start an agent (its output's "start"), from an
// action you picked, from the action the attach gate ran, or from a refresh
// (once per key). regia starts it the way typing a task does, then, once
// `claude agents` lists it: as "coordinator" the group's agents (or the agent
// whose row it is) move under it; as "swap" it takes the agent's place
// (regia swap); and when enter asked for it, regia attaches to it.

// startWait is how long regia waits for a started session to be listed.
var startWait = 20 * time.Second

// pendingStart is a session a plugin started, waited for until it's listed.
type pendingStart struct {
	id, row, plugin string
	as              string
	reason, note    string
	attach          bool
	until           time.Time
}

type pluginStartedMsg struct {
	plugin, row   string
	start         plugins.Start
	attach        bool
	parent, after string
	out           string
	err           error
}

type pendingTickMsg struct{}

type pluginSwapMsg struct {
	plugin, summary string
	err             error
}

// pluginStart starts what plugin asked for, for row (an agent's or a group's id).
func (m *model) pluginStart(plugin, row string, s plugins.Start, attach bool) tea.Cmd {
	_, isGroup := m.layout.Groups[row]
	isGroup = isGroup && m.agents[row] == nil
	a := m.agents[row]
	if !isGroup && a == nil {
		m.flash("%s: can't start: %s is no longer listed", plugin, row)
		return nil
	}
	if s.As == plugins.AsSwap && (isGroup || a.Interactive) {
		m.flash("%s: start as swap needs a background agent's row", plugin)
		return nil
	}
	parent, after := row, ""
	if !isGroup {
		parent, after = m.layout.Parent[row], row
	}
	dir := expandHome(s.Dir)
	switch {
	case dir != "":
	case s.As == plugins.AsSwap && isDir(a.Cwd):
		dir = a.Cwd // the fresh session goes on where the old one worked
	default:
		dir, _ = m.layout.SpawnDir(row, m.agents) // as typing with the row selected
	}
	if !isDir(dir) {
		m.flash("%s: can't start in %s: no such directory", plugin, tildify(dir))
		return nil
	}
	m.flash("%s: starting %s in %s…", plugin, s.As, tildify(dir))
	return func() tea.Msg {
		var out string
		var err error
		if s.Model != "" {
			out, err = spawnAgentModel(dir, s.Model, s.Prompt)
		} else {
			out, err = spawnAgent(dir, s.Prompt)
		}
		return pluginStartedMsg{plugin: plugin, row: row, start: s, attach: attach, parent: parent, after: after, out: out, err: err}
	}
}

var newIDRe = regexp.MustCompile(`\b[0-9a-f]{8}\b`)

// pluginStarted places the new session and waits for it to be listed.
func (m *model) pluginStarted(msg pluginStartedMsg) tea.Cmd {
	if msg.err != nil {
		m.flash("%s: start failed: %v %s", msg.plugin, msg.err, strings.TrimSpace(msg.out))
		return nil
	}
	id := newIDRe.FindString(msg.out)
	if id == "" {
		m.flash("%s: started, but its id isn't in claude's output: %s", msg.plugin, strings.TrimSpace(msg.out))
		return nil
	}
	m.layout.PlaceNew(id, msg.parent, msg.after)
	for p := msg.parent; p != ""; p = m.layout.Parent[p] { // so its row is on screen
		delete(m.layout.Collapsed, p)
	}
	m.saveLayout()
	m.selectID = id
	if m.view != viewTree {
		m.setView(viewTree)
	}
	m.pending = &pendingStart{id: id, row: msg.row, plugin: msg.plugin, as: msg.start.As, reason: msg.start.Reason,
		note: msg.start.Note, attach: msg.attach, until: time.Now().Add(startWait)}
	m.flash("%s: started %s · waiting for claude agents to list it…", msg.plugin, id)
	m.fetching = true
	return tea.Batch(fetchCmd, pendingTick())
}

func pendingTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return pendingTickMsg{} })
}

// pendingTicked lists agents again while a started session isn't listed yet.
func (m *model) pendingTicked() tea.Cmd {
	p := m.pending
	if p == nil {
		return nil
	}
	if time.Now().After(p.until) {
		m.pending = nil
		m.flash("%s: %s wasn't listed by claude agents after %s: it stays where it was put", p.plugin, p.id, startWait)
		return nil
	}
	if m.fetching {
		return pendingTick()
	}
	m.fetching = true
	return tea.Batch(fetchCmd, pendingTick())
}

// pendingListed finishes a started session once claude agents lists it.
func (m *model) pendingListed() tea.Cmd {
	p := m.pending
	if p == nil || m.agents[p.id] == nil {
		return nil
	}
	m.pending = nil
	var cmds []tea.Cmd
	switch p.as {
	case plugins.AsCoordinator:
		var under []string
		if _, ok := m.layout.Groups[p.row]; ok && m.agents[p.row] == nil {
			under = m.layout.Children(p.row, m.agents) // the group's agents
		} else if m.agents[p.row] != nil {
			under = []string{p.row}
		}
		moved := 0
		for _, k := range under {
			if k != p.id && m.layout.Reparent(k, p.id, "", m.agents) == nil {
				moved++
			}
		}
		m.saveLayout()
		m.rebuild()
		m.flash("%s: %s coordinates %s", p.plugin, p.id, count(moved, "agent"))
	case plugins.AsSwap:
		reason := p.reason
		if reason == "" {
			reason = "a fresh session from " + p.plugin
		}
		old, who := p.row, "you, through "+p.plugin
		cmds = append(cmds, func() tea.Msg {
			s, err := swapSessions(who, old, p.id, reason, p.note)
			return pluginSwapMsg{plugin: p.plugin, summary: s, err: err}
		})
	default:
		m.flash("%s: %s is listed", p.plugin, p.id)
	}
	if p.attach {
		cmds = append(cmds, m.attachNow(p.id))
	}
	return tea.Batch(cmds...)
}

// swapSessions is group.SwapFor; a var so tests don't run claude rm.
var swapSessions = group.SwapFor

func (m *model) pluginSwapped(msg pluginSwapMsg) tea.Cmd {
	m.syncLayout()
	m.rebuild()
	if msg.err != nil {
		m.flash("%s: the swap was refused: %v", msg.plugin, msg.err)
	} else {
		m.flash("%s: %s", msg.plugin, msg.summary)
	}
	m.fetching = true
	return fetchCmd
}

// refreshStarts runs the starts plugins' refreshes asked for (once per key).
func (m *model) refreshStarts() tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range plugins.TakeStarts() {
		cmds = append(cmds, m.pluginStart(s.Plugin.Name, s.Target, s.Start, false))
	}
	return tea.Batch(cmds...)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[1:])
	}
	return p
}
