// Package claude reads Claude Code's view of its sessions: the supported
// `claude agents --json --all` listing, merged with the read-only job state
// files under ~/.claude/jobs and the session transcripts (history). Every
// internal field is treated as optional.
package claude

import (
	"encoding/json"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Agent is one Claude Code session, merged from `claude agents --json --all`
// (the supported interface) and the job's state files under ~/.claude/jobs
// (read-only, best effort: internal format, every field optional).
type Agent struct {
	ID          string // short id used by `claude attach`; "s:<8>" for interactive sessions
	SessionID   string
	Name        string
	Cwd         string
	Kind        string // background | interactive
	State       string // normalized: blocked busy idle done failed; the UI adds ready (done, not seen yet)
	StartedAt   time.Time
	Interactive bool

	// From ~/.claude/jobs/<id>/state.json
	Detail     string
	Needs      string
	Suggested  string
	Intent     string
	Branch     string
	Tokens     int
	UpdatedAt  time.Time
	PRs        []Link
	ForkParent string // short id
	Transcript string // session transcript (jsonl)
	JobState   string // the job file's own state, raw (see ListingStamp)

	Stale   bool // set by the UI: waiting on you since before today
	Snoozed bool // set by the UI: a question you put off (Z) until the agent does something new
}

// YourTurn: the agent waits on you, with a question (blocked, not stale or
// snoozed) or a result you haven't looked at yet (ready).
func (a *Agent) YourTurn() bool {
	return (a.State == "blocked" && !a.Stale && !a.Snoozed) || a.State == "ready"
}

type Link struct {
	ID   string `json:"id"`
	Href string `json:"href"`
	Kind string `json:"kind"`
}

type rawAgent struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	Status    string `json:"status"`
	StartedAt int64  `json:"startedAt"`
}

type rawJob struct {
	State      string          `json:"state"`
	Detail     string          `json:"detail"`
	Needs      json.RawMessage `json:"needs"`
	Suggested  json.RawMessage `json:"suggestedReply"`
	Intent     string          `json:"intent"`
	Tokens     int             `json:"tokens"`
	UpdatedAt  string          `json:"updatedAt"`
	Children   []Link          `json:"children"`
	ForkParent string          `json:"forkParentSessionId"`
	Branch     string          `json:"worktreeBranch"`
	Transcript string          `json:"linkScanPath"`
}

func Home() string { h, _ := os.UserHomeDir(); return h }

func Dir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(Home(), ".claude")
}

func cacheFile() string {
	d, _ := os.UserCacheDir()
	return filepath.Join(d, "regia", "agents.json")
}

// FetchAgents runs the supported CLI listing. Takes ~0.2s, so it runs async.
func FetchAgents() ([]byte, error) {
	out, err := exec.Command("claude", "agents", "--json", "--all").Output()
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Dir(cacheFile()), 0o755)
	_ = os.WriteFile(cacheFile(), out, 0o644) // its mtime also tells `regia context` how fresh it is
	return out, nil
}

// ListingStamp changes whenever what `claude agents` reports may have: a
// job started or went away, a job's own state moved, a session's status file
// was written. The listing is a whole process (~0.1 s of CPU, 150 MB), so
// regia runs it when the stamp moves rather than on a timer alone.
func ListingStamp(agents map[string]*Agent) uint64 {
	var sum uint64
	add := func(parts ...string) {
		h := fnv.New64a()
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
		sum += h.Sum64() // a sum: map order doesn't matter
	}
	for _, d := range []string{"jobs", "sessions"} {
		if fi, err := os.Stat(filepath.Join(Dir(), d)); err == nil {
			add(d, fi.ModTime().String())
		}
	}
	if es, err := os.ReadDir(filepath.Join(Dir(), "sessions")); err == nil {
		for _, e := range es {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if fi, err := e.Info(); err == nil {
				add(e.Name(), fi.ModTime().String())
			}
		}
	}
	for id, a := range agents {
		add(id, a.JobState)
	}
	return sum
}

// CachedAge is how old the last listing is (a long time when there is none).
func CachedAge() time.Duration {
	fi, err := os.Stat(cacheFile())
	if err != nil {
		return 1 << 62
	}
	return time.Since(fi.ModTime())
}

