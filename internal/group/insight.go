package group

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/gitinfo"
)

// Reads for agents beyond the tree: the inbox (who needs the user), a
// group's timeline (what happened) and the files an agent changed. Same
// scope as tree: what the caller may read.

// --- inbox -------------------------------------------------------------------

// InboxItem is an agent that needs the user, and why.
type InboxItem struct {
	Node
	Why     string `json:"why"`             // waiting, stuck, failed
	Stuck   string `json:"stuck,omitempty"` // why it looks stuck
	Since   string `json:"since,omitempty"` // how long it has been like this
	Snoozed bool   `json:"snoozed,omitempty"`
}

// Inbox lists the agents the caller may read that need the user: waiting on
// a question, looking stuck, or failed; in that order, the longest waiting
// first. Archived agents are left out.
func Inbox(self string) ([]InboxItem, error) {
	w, err := read(self)
	if err != nil {
		return nil, err
	}
	c := w.who(self)
	now := time.Now()
	rank := map[string]int{"waiting": 0, "stuck": 1, "failed": 2}
	type item struct {
		InboxItem
		at time.Time
	}
	var items []item
	for id, a := range w.agents {
		if a == nil || w.l.IsArchived(id) || !c.canRead(w, id) {
			continue
		}
		claude.Enrich(a)
		it := InboxItem{Snoozed: a.Snoozed}
		switch {
		case a.State == "blocked":
			it.Why = "waiting"
		case a.State == "busy" && claude.Health(a, now) != "":
			it.Why, it.Stuck = "stuck", claude.Health(a, now)
		case a.State == "failed":
			it.Why = "failed"
		default:
			continue
		}
		it.Node = w.node(id, now)
		at := a.UpdatedAt
		if at.IsZero() {
			at = a.StartedAt
		}
		if !at.IsZero() {
			it.Since = since(now.Sub(at))
		}
		items = append(items, item{it, at})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if ri, rj := rank[items[i].Why], rank[items[j].Why]; ri != rj {
			return ri < rj
		}
		if !items[i].at.Equal(items[j].at) {
			return items[i].at.Before(items[j].at)
		}
		return items[i].ID < items[j].ID
	})
	out := make([]InboxItem, len(items))
	for i, it := range items {
		out[i] = it.InboxItem
	}
	return out, nil
}

func since(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// --- timeline ----------------------------------------------------------------

// Event is one line of a timeline.
type Event struct {
	At   string `json:"at"` // RFC 3339
	Who  string `json:"who"`
	ID   string `json:"id"` // the agent's id
	What string `json:"what"`
	Text string `json:"text,omitempty"`
}

// maxEvents caps a timeline: the newest are kept.
const maxEvents = 150

// TimelineOut is a timeline and what it covers.
type TimelineOut struct {
	Of      string  `json:"of"` // the group or agent's title
	Since   string  `json:"since"`
	Events  []Event `json:"events"`
	Dropped int     `json:"dropped,omitempty"` // older events left out
}

// Timeline is what happened under id (a group, or an agent and the agents
// under it; "": what the caller may read) since a time, oldest first: the
// agents' state changes, the PRs they opened and the commits on their
// branches.
func Timeline(self, id string, from time.Time) (TimelineOut, error) {
	w, err := read(self)
	if err != nil {
		return TimelineOut{}, err
	}
	c := w.who(self)
	if id == "" {
		id = c.readRoot()
	}
	if id != Everything && !w.exists(id) {
		return TimelineOut{}, fmt.Errorf("no group or agent %q in regia's tree (ids are in tree)", id)
	}
	if !c.canRead(w, id) {
		return TimelineOut{}, fmt.Errorf("%s is outside what you may read", id)
	}
	var as []*claude.Agent
	for aid, a := range w.agents {
		if a == nil || !c.canRead(w, aid) {
			continue
		}
		if id == Everything || aid == id || w.under(aid, id) {
			as = append(as, a)
		}
	}
	claude.SortByStart(as)
	title := "the whole tree"
	if id != Everything {
		title = w.title(id)
	}
	out := TimelineOut{Of: title, Since: from.Local().Format(time.RFC3339)}

	type ev struct {
		Event
		at time.Time
	}
	var evs []ev
	add := func(at time.Time, a *claude.Agent, what, text string) {
		if at.Before(from) {
			return
		}
		evs = append(evs, ev{Event{At: at.Local().Format(time.RFC3339), Who: w.title(a.ID), ID: a.ID, What: what, Text: trunc(claude.Tidy(text), 300)}, at})
	}
	for _, a := range as {
		if a.Interactive {
			continue
		}
		info := claude.ReadTimeline(a.ID)
		for i, e := range info.Events {
			switch e.State {
			case "busy":
				if i == 0 {
					add(e.At, a, "started", e.Detail)
				} else {
					add(e.At, a, "back to work", e.Detail)
				}
			case "blocked":
				add(e.At, a, "waiting", e.Detail)
			case "done", "failed":
				add(e.At, a, e.State, e.Detail)
			}
		}
		for _, p := range a.PRs {
			at, ok := info.Links[p.Href]
			if !ok {
				at = a.UpdatedAt
			}
			kind := p.Kind
			if kind == "" {
				kind = "PR"
			}
			add(at, a, "opened "+kind, strings.TrimSpace("#"+p.ID+" "+p.Href))
		}
	}
	seen := map[string]bool{}
	for k, ag := range LogKeys(as) {
		if k.Since.Before(from) {
			k.Since = from.Truncate(time.Minute)
		}
		for _, cm := range gitinfo.ReadLog(k) {
			if !seen[cm.Hash] {
				seen[cm.Hash] = true
				add(cm.At, Committer(ag, cm.At), "commit", cm.Hash+" "+cm.Subject)
			}
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].at.Before(evs[j].at) })
	if len(evs) > maxEvents {
		out.Dropped = len(evs) - maxEvents
		evs = evs[out.Dropped:]
	}
	out.Events = make([]Event, len(evs))
	for i, e := range evs {
		out.Events[i] = e.Event
	}
	return out, nil
}

