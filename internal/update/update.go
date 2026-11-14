// Package update keeps regia current: it compares this binary with the
// latest version published on the Go module proxy, reinstalls it in place
// with `go install`, and updates the Claude Code plugin.
//
// Only binaries installed with `go install` are updated. A binary built from
// a checkout (it carries git info) is a development build: it may be ahead
// of what's published, so updating it would be a downgrade.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	Module = "github.com/farzeni/regia"
	Pkg    = Module + "/cmd/regia"
	Plugin = "regia@regia"
)

// Build describes the running binary.
type Build struct {
	Version  string // module version: v1.2.3 or a pseudo-version; "(devel)" if unknown
	Revision string // git commit, for a build from a checkout
	Dirty    bool   // checkout had uncommitted changes
	Dev      bool   // built from a checkout, not installed from the proxy
}

// Current reads the running binary's build info.
func Current() Build {
	b := Build{Version: "(devel)"}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		b.Dev = true
		return b
	}
	b.Version = bi.Main.Version
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Revision, b.Dev = s.Value, true
		case "vcs.modified":
			b.Dirty = s.Value == "true"
		}
	}
	if b.Version == "" || b.Version == "(devel)" {
		b.Dev = true
	}
	return b
}

func (b Build) String() string {
	if b.Dev {
		rev := b.Revision
		if len(rev) > 7 {
			rev = rev[:7]
		}
		if b.Dirty {
			rev += "+changes"
		}
		return strings.TrimSpace("dev build " + rev)
	}
	return b.Version
}

// Branch is what gets published: regia is released from main.
const Branch = "main"

// Latest asks the Go module proxy (through `go list`) for the newest version:
// the head of main. Not @latest: the proxy caches that answer for a while,
// so right after a push it still names an older commit, while a branch query
// is resolved fresh. With tags, main's pseudo-version still sorts after them.
func Latest(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-json", Module+"@"+Branch)
	cmd.Dir = os.TempDir() // outside any module or workspace
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			return "", fmt.Errorf("go list: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("go list: %w (is Go installed?)", err)
	}
	var v struct{ Version string }
	if err := json.Unmarshal(out, &v); err != nil || v.Version == "" {
		return "", fmt.Errorf("go list: unexpected output %q", out)
	}
	return v.Version, nil
}

// Newer reports whether latest is a later version than current
// (semver order; pseudo-versions sort by commit time).
func Newer(latest, current string) bool {
	if !semver.IsValid(latest) {
		return false
	}
	return !semver.IsValid(current) || semver.Compare(latest, current) > 0
}

// Executable is where the running binary lives (symlinks resolved).
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Install puts version of regia where the running binary is, with
// `go install` (GOBIN set to its directory). The running process is not
// affected; the next start uses the new binary.
func Install(ctx context.Context, version string) (string, error) {
	exe, err := Executable()
	if err != nil {
		return "", err
	}
	if filepath.Base(exe) != "regia" {
		return "", fmt.Errorf("the binary is named %s: go install would write regia next to it instead", filepath.Base(exe))
	}
	cmd := exec.CommandContext(ctx, "go", "install", Pkg+"@"+version)
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "GOBIN="+filepath.Dir(exe), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go install: %s", strings.TrimSpace(string(out)))
	}
	return exe, nil
}

// UpdatePlugin runs `claude plugin update regia@regia`; the hook it carries
// may have changed with the binary.
func UpdatePlugin(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "claude", "plugin", "update", Plugin).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ---------------------------------------------------------------- auto-update

// state remembers the last check, so the UI asks the proxy at most once a day.
type state struct {
	CheckedAt time.Time `json:"checkedAt"`
	Latest    string    `json:"latest"`
}

func statePath() string {
	d, _ := os.UserCacheDir()
	return filepath.Join(d, "regia", "update.json")
}

// Settings live in ~/.config/regia/settings.json.
type Settings struct {
	AutoUpdate *bool `json:"autoUpdate,omitempty"` // nil: on
}

func settingsPath() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "regia", "settings.json")
}

func LoadSettings() Settings {
	var s Settings
	if b, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func (s Settings) Save() error {
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.MkdirAll(filepath.Dir(settingsPath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), append(b, '\n'), 0o644)
}

// AutoEnabled: on unless turned off (regia update --auto=off or
// REGIA_NO_AUTOUPDATE=1).
func AutoEnabled() bool {
	if os.Getenv("REGIA_NO_AUTOUPDATE") != "" {
		return false
	}
	s := LoadSettings()
	return s.AutoUpdate == nil || *s.AutoUpdate
}

// Seams for tests.
var (
	currentFn = Current
	latestFn  = Latest
	installFn = Install
	pluginFn  = UpdatePlugin
)

// Result is what an automatic update did.
type Result struct {
	From, To string
	Err      error
}

// Auto is the UI's background update: for an installed (not dev) build,
// at most once a day, install the latest version if it's newer and
// update the plugin. ok is false when nothing was attempted.
func Auto(ctx context.Context, now time.Time) (r Result, ok bool) {
	b := currentFn()
	if b.Dev || !AutoEnabled() {
		return r, false
	}
	var st state
	if raw, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	if now.Sub(st.CheckedAt) < 24*time.Hour {
		return r, false
	}
	latest, err := latestFn(ctx)
	st.CheckedAt = now // also on failure: no retry storm while offline
	if err == nil {
		st.Latest = latest
	}
	if raw, e := json.Marshal(st); e == nil {
		_ = os.MkdirAll(filepath.Dir(statePath()), 0o755)
		_ = os.WriteFile(statePath(), raw, 0o644)
	}
	if err != nil || !Newer(latest, b.Version) {
		return r, false
	}
	r = Result{From: b.Version, To: latest}
	if _, r.Err = installFn(ctx, latest); r.Err == nil {
		_, _ = pluginFn(ctx) // best effort: the binary is what matters
	}
	return r, true
}
