package layout

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
)

// Several processes write layout.json: regia itself, a second regia, and
// agents through `regia mcp`. Every write holds a lock, and a save from a copy
// loaded earlier merges with what others saved since, key by key (see merge),
// so no one's change is lost.

// locked runs do while holding an exclusive lock on layout.json.lock.
func locked(do func() error) error {
	p := Path()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return do()
}

// Update loads the layout fresh, applies fn and saves it, all under the lock:
// how a process that doesn't keep a layout in memory (`regia mcp`) changes it.
// It returns the layout as saved and what changed. Nothing is saved when fn
// fails.
func Update(fn func(l *Layout) error) (*Layout, Change, error) {
	var l *Layout
	var ch Change
	err := locked(func() error {
		l = Load()
		before := l.base
		if err := fn(l); err != nil {
			return err
		}
		if err := l.write(); err != nil {
			return err
		}
		ch = diff(before, l.base)
		return nil
	})
	return l, ch, err
}

// write saves l as it is, recording what was written as its base.
func (l *Layout) write() error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	p := Path()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	l.base = b
	if fi, err := os.Stat(p); err == nil {
		l.mtime = fi.ModTime()
	}
	return nil
}

// adopt replaces l's saved fields with the JSON b (a merge result), keeping
// what isn't saved (Spawns).
func (l *Layout) adopt(b []byte) error {
	n := &Layout{}
	if err := json.Unmarshal(b, n); err != nil {
		return err
	}
	n.Spawns = l.Spawns
	n.fill()
	*l = *n
	return nil
}

// fields splits a layout file into its top-level fields; nil when empty or
// unreadable.
func fields(b []byte) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if len(bytes.TrimSpace(b)) == 0 || json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// entries splits a field holding a map into its entries; ok false when the
// field is something else (a number).
func entries(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if raw == nil || string(raw) == "null" {
		return map[string]json.RawMessage{}, true
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil, false
	}
	return m, true
}

// same compares two JSON values ignoring layout; nil (absent) only equals nil.
func same(a, b json.RawMessage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

// merge3 combines ours (loaded from base, then changed here) with theirs
// (what is on disk now, saved by someone else since base): for every entry of
// every map, and every other field, ours wins where we changed it and theirs
// is kept everywhere else. A tree that comes out slightly off (a child listed
// in two orders) heals itself: Parent is the truth, Order only sorts.
func merge3(base, ours, theirs []byte) ([]byte, error) {
	b, o, t := fields(base), fields(ours), fields(theirs)
	if o == nil {
		return theirs, nil
	}
	out := map[string]json.RawMessage{}
	for k := range union(b, o, t) {
		bm, bok := entries(b[k])
		om, ook := entries(o[k])
		tm, tok := entries(t[k])
		if !bok || !ook || !tok { // not a map: the whole field
			if pick := pick(b[k], o[k], t[k]); pick != nil {
				out[k] = pick
			}
			continue
		}
		m := map[string]json.RawMessage{}
		for e := range union(bm, om, tm) {
			if pick := pick(bm[e], om[e], tm[e]); pick != nil {
				m[e] = pick
			}
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		out[k] = raw
	}
	return json.Marshal(out)
}

// pick is ours when we changed it since base, else theirs (nil: absent).
func pick(base, ours, theirs json.RawMessage) json.RawMessage {
	if !same(base, ours) {
		return ours
	}
	return theirs
}

func union(ms ...map[string]json.RawMessage) map[string]bool {
	u := map[string]bool{}
	for _, m := range ms {
		for k := range m {
			u[k] = true
		}
	}
	return u
}
