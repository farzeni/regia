package layout

import (
	"fmt"
	"time"

	"github.com/farzeni/regia/internal/claude"
)

// Swap is one hand-over: a fresh session took the place of an old one (its
// name, place in the tree, pin and children), and the old one was removed,
// so one task or epic keeps one name and one row while its session changes.
type Swap struct {
	From    string    `json:"from"`              // the old session's id
	Session string    `json:"session,omitempty"` // its Claude Code session id: how search's history finds it
	Title   string    `json:"title"`             // its name then
	At      time.Time `json:"at"`                // when
	By      string    `json:"by"`                // "you", or the agent that asked
	Reason  string    `json:"reason,omitempty"`  // why
	Note    string    `json:"note,omitempty"`    // a path or URL the caller gave, e.g. the handoff file
	RmErr   string    `json:"rm_err,omitempty"`  // claude rm refused the old session: why (it stays, archived)
}

// SwapIn puts b in a's place: a's place among its siblings, a's children,
// its name (a's title when it had no alias), pin, mute, collapsed and your
// grant, and the chain of earlier swaps with this one at the end. a's claim
// ends. a is archived under b until it is removed. title is a's name now.
func (l *Layout) SwapIn(a, b, title string, sw Swap, agents map[string]*claude.Agent) error {
	if a == b {
		return fmt.Errorf("a session can't replace itself")
	}
	for p, n := l.effParent(a, agents), 0; p != "" && n < 64; p, n = l.effParent(p, agents), n+1 {
		if p == b {
			return fmt.Errorf("%s sits under %s: it can't take its place", a, b)
		}
	}
	parent := l.effParent(a, agents)
	kids := l.Children(a, agents)
	own := l.Children(b, agents) // b's own agents stay with it, after a's

	// b leaves its own place first, so a child of a can't end up above it
	old := l.Parent[b]
	l.Order[old] = remove(l.Order[old], b)
	sib := l.materialize(parent, agents)
	if i := indexOf(sib, a); i >= 0 {
		sib[i] = b
	} else {
		sib = append(sib, b)
	}
	l.Order[parent] = remove(sib, a)
	l.Parent[b] = parent

	var under []string
	for _, k := range kids {
		if k != b {
			l.Parent[k] = b
			under = append(under, k)
		}
	}
	l.Order[b] = append(under, own...)
	delete(l.Order, a)

	if al := l.Alias[a]; al != "" {
		l.Alias[b] = al
	} else if title != "" {
		l.Alias[b] = title
	}
	l.Pinned[b] = true
	move := func(m map[string]bool) {
		if m[a] {
			m[b] = true
		}
		delete(m, a)
	}
	move(l.Pins)
	move(l.Muted)
	move(l.Collapsed)
	if g := l.Grants[a]; g != "" && l.Claims[a] == "" { // your grant goes on; a claim was a's own
		l.Grants[b] = g
	}
	delete(l.Grants, a)
	delete(l.Claims, a)
	delete(l.Archived, b)

	if l.Swaps == nil {
		l.Swaps = map[string][]Swap{}
	}
	sw.From, sw.Title = a, title
	if ag := agents[a]; ag != nil {
		sw.Session = ag.SessionID
	}
	chain := append([]Swap{}, l.Swaps[a]...)
	l.Swaps[b] = append(append(chain, l.Swaps[b]...), sw)
	delete(l.Swaps, a)

	l.Parent[a] = b // out of the way, under the one that replaced it
	l.Order[b] = append(l.Order[b], a)
	l.Archived[a] = sw.At
	delete(l.Alias, a)
	return nil
}

// Replaced is who took over from id, following the chain to the session in
// place now ("" when id was never swapped out).
func (l *Layout) Replaced(id string) (by string, sw Swap) {
	for b, chain := range l.Swaps {
		for _, s := range chain {
			if s.From == id {
				return b, s
			}
		}
	}
	return "", Swap{}
}

// ReplacedSession is Replaced for a Claude Code session id (a removed
// session search's history found).
func (l *Layout) ReplacedSession(sid string) (by string, sw Swap) {
	if sid == "" {
		return "", Swap{}
	}
	for b, chain := range l.Swaps {
		for _, s := range chain {
			if s.Session == sid {
				return b, s
			}
		}
	}
	return "", Swap{}
}

// NoteRmErr records that claude rm refused a, swapped out for b.
func (l *Layout) NoteRmErr(b, a, why string) {
	chain := l.Swaps[b]
	for i := range chain {
		if chain[i].From == a {
			chain[i].RmErr = why
		}
	}
}
