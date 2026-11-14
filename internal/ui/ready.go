package ui

import (
	"time"

	"github.com/farzeni/regia/internal/claude"
)

// Claude Code marks a session "done" when a turn ends with a result, which
// for a conversation is after every reply, not when the work is over. So a
// done session you haven't looked at yet is "ready": your turn, shown, in
// the inbox and counted. Looking at it (selected for a moment, or attached)
// makes it plain done again. And a question nobody answered for a day is
// stale: still shown, but no longer counted as needing you.

const (
	staleAfter = 24 * time.Hour
	seenAfter  = 2 * time.Second // selected this long = looked at
)

// classify derives ready and stale from what Claude Code reports.
func (m *model) classify(now time.Time) {
	if m.layout.Seen == nil && len(m.agents) > 0 {
		// first run with this feature: what's done already counts as seen
		m.layout.Seen = map[string]time.Time{}
		for _, a := range m.agents {
			if a.State == "done" {
				m.layout.Seen[a.ID] = now
			}
		}
		m.saveLayout()
	}
	for _, a := range m.agents {
		if a.State == "done" && !a.Interactive && !a.UpdatedAt.IsZero() && a.UpdatedAt.After(m.layout.Seen[a.ID]) {
			a.State = "ready"
		}
		a.Stale = a.State == "blocked" && now.Sub(waitedSince(a)) > staleAfter
	}
}

// markSeen records that you looked at id; a ready agent becomes done.
func (m *model) markSeen(id string, now time.Time) {
	a := m.agents[id]
	if a == nil || a.Interactive {
		return
	}
	if m.layout.Seen == nil {
		m.layout.Seen = map[string]time.Time{}
	}
	m.layout.Seen[id] = now
	if a.State == "ready" {
		a.State = "done"
	}
	m.saveLayout()
}

// dwell marks the selected agent seen once it stayed selected seenAfter.
func (m *model) dwell(now time.Time) {
	r := m.selected()
	if r == nil || r.Agent == nil {
		m.dwellID = ""
		return
	}
	if r.ID != m.dwellID {
		m.dwellID, m.dwellAt = r.ID, now
		return
	}
	if r.Agent.State == "ready" && now.Sub(m.dwellAt) >= seenAfter {
		m.markSeen(r.ID, now)
	}
}

// inboxRank orders the inbox: questions, then results to read, then stale.
func inboxRank(a *claude.Agent) int {
	switch {
	case a.State == "blocked" && !a.Stale:
		return 0
	case a.State == "ready":
		return 1
	case a.Stale:
		return 2
	}
	return 3 // the selected agent, kept while you read it
}
