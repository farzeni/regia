package prinfo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// The cache on disk (~/.cache/regia/prinfo.json): regia writes it after each
// gh answer, so another process (regia mcp) can read PR status without
// asking gh, and a regia that opens shows it before gh answers again.

// diskTTL drops entries nobody asked about for a week.
const diskTTL = 7 * 24 * time.Hour

var persist bool // Persist was called: fetch writes the cache to disk

func diskPath() string {
	d, _ := os.UserCacheDir()
	return filepath.Join(d, "regia", "prinfo.json")
}

// Persist loads the cache from disk and has every gh answer from now on
// written back. Called once by the TUI.
func Persist() {
	Load()
	mu.Lock()
	persist = true
	mu.Unlock()
}

// Load adds to the cache what the disk knows and memory doesn't (or knows
// older). Read-only: regia mcp calls it before reading.
func Load() {
	b, err := os.ReadFile(diskPath())
	if err != nil {
		return
	}
	var disk map[string]Info
	if json.Unmarshal(b, &disk) != nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	for k, i := range disk {
		if old, ok := cache[k]; !ok || old.Fetched.Before(i.Fetched) {
			cache[k] = i
		}
	}
}

// save writes the cache (called after a fetch when persisting).
func save() {
	mu.Lock()
	if !persist {
		mu.Unlock()
		return
	}
	out := make(map[string]Info, len(cache))
	for k, i := range cache {
		if time.Since(i.Fetched) < diskTTL {
			out[k] = i
		}
	}
	mu.Unlock()
	b, err := json.Marshal(out)
	if err != nil {
		return
	}
	p := diskPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.CreateTemp(filepath.Dir(p), ".prinfo-*")
	if err != nil {
		return
	}
	_, werr := f.Write(b)
	if cerr := f.Close(); werr != nil || cerr != nil || os.Rename(f.Name(), p) != nil {
		_ = os.Remove(f.Name())
	}
}
