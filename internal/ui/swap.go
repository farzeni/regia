package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

// A session swap (regia swap, an agent's swap tool) leaves a chain on the row
// that took over: these lines show it on the card, and on the replaced
// session wherever it still shows up (archived under the new one, or found in
// search's history once removed).

// swapFields are the card's lines about the sessions this one replaced: the
// last swap with its note, how many before it, and a claude rm that refused.
func (m *model) swapFields(id string) (from, refused string) {
	chain := m.layout.Swaps[id]
	if len(chain) == 0 {
		return "", ""
	}
	last := chain[len(chain)-1]
	from = swapLine(last.Title, last)
	if n := len(chain) - 1; n > 0 {
		from += fmt.Sprintf("  (%d before it: ctrl+g finds them)", n)
	}
	for _, s := range chain {
		if s.RmErr != "" && m.agents[s.From] != nil {
			refused = fmt.Sprintf("claude rm refused %s: %s (x removes it once fixed)", s.Title, s.RmErr)
		}
	}
	return from, refused
}

// replacedLine is what a replaced session shows: who took its place.
func (m *model) replacedLine(id, sid string) string {
	b, sw := m.layout.Replaced(id)
	if b == "" {
		b, sw = m.layout.ReplacedSession(sid)
	}
	if b == "" {
		return ""
	}
	title := b
	if al := m.layout.Alias[b]; al != "" {
		title = al
	} else if a := m.agents[b]; a != nil {
		title = a.Name
	}
	return swapLine(title+" ("+b+")", sw)
}

// swapLine is "<who> · <day> <time> · <reason> · <note>".
func swapLine(who string, s layout.Swap) string {
	return strings.Join(nonEmpty(who, dayLabel(s.At)+" "+clock(s.At), s.Reason, s.Note), " · ")
}

// retireSession removes a session that swapped itself out; a var so tests
// don't run claude.
var retireSession = group.Retire

type retiredMsg struct {
	id  string
	err error
}

// retireSwapped removes the sessions that swapped themselves out and are no
// longer working, in case their Stop hook didn't (the plugin not updated yet,
// regia not on the PATH there). group.Retire checks again before claude rm.
func (m *model) retireSwapped() tea.Cmd {
	var cmds []tea.Cmd
	for id := range m.layout.Retire {
		a := m.agents[id]
		if a == nil || a.State == "busy" || m.retiring[id] {
			continue
		}
		if m.retiring == nil {
			m.retiring = map[string]bool{}
		}
		m.retiring[id] = true
		cmds = append(cmds, func() tea.Msg { return retiredMsg{id, retireSession(id)} })
	}
	return tea.Batch(cmds...)
}

func (m *model) retired(msg retiredMsg) tea.Cmd {
	delete(m.retiring, msg.id)
	m.syncLayout()
	if msg.err != nil {
		m.flash("swapped-out session %s stays: %v", msg.id, msg.err)
	}
	m.fetching = true
	return fetchCmd
}
