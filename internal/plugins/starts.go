package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// A plugin's refresh may ask regia to start an agent (Output.Start). Refreshes
// run every few minutes, so such a start must carry a key, and regia starts
// it once per key per plugin: the keys are kept in plugin-starts.json, next to
// layout.json, so a restart of regia (or a second regia) doesn't start it again.

// StartReq is a start a refresh asked for, for the agent or group Target.
type StartReq struct {
	Plugin *Manifest
	Target string
	Start  Start
}

// startKeysKept is how long a used key is remembered.
const startKeysKept = 30 * 24 * time.Hour

func startsPath() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "regia", "plugin-starts.json")
}

// claimStart records plugin's key as used and reports whether it was new.
// It holds a lock on the file, so of two regias only one starts it.
func claimStart(plugin, key string, now time.Time) bool {
	path := startsPath()
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return false
	}
	lk, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false
	}
	defer lk.Close()
	if syscall.Flock(int(lk.Fd()), syscall.LOCK_EX) != nil {
		return false
	}
	defer syscall.Flock(int(lk.Fd()), syscall.LOCK_UN)

	used := map[string]time.Time{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &used)
	}
	k := plugin + "\x00" + key
	if _, ok := used[k]; ok {
		return false
	}
	for x, at := range used {
		if now.Sub(at) > startKeysKept {
			delete(used, x)
		}
	}
	used[k] = now
	b, _ := json.Marshal(used)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) != nil || os.Rename(tmp, path) != nil {
		return false
	}
	return true
}

// queueStart keeps a refresh's start for the UI (TakeStarts), once per key.
// A start without a key is not run: it is noted as the result's warning.
func queueStart(p *Manifest, target string, out *Output) (warn string) {
	s := out.Start
	if s == nil {
		return ""
	}
	if s.Key == "" {
		return "a refresh's start needs a \"key\" (started once per key): ignored"
	}
	if !claimStart(p.Name, s.Key, time.Now()) {
		return ""
	}
	reg.Lock()
	reg.starts = append(reg.starts, StartReq{Plugin: p, Target: target, Start: *s})
	reg.Unlock()
	return ""
}

// TakeStarts returns the starts refreshes asked for since the last call.
func TakeStarts() []StartReq {
	reg.Lock()
	defer reg.Unlock()
	s := reg.starts
	reg.starts = nil
	return s
}