// LogKeys groups agents by the git log that covers their work: their
// directory and branch, read since the first of them started.
func LogKeys(as []*claude.Agent) map[gitinfo.LogKey][]*claude.Agent {
	type dr struct{ dir, ref string }
	by := map[dr][]*claude.Agent{}
	for _, a := range as {
		if a.Interactive || a.Cwd == "" {
			continue
		}
		ref := a.Branch
		if ref == "" {
			ref = "HEAD"
		}
		k := dr{a.Cwd, ref}
		by[k] = append(by[k], a)
	}
	out := map[gitinfo.LogKey][]*claude.Agent{}
	for k, ag := range by {
		since := ag[0].StartedAt
		for _, a := range ag {
			if a.StartedAt.Before(since) {
				since = a.StartedAt
			}
		}
		out[gitinfo.LogKey{Dir: k.dir, Ref: k.ref, Since: since.Truncate(time.Minute)}] = ag
	}
	return out
}

// Committer is the agent a commit is put down to: of those sharing the
// branch (in start order), the last one started before it.
func Committer(ag []*claude.Agent, at time.Time) *claude.Agent {
	who := ag[0]
	for _, a := range ag {
		if !a.StartedAt.After(at) {
			who = a
		}
	}
	return who
}

// --- changes -----------------------------------------------------------------

// Change is one file an agent edited, its edits summed.
type Change struct {
	File    string `json:"file"` // relative to the agent's directory when inside it
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Line    int    `json:"line,omitempty"` // where its last edit starts
}

// ChangesOut is the files an agent edited, the most recent first.
type ChangesOut struct {
	Agent  string   `json:"agent"`
	Cwd    string   `json:"cwd,omitempty"`
	Branch string   `json:"branch,omitempty"`
	Files  []Change `json:"files"`
}

// Changes lists the files agent id edited in its conversation, newest first.
func Changes(self, id string) (ChangesOut, error) {
	w, err := read(self)
	if err != nil {
		return ChangesOut{}, err
	}
	a := w.agents[id]
	if a == nil {
		return ChangesOut{}, fmt.Errorf("no agent %q in regia's tree (ids are in tree)", id)
	}
	if !w.who(self).canRead(w, id) {
		return ChangesOut{}, fmt.Errorf("%s is outside what you may read", id)
	}
	path := claude.TranscriptPath(a)
	if path == "" {
		return ChangesOut{}, errors.New("no conversation found for " + id)
	}
	out := ChangesOut{Agent: w.title(id), Cwd: a.Cwd, Branch: a.Branch, Files: []Change{}}
	for _, f := range claude.ChangedFiles(claude.Conversation(claude.Messages(path)), a.Cwd) {
		out.Files = append(out.Files, Change{File: f.Rel, Added: f.Added, Removed: f.Removed, Line: f.Line})
	}
	return out, nil
}

// --- labels: rename_agent, pin ---------------------------------------------

// canLabel says why c may not rename or pin agent id ("" if it may): as
// canChange, but an organizer may label itself.
func (c Caller) canLabel(w *world, id string) string {
	if !w.isAgent(id) {
		if w.isGroup(id) {
			return fmt.Sprintf("%s is a group: rename_group renames groups", id)
		}
		return fmt.Sprintf("%s is not an agent in regia's tree", id)
	}
	if id == c.ID && c.Scope != "" {
		return ""
	}
	return c.canChange(w, id)
}

// RenameAgent sets the name regia shows for agent id ("" goes back to its
// own name). Claude Code's name for the session is not touched.
func RenameAgent(self, id, name string) (string, error) {
	name = strings.TrimSpace(strings.Join(strings.Fields(name), " "))
	return change(self, "rename_agent", func(w *world, c Caller) (string, error) {
		if why := c.canLabel(w, id); why != "" {
			return "", errors.New(why)
		}
		old := w.title(id)
		if name == "" {
			delete(w.l.Alias, id)
			return fmt.Sprintf("%q is called by its own name again (%s)", old, w.title(id)), nil
		}
		w.l.Alias[id] = name
		return fmt.Sprintf("renamed agent %q to %q", old, name), nil
	})
}

// Pin keeps agents listed in regia when they are done (on), or lets them be
// hidden again.
func Pin(self string, ids []string, on bool) (string, error) {
	tool := "pin"
	if !on {
		tool = "unpin"
	}
	return change(self, tool, func(w *world, c Caller) (string, error) {
		if len(ids) == 0 {
			return "", errors.New("no agents given")
		}
		for _, id := range ids {
			if why := c.canLabel(w, id); why != "" {
				return "", errors.New(why)
			}
		}
		for _, id := range ids {
			if on {
				w.l.Pins[id] = true
			} else {
				delete(w.l.Pins, id)
			}
		}
		if on {
			return "pinned " + plural2(len(ids), "agent", "agents"), nil
		}
		return "unpinned " + plural2(len(ids), "agent", "agents"), nil
	})
}