// CachedAgents returns the last listing so the first frame is instant.
func CachedAgents() []byte {
	b, _ := os.ReadFile(cacheFile())
	return b
}

func ParseAgents(b []byte) []*Agent {
	var raw []rawAgent
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	out := make([]*Agent, 0, len(raw))
	for _, r := range raw {
		a := &Agent{
			ID: r.ID, SessionID: r.SessionID, Name: r.Name, Cwd: r.Cwd, Kind: r.Kind,
			StartedAt: time.UnixMilli(r.StartedAt),
		}
		st := r.State
		if st == "" {
			st = r.Status
		}
		a.State = normState(st)
		if r.Kind == "interactive" || a.ID == "" {
			a.Interactive = true
			a.ID = "s:" + short(r.SessionID)
		}
		if a.Name == "" {
			a.Name = a.ID
		}
		out = append(out, a)
	}
	return out
}

func normState(s string) string {
	switch s {
	case "working", "busy", "active":
		return "busy"
	case "blocked", "needs input", "needs_input", "waiting":
		return "blocked"
	case "done", "completed":
		return "done"
	case "failed", "error":
		return "failed"
	case "":
		return "idle"
	}
	return s
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

type jobEntry struct {
	size int64
	mod  time.Time
	job  *rawJob // nil: not valid JSON
}

var (
	jcMu  sync.Mutex
	jcMap = map[string]jobEntry{}
)

// readJob reads a job state file, parsed again only when it changed.
func readJob(path string) *rawJob {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	jcMu.Lock()
	e, ok := jcMap[path]
	jcMu.Unlock()
	if ok && e.size == fi.Size() && e.mod.Equal(fi.ModTime()) {
		return e.job
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	e = jobEntry{size: fi.Size(), mod: fi.ModTime()}
	if j := new(rawJob); json.Unmarshal(b, j) == nil {
		e.job = j
	}
	jcMu.Lock()
	jcMap[path] = e
	jcMu.Unlock()
	return e.job
}

// Enrich merges the job state file. Done every tick: it's parsed only when
// it changed.
func Enrich(a *Agent) {
	if a.Interactive {
		return
	}
	j := readJob(filepath.Join(Dir(), "jobs", a.ID, "state.json"))
	if j == nil {
		return
	}
	a.Detail = j.Detail
	a.Needs = rawString(j.Needs)
	a.Suggested = rawString(j.Suggested)
	a.Intent = j.Intent
	a.Tokens = j.Tokens
	a.Branch = j.Branch
	a.PRs = j.Children
	a.ForkParent = short(j.ForkParent)
	a.Transcript = j.Transcript
	a.JobState = j.State
	if t, err := time.Parse(time.RFC3339Nano, j.UpdatedAt); err == nil {
		a.UpdatedAt = t
	}
	// The listing is the source of truth for liveness; the job file is fresher
	// for the semantic state. Prefer "blocked"/"failed" from the job file.
	if s := normState(j.State); s == "blocked" || s == "failed" {
		if a.State != "busy" {
			a.State = s
		}
	}
	if a.State != "blocked" {
		a.Needs, a.Suggested = "", ""
	}
}

// rawString accepts a JSON string, null, or anything else (rendered compactly).
func rawString(r json.RawMessage) string {
	if len(r) == 0 || string(r) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	return string(r)
}

// Project derives a grouping key from a cwd: ~/Projects/<x>/... -> x.
func Project(cwd string) string {
	if i := strings.Index(cwd, "/.claude/worktrees/"); i >= 0 {
		cwd = cwd[:i]
	}
	h := Home()
	for _, root := range []string{filepath.Join(h, "Projects"), filepath.Join(h, "Git")} {
		if rel, err := filepath.Rel(root, cwd); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			return strings.Split(rel, string(filepath.Separator))[0]
		}
	}
	if cwd == h || cwd == "" {
		return "~"
	}
	return filepath.Base(cwd)
}

func stateRank(s string) int {
	switch s {
	case "blocked":
		return 0
	case "ready":
		return 1
	case "failed":
		return 1
	case "busy":
		return 2
	case "idle":
		return 3
	}
	return 4
}

func SortByStart(as []*Agent) {
	sort.SliceStable(as, func(i, j int) bool { return as[i].StartedAt.Before(as[j].StartedAt) })
}
