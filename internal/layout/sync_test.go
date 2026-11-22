package layout

import (
	"strings"
	"sync"
	"testing"
)

// regia keeps a layout in memory; an agent changes the file meanwhile. regia's
// next save keeps the agent's change and its own.
func TestSaveMergesOtherWriters(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	l := Load()
	l.Groups["g:a"] = "alpha"
	l.Parent["x"] = "g:a"
	l.Sidebar = 30
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}

	// another process: a new group, x moved into it, an alias
	if _, _, err := Update(func(o *Layout) error {
		o.Groups["g:b"] = "beta"
		o.Parent["x"] = "g:b"
		o.Alias["y"] = "why"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// regia, from its stale copy: renames alpha, archives z, widens the list
	l.Groups["g:a"] = "ALPHA"
	l.Archive("z")
	l.Sidebar = 40
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}

	for _, got := range []*Layout{l, Load()} {
		if got.Groups["g:a"] != "ALPHA" || got.Groups["g:b"] != "beta" || got.Parent["x"] != "g:b" ||
			got.Alias["y"] != "why" || !got.IsArchived("z") || got.Sidebar != 40 {
			t.Fatalf("merged layout lost a change: groups %v parent %v alias %v archived %v sidebar %d",
				got.Groups, got.Parent, got.Alias, got.Archived, got.Sidebar)
		}
	}
	if l.ChangedOnDisk() {
		t.Fatal("after a save the copy matches the file")
	}

	// deleting an entry merges too: theirs removed one we didn't touch
	if _, _, err := Update(func(o *Layout) error { delete(o.Alias, "y"); return nil }); err != nil {
		t.Fatal(err)
	}
	l.Groups["g:c"] = "gamma"
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got.Alias["y"] != "" || got.Groups["g:c"] != "gamma" {
		t.Fatalf("delete lost: alias %v groups %v", got.Alias, got.Groups)
	}
}

// Many writers at once: every change lands.
func TestConcurrentUpdates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('a' + i))
			Update(func(l *Layout) error { l.Alias[id] = id; return nil })
		}()
	}
	wg.Wait()
	if n := len(Load().Alias); n != 20 {
		t.Fatalf("%d of 20 changes kept", n)
	}
}

func TestUpdateChangeAndRevert(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	Update(func(l *Layout) error { l.Groups["g:a"] = "alpha"; l.Parent["x"] = "g:a"; return nil })

	_, ch, err := Update(func(l *Layout) error {
		l.Groups["g:b"] = "beta"
		l.Parent["x"] = "g:b"
		l.Sidebar = 50
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ch["groups"]) != 1 || len(ch["parent"]) != 1 || ch["sidebar"] == nil {
		t.Fatalf("change %+v", ch)
	}
	if err := Revert(ch); err != nil {
		t.Fatal(err)
	}
	if l := Load(); l.Groups["g:b"] != "" || l.Parent["x"] != "g:a" || l.Sidebar != 0 || l.Groups["g:a"] != "alpha" {
		t.Fatalf("revert: groups %v parent %v sidebar %d", l.Groups, l.Parent, l.Sidebar)
	}

	// changed again since: refused, nothing touched
	_, ch, _ = Update(func(l *Layout) error { l.Parent["x"] = "g:b2"; l.Alias["x"] = "ex"; return nil })
	Update(func(l *Layout) error { l.Parent["x"] = "elsewhere"; return nil })
	if err := Revert(ch); err == nil || !strings.Contains(err.Error(), "parent x") {
		t.Fatalf("revert over a later change: %v", err)
	}
	if l := Load(); l.Alias["x"] != "ex" || l.Parent["x"] != "elsewhere" {
		t.Fatal("a refused revert must change nothing")
	}

	// a failing update saves nothing
	if _, _, err := Update(func(l *Layout) error { l.Alias["q"] = "q"; return errFail }); err == nil || Load().Alias["q"] != "" {
		t.Fatal("failed update was saved")
	}
}

var errFail = errString("fail")

type errString string

func (e errString) Error() string { return string(e) }
