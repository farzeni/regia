package layout

import (
	"time"

	"github.com/farzeni/regia/internal/claude"
)

// ForgetAfter is how long an agent may be missing from `claude agents`
// before regia forgets it: its place, name, pins, archive and seen marks. A
// removed session leaves them behind, and every session's hooks read this
// file, so at dozens of removals a day it would only grow. The conversation
// stays findable in search's whole history (ctrl+g), which reads transcripts.
const ForgetAfter = 7 * 24 * time.Hour

// Prune notes agents missing from the listing (Gone) and forgets those
// missing for ForgetAfter; an agent listed again is no longer missing. Agents
// under a forgotten one take its place, as they are already shown. agents
// must be a full, fresh listing: an empty one prunes nothing. Returns true if
// the layout changed.
func (l *Layout) Prune(agents map[string]*claude.Agent, now time.Time) bool {
	if len(agents) == 0 {
		return false
	}
	if l.Gone == nil {
		l.Gone = map[string]time.Time{}
	}
	changed := false
	for id := range l.agentIDs() {
		since, missing := l.Gone[id]
		switch {
		case agents[id] != nil:
			if missing {
				delete(l.Gone, id)
				changed = true
			}
		case !missing:
			l.Gone[id] = now
			changed = true
		case now.Sub(since) >= ForgetAfter:
			l.forget(id)
			changed = true
		}
	}
	return changed
}

// agentIDs is every agent the layout says something about.
func (l *Layout) agentIDs() map[string]bool {
	ids := map[string]bool{}
	add := func(id string) {
		if id != "" && !l.IsGroup(id) {
			ids[id] = true
		}
	}
	for id, p := range l.Parent {
		add(id)
		add(p)
	}
	for p, kids := range l.Order {
		add(p)
		for _, k := range kids {
			add(k)
		}
	}
	for _, m := range []map[string]bool{l.Collapsed, l.Pinned, l.Muted, l.Pins} {
		for id := range m {
			add(id)
		}
	}
	for _, m := range []map[string]string{l.Alias, l.Grants, l.Claims} {
		for id := range m {
			add(id)
		}
	}
	for _, m := range []map[string]time.Time{l.Archived, l.Seen, l.Snoozed, l.Gone} {
		for id := range m {
			add(id)
		}
	}
	return ids
}

// forget drops everything about agent id; what sat under it moves into its
// place.
func (l *Layout) forget(id string) {
	parent := l.Parent[id]
	var kids []string
	for _, k := range l.Order[id] {
		if l.Parent[k] == id {
			kids = append(kids, k)
		}
	}
	for k, p := range l.Parent {
		if p == id && indexOf(kids, k) < 0 {
			kids = append(kids, k)
		}
	}
	sib := l.Order[parent]
	out := []string{}
	if i := indexOf(sib, id); i >= 0 {
		out = append(append(append(out, sib[:i]...), kids...), sib[i+1:]...)
	} else {
		out = append(append(out, sib...), kids...)
	}
	for _, k := range kids {
		l.Parent[k] = parent
	}
	if len(out) > 0 {
		l.Order[parent] = out
	}
	for p, ks := range l.Order { // a stray copy elsewhere
		if p != parent {
			l.Order[p] = remove(ks, id)
		}
	}
	delete(l.Order, id)
	delete(l.Parent, id)
	delete(l.Collapsed, id)
	delete(l.Pinned, id)
	delete(l.Muted, id)
	delete(l.Pins, id)
	delete(l.Alias, id)
	delete(l.Grants, id)
	delete(l.Claims, id)
	delete(l.Archived, id)
	delete(l.Seen, id)
	delete(l.Snoozed, id)
	delete(l.Gone, id)
}
