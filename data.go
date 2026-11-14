package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
	State       string // normalized: blocked busy idle done failed
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
}

type Link struct {
	ID   string `json:"id"`
	Href string `json:"href"`
	Kind string `json:"kind"`
}

type TLEntry struct {
	At     time.Time
	State  string
	Detail string
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
}

func home() string { h, _ := os.UserHomeDir(); return h }

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".claude")
}

func cacheFile() string {
	d, _ := os.UserCacheDir()
	return filepath.Join(d, "regia", "agents.json")
}

// fetchAgents runs the supported CLI listing. Takes ~0.2s, so it runs async.
func fetchAgents() ([]byte, error) {
	out, err := exec.Command("claude", "agents", "--json", "--all").Output()
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Dir(cacheFile()), 0o755)
	_ = os.WriteFile(cacheFile(), out, 0o644)
	return out, nil
}

// cachedAgents returns the last listing so the first frame is instant.
func cachedAgents() []byte {
	b, _ := os.ReadFile(cacheFile())
	return b
}

func parseAgents(b []byte) []*Agent {
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

// enrich merges the job state file. Cheap (small JSON), done every tick.
func enrich(a *Agent) {
	if a.Interactive {
		return
	}
	b, err := os.ReadFile(filepath.Join(claudeDir(), "jobs", a.ID, "state.json"))
	if err != nil {
		return
	}
	var j rawJob
	if json.Unmarshal(b, &j) != nil {
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

func readTimeline(id string, max int) []TLEntry {
	f, err := os.Open(filepath.Join(claudeDir(), "jobs", id, "timeline.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []TLEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<22)
	for sc.Scan() {
		var e struct{ At, State, Detail string }
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339Nano, e.At)
		all = append(all, TLEntry{At: t, State: e.State, Detail: e.Detail})
	}
	if len(all) > max {
		all = all[len(all)-max:]
	}
	return all
}

// project derives a grouping key from a cwd: ~/Projects/<x>/... -> x.
func project(cwd string) string {
	if i := strings.Index(cwd, "/.claude/worktrees/"); i >= 0 {
		cwd = cwd[:i]
	}
	h := home()
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
	case "failed":
		return 1
	case "busy":
		return 2
	case "idle":
		return 3
	}
	return 4
}

func sortByStart(as []*Agent) {
	sort.SliceStable(as, func(i, j int) bool { return as[i].StartedAt.Before(as[j].StartedAt) })
}

func copyClipboard(s string) error {
	for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"pbcopy"}} {
		if _, err := exec.LookPath(c[0]); err == nil {
			cmd := exec.Command(c[0], c[1:]...)
			cmd.Stdin = bytes.NewBufferString(s)
			return cmd.Run()
		}
	}
	return os.ErrNotExist
}
