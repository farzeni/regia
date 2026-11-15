package ui

import (
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
)

// Claude Code marks a session "done" when a turn ends with a result, which
// for a conversation is after every reply, not when the work is over. So a
// done session you haven't looked at yet is "ready": your turn, shown, in
// the inbox and counted. Looking at it (selected for a moment, or attached)
// makes it plain done again. And a question from before today is stale:
// still shown, but no longer counted as needing you.

const (
	seenAfter = 2 * time.Second // selected this long = looked at
	dayStarts = 4               // the hour "today" begins: work past midnight still counts as the same day
)

// dayStart is when today began: 4am, or yesterday's 4am before that hour.
func dayStart(now time.Time) time.Time {
	d := time.Date(now.Year(), now.Month(), now.Day(), dayStarts, 0, 0, 0, now.Location())
	if now.Before(d) {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// fromToday reports whether a's last change happened today (see dayStart).
func fromToday(a *claude.Agent, now time.Time) bool {
	return !waitedSince(a).Before(dayStart(now))
}

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
		a.Stale = a.State == "blocked" && !fromToday(a, now)
		a.Snoozed = false
		if at, ok := m.layout.Snoozed[a.ID]; ok {
			if a.State == "blocked" && !waitedSince(a).After(at) {
				a.Snoozed = true
			} else { // it did something since: back in the inbox
				delete(m.layout.Snoozed, a.ID)
				m.saveLayoutQuiet()
			}
		}
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
	if r == nil || r.Agent == nil || m.view == viewSearch { // a search hit is not the session's latest result
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

// snoozeKey (Z) takes the selected question off the inbox until the agent
// does something new; on a snoozed one it brings it back. A result to read
// (ready) has nothing to wait for: snoozing it just marks it seen.
func (m *model) snoozeKey(r *layout.Row) {
	if r == nil || r.Agent == nil || r.Agent.Interactive {
		return
	}
	a := r.Agent
	m.syncLayout()
	switch {
	case a.Snoozed:
		delete(m.layout.Snoozed, a.ID)
		a.Snoozed = false
		m.flash("%s is back in the inbox", r.Title)
	case a.State == "ready":
		m.markSeen(a.ID, time.Now())
		m.flash("%s marked as read", r.Title)
		return
	case a.State == "blocked":
		if m.layout.Snoozed == nil {
			m.layout.Snoozed = map[string]time.Time{}
		}
		m.layout.Snoozed[a.ID] = waitedSince(a)
		a.Snoozed = true
		m.flash("snoozed %s: back in the inbox when it does something new · Z again undoes", r.Title)
	default:
		m.flash("Z snoozes a question waiting for you")
		return
	}
	m.saveLayout()
}

// inboxRank orders the inbox: questions, then results to read, then stale.
func inboxRank(a *claude.Agent) int {
	switch {
	case a.State == "blocked" && !a.Stale:
		return 0
	case a.State == "ready" || prNeedsLook(a):
		return 1
	case a.Stale:
		return 2
	}
	return 3 // the selected agent, kept while you read it
}
