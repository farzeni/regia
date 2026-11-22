package layout

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Change is what one write did to layout.json, entry by entry: for each field
// ("parent", "groups"…) and each key in it, the value before and after (nil:
// absent). A field that isn't a map uses the key "". It is how an agent's
// action is recorded and undone.
type Change map[string]map[string]Delta

type Delta struct {
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

// Empty says whether nothing changed.
func (c Change) Empty() bool { return len(c) == 0 }

// diff lists what differs between two layout files.
func diff(before, after []byte) Change {
	b, a := fields(before), fields(after)
	c := Change{}
	add := func(f, k string, x, y json.RawMessage) {
		if same(x, y) {
			return
		}
		if c[f] == nil {
			c[f] = map[string]Delta{}
		}
		c[f][k] = Delta{Before: x, After: y}
	}
	for f := range union(b, a) {
		bm, bok := entries(b[f])
		am, aok := entries(a[f])
		if !bok || !aok {
			add(f, "", b[f], a[f])
			continue
		}
		for k := range union(bm, am) {
			add(f, k, bm[k], am[k])
		}
	}
	return c
}

// Revert undoes c on the layout on disk, under the lock. It refuses, saying
// which entries, when any of them changed again since: undoing then would
// throw away a later change.
func Revert(c Change) error {
	_, _, err := Update(func(l *Layout) error {
		cur := fields(l.base)
		if cur == nil {
			cur = map[string]json.RawMessage{}
		}
		var moved []string
		for f, keys := range c {
			m, ok := entries(cur[f])
			for k, d := range keys {
				now := cur[f]
				if ok && k != "" {
					now = m[k]
				}
				if !same(now, d.After) {
					moved = append(moved, f+" "+k)
				}
			}
		}
		if len(moved) > 0 {
			sort.Strings(moved)
			return fmt.Errorf("changed again since (%s): not undone", strings.Join(moved, ", "))
		}
		for f, keys := range c {
			m, ok := entries(cur[f])
			for k, d := range keys {
				if !ok || k == "" {
					if d.Before == nil {
						delete(cur, f)
					} else {
						cur[f] = d.Before
					}
					continue
				}
				if d.Before == nil {
					delete(m, k)
				} else {
					m[k] = d.Before
				}
			}
			if ok {
				raw, err := json.Marshal(m)
				if err != nil {
					return err
				}
				cur[f] = raw
			}
		}
		b, err := json.Marshal(cur)
		if err != nil {
			return err
		}
		return l.adopt(b)
	})
	return err
}
