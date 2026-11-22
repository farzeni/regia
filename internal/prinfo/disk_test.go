package prinfo

import (
	"os"
	"testing"
	"time"
)

// What one process learned from gh, another reads from disk; only a
// persisting process writes.
func TestDiskRoundTrip(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Cleanup(func() { mu.Lock(); persist = false; cache = map[string]Info{}; mu.Unlock() })

	Seed("/r", "feat", Info{OK: true, Number: 7, State: "open", Checks: "failing"})
	save()
	if _, err := os.Stat(diskPath()); err == nil {
		t.Fatal("not persisting: nothing written")
	}
	Persist()
	save()
	mu.Lock()
	cache = map[string]Info{}
	mu.Unlock()
	Load()
	if i, _ := Cached("/r", "feat"); i.Number != 7 || !i.NeedsLook() {
		t.Fatalf("loaded %+v", i)
	}

	// memory newer than disk wins
	Seed("/r", "feat", Info{OK: true, Number: 7, State: "merged"})
	Load()
	if i, _ := Cached("/r", "feat"); i.State != "merged" {
		t.Fatalf("disk overwrote a newer answer: %+v", i)
	}
	// entries older than a week are dropped
	mu.Lock()
	cache[key("/r", "old")] = Info{OK: true, Number: 1, Fetched: time.Now().Add(-8 * 24 * time.Hour)}
	mu.Unlock()
	save()
	mu.Lock()
	cache = map[string]Info{}
	mu.Unlock()
	Load()
	if _, ok := cache[key("/r", "old")]; ok {
		t.Fatal("a week-old entry is dropped")
	}
}
