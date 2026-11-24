// Package plugins runs your own scripts against each agent and keeps what
// they say, so the UI can show it: extra lines on the card, a badge on the
// row, a reason to put the agent in the inbox, and actions you trigger from
// the panel.
//
// A plugin is a directory under ~/.config/regia/plugins/<name>/ holding a
// plugin.json manifest and whatever it runs. regia runs the command with the
// agent's facts as JSON on stdin and reads JSON from stdout. The protocol is
// documented for plugin authors in docs/plugins.md; keep the two in step.
//
// Runs happen in the background (call the func a Refresher returns from a
// tea.Cmd), are cached per plugin and agent for the manifest's "every", and
// back off when a plugin keeps failing.
package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// ProtocolVersion is sent as "version" in every input. Bump it only on a
// change that breaks existing plugins.
const ProtocolVersion = 1

const (
	defaultEvery   = 2 * time.Minute
	defaultTimeout = 10 * time.Second
	minEvery       = 10 * time.Second
	maxBackoff     = 30 * time.Minute
	maxStdout      = 256 << 10
	maxFields      = 8
	maxActions     = 9
)

// Manifest is a plugin's plugin.json.
type Manifest struct {
	Name string `json:"-"` // the directory's name
	Dir  string `json:"-"` // the directory

	Command     []string `json:"command"`     // what to run; the program and arguments naming files in Dir are resolved against Dir (see resolve, resolveArg)
	Label       string   `json:"label"`       // default label for fields; the name when empty
	Dirs        []string `json:"dirs"`        // only agents whose cwd is under one of these (globs allowed); empty: every agent
	States      []string `json:"states"`      // only agents in these states; empty: every state
	Interactive bool     `json:"interactive"` // also run for interactive sessions
	Every       Duration `json:"every"`       // how long a result is kept; default 2m, at least 10s
	Timeout     Duration `json:"timeout"`     // how long one run may take; default 10s
	Disabled    bool     `json:"disabled"`
	Gates       []string `json:"gates"` // what the plugin is asked before it happens, and may refuse: "remove" (see Gate)

	raw []byte
}

// Duration is a time.Duration written as "90s", "2m" in JSON.
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"2m\"")
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (m *Manifest) every() time.Duration {
	if m.Every <= 0 {
		return defaultEvery
	}
	return max(minEvery, time.Duration(m.Every))
}

func (m *Manifest) timeout() time.Duration {
	if m.Timeout <= 0 {
		return defaultTimeout
	}
	return time.Duration(m.Timeout)
}

// LabelOr is the label for a field that has none.
func (m *Manifest) LabelOr() string {
	if m.Label != "" {
		return m.Label
	}
	return m.Name
}

// ---- input

// Input is what a plugin reads on stdin.
type Input struct {
	Version int        `json:"version"`
	Event   string     `json:"event"`            // "refresh", "action", "remove" or "swap"
	Action  string     `json:"action,omitempty"` // the action's id, for "action"
	By      string     `json:"by,omitempty"`     // "remove", "swap": who asked, "you" or the agent's name
	Reason  string     `json:"reason,omitempty"` // "remove", "swap": the reason given
	To      *AgentInfo `json:"to,omitempty"`     // "swap": the session taking the agent's place
	Note    string     `json:"note,omitempty"`   // "swap": the link the caller gave, e.g. the handoff file
	Plugin  PluginInfo `json:"plugin"`
	Agent   AgentInfo  `json:"agent"`
	Git     *GitInfo   `json:"git"` // null when the directory isn't a git checkout (or not known yet)
	PR      *PRInfo    `json:"pr"`  // null when regia knows no PR for the agent
	Last    *Output    `json:"last"`
}

type PluginInfo struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type AgentInfo struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id"`
	Name        string    `json:"name"`
	Title       string    `json:"title"` // what regia shows: your rename, else the name
	Cwd         string    `json:"cwd"`
	Kind        string    `json:"kind"`
	State       string    `json:"state"`
	Interactive bool      `json:"interactive"`
	Branch      string    `json:"branch"`
	Intent      string    `json:"intent"` // the task it was started with
	Detail      string    `json:"detail"`
	Needs       string    `json:"needs"`
	Links       []Link    `json:"links"`
	Transcript  string    `json:"transcript"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Group       []string  `json:"group"` // the groups and coordinators it sits under, outermost first
}

type Link struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	URL  string `json:"url"`
}

type GitInfo struct {
	Branch   string `json:"branch"`
	Upstream string `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Changed  int    `json:"changed"`
	Head     string `json:"head"`
	Subject  string `json:"subject"`
	Worktree bool   `json:"worktree"`
}

type PRInfo struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	State  string `json:"state"`  // draft, open, merged, closed
	Checks string `json:"checks"` // passing, failing, pending, ""
	Review string `json:"review"` // approved, changes, requested, ""
}

// ---- output

// Output is what a plugin prints on stdout. Every key is optional.
type Output struct {
	Fields    []Field  `json:"fields,omitempty"`
	Badge     *Badge   `json:"badge,omitempty"`
	Attention string   `json:"attention,omitempty"`
	Actions   []Action `json:"actions,omitempty"`
	Every     string   `json:"every,omitempty"` // overrides the manifest's for this result; "never" keeps it until regia restarts

	// only read after an action
	Message string `json:"message,omitempty"` // shown in the status line
	Copy    string `json:"copy,omitempty"`    // put on the clipboard
	Open    string `json:"open,omitempty"`    // URL to open in the browser
	Remove  bool   `json:"remove,omitempty"`  // remove the agent (claude rm) the way x does

	// only read for "remove"
	Refuse string `json:"refuse,omitempty"` // why the agent may not be removed yet
}

type Field struct {
	Label  string `json:"label,omitempty"`
	Text   string `json:"text"`
	Status string `json:"status,omitempty"` // ok, warn, error, dim, or "" (plain)
	URL    string `json:"url,omitempty"`
}

// Badge is one or two characters on the agent's row. Accepts "J" or {"text":"J","status":"warn"}.
type Badge struct {
	Text   string `json:"text"`
	Status string `json:"status,omitempty"`
}

func (b *Badge) UnmarshalJSON(d []byte) error {
	var s string
	if json.Unmarshal(d, &s) == nil {
		b.Text = s
		return nil
	}
	type plain Badge
	var p plain
	if err := json.Unmarshal(d, &p); err != nil {
		return err
	}
	*b = Badge(p)
	return nil
}

type Action struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Confirm bool   `json:"confirm,omitempty"` // ask before running
}

// Statuses are the values Field.Status and Badge.Status may take.
var Statuses = []string{"", "ok", "warn", "error", "dim"}

// Parse reads a plugin's stdout. Empty output is a valid empty result.
func Parse(b []byte) (Output, error) {
	var o Output
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&o); err != nil {
		return Output{}, fmt.Errorf("stdout is not a JSON object: %v", err)
	}
	var warn []string
	if len(o.Fields) > maxFields {
		o.Fields = o.Fields[:maxFields]
		warn = append(warn, fmt.Sprintf("more than %d fields: the rest dropped", maxFields))
	}
	if len(o.Actions) > maxActions {
		o.Actions = o.Actions[:maxActions]
		warn = append(warn, fmt.Sprintf("more than %d actions: the rest dropped", maxActions))
	}
	acts := o.Actions[:0]
	for _, a := range o.Actions {
		if a.ID == "" {
			warn = append(warn, "an action without id was dropped")
			continue
		}
		if a.Label == "" {
			a.Label = a.ID
		}
		acts = append(acts, a)
	}
	o.Actions = acts
	for _, f := range o.Fields {
		if !validStatus(f.Status) {
			warn = append(warn, fmt.Sprintf("unknown status %q (ok, warn, error, dim)", f.Status))
		}
	}
	if o.Badge != nil {
		if o.Badge.Text == "" {
			o.Badge = nil
		} else {
			if r := []rune(o.Badge.Text); len(r) > 2 {
				o.Badge.Text = string(r[:2])
				warn = append(warn, "badge cut to 2 characters")
			}
			if !validStatus(o.Badge.Status) {
				warn = append(warn, fmt.Sprintf("unknown badge status %q", o.Badge.Status))
			}
		}
	}
	if o.Every != "" && o.Every != "never" {
		if _, err := time.ParseDuration(o.Every); err != nil {
			warn = append(warn, fmt.Sprintf("every %q: %v", o.Every, err))
		}
	}
	if len(warn) > 0 {
		return o, &Warning{warn}
	}
	return o, nil
}

// Warning: the output was used, but parts of it were wrong.
type Warning struct{ Problems []string }

func (w *Warning) Error() string { return strings.Join(w.Problems, "; ") }

func validStatus(s string) bool {
	for _, v := range Statuses {
		if s == v {
			return true
		}
	}
	return false
}

// ---- loading

// Dir is where plugins live: ~/.config/regia/plugins (REGIA_PLUGINS_DIR overrides).
func Dir() string {
	if d := os.Getenv("REGIA_PLUGINS_DIR"); d != "" {
		return d
	}
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "regia", "plugins")
}

// LoadError is a plugin directory whose manifest couldn't be used.
type LoadError struct {
	Name string
	Err  error
}

// Load reads every plugin in dir, sorted by name. Disabled plugins are left
// out; broken ones are reported in errs.
func Load(dir string) (ps []*Manifest, errs []LoadError) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p, err := LoadOne(filepath.Join(dir, e.Name()))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, LoadError{e.Name(), err})
			}
			continue
		}
		if !p.Disabled {
			ps = append(ps, p)
		}
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })
	return ps, errs
}

// LoadOne reads the plugin in dir.
func LoadOne(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("plugin.json: %v", err)
	}
	if len(m.Command) == 0 || m.Command[0] == "" {
		return nil, fmt.Errorf("plugin.json: \"command\" is required, e.g. [\"./check.sh\"]")
	}
	for _, s := range m.States {
		switch s {
		case "blocked", "busy", "idle", "done", "failed", "ready", Removed:
		default:
			return nil, fmt.Errorf("plugin.json: unknown state %q (blocked busy idle done failed ready removed)", s)
		}
	}
	for _, g := range m.Gates {
		if g != GateRemove && g != GateSwap {
			return nil, fmt.Errorf("plugin.json: unknown gate %q (remove swap)", g)
		}
	}
	m.Name = filepath.Base(dir)
	m.Dir, _ = filepath.Abs(dir)
	m.raw = b
	return &m, nil
}

// Removed is the state of a session no longer listed, found in the whole
// history (ctrl+g in search). Only plugins that name it in "states" run for
// those, so a plugin can still say something about it (where its handoff is).
const Removed = "removed"

// Applies reports whether the plugin runs for an agent with these facts.
func (m *Manifest) Applies(a AgentInfo) bool {
	if a.Interactive && !m.Interactive {
		return false
	}
	if a.State == Removed && !slices.Contains(m.States, Removed) {
		return false
	}
	if len(m.States) > 0 {
		ok := false
		for _, s := range m.States {
			// "ready" is regia's name for done-and-not-seen: it matches done too
			if s == a.State || (s == "done" && a.State == "ready") || (s == "ready" && a.State == "done") {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return m.covers(a.Cwd)
}

// covers says whether the manifest's "dirs" take in an agent working in cwd.
func (m *Manifest) covers(cwd string) bool {
	if len(m.Dirs) == 0 {
		return true
	}
	if cwd == "" {
		return false
	}
	for _, d := range m.Dirs {
		if underDir(cwd, expandHome(d)) {
			return true
		}
	}
	return false
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[1:])
	}
	return p
}

// underDir: cwd is pattern or inside it. pattern may hold glob characters,
// matched against cwd and each of its parents; a trailing "/**" means "or
// anything inside", which is already what a directory means.
func underDir(cwd, pattern string) bool {
	pattern = strings.TrimSuffix(strings.TrimSuffix(pattern, "/**"), "/")
	cwd = filepath.Clean(cwd)
	for p := cwd; ; p = filepath.Dir(p) {
		if ok, _ := filepath.Match(pattern, p); ok || p == pattern {
			return true
		}
		if q := filepath.Dir(p); q == p {
			return false
		}
	}
}

// ---- running

// Run runs the plugin once with in on stdin, blocking. dir is where it runs
// (the agent's directory, or the plugin's when that's gone). stderr is
// returned for diagnostics.
func Run(ctx context.Context, m *Manifest, in Input, dir string) (out Output, stderr string, err error) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout())
	defer cancel()
	b, _ := json.Marshal(in)
	argv := []string{m.resolve(m.Command[0])}
	for _, a := range m.Command[1:] {
		argv = append(argv, m.resolveArg(a))
	}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if dir == "" {
		dir = m.Dir
	} else if _, e := os.Stat(dir); e != nil {
		dir = m.Dir
	}
	c.Dir = dir
	c.Stdin = bytes.NewReader(b)
	c.Env = append(os.Environ(),
		"REGIA_PLUGIN="+m.Name,
		"REGIA_PLUGIN_DIR="+m.Dir,
		"REGIA_EVENT="+in.Event,
		"REGIA_ACTION="+in.Action,
		"REGIA_AGENT_ID="+in.Agent.ID,
		"REGIA_AGENT_CWD="+in.Agent.Cwd,
	)
	var so, se limitedBuf
	so.max, se.max = maxStdout, 16<<10
	c.Stdout, c.Stderr = &so, &se
	c.WaitDelay = time.Second
	runErr := c.Run()
	stderr = strings.TrimSpace(se.String())
	if ctx.Err() == context.DeadlineExceeded {
		return Output{}, stderr, fmt.Errorf("timed out after %s", m.timeout())
	}
	if runErr != nil {
		msg := runErr.Error()
		if l := lastLine(stderr); l != "" {
			msg += ": " + l
		}
		return Output{}, stderr, errors.New(msg)
	}
	if so.over {
		return Output{}, stderr, fmt.Errorf("stdout over %d KB", maxStdout>>10)
	}
	out, err = Parse(so.Bytes())
	return out, stderr, err
}

func (m *Manifest) resolve(cmd string) string {
	if filepath.IsAbs(cmd) {
		return cmd
	}
	if strings.ContainsRune(cmd, '/') {
		return filepath.Join(m.Dir, cmd)
	}
	if p := filepath.Join(m.Dir, cmd); isFile(p) {
		return p
	}
	return cmd // on PATH
}

// resolveArg: an argument naming a file in the plugin directory ("check.py",
// "lib/x.py", "./conf.json") becomes its absolute path, since the command
// runs in the agent's directory; anything else is passed as written.
func (m *Manifest) resolveArg(arg string) string {
	if arg == "" || filepath.IsAbs(arg) || strings.HasPrefix(arg, "-") {
		return arg
	}
	if p := filepath.Join(m.Dir, arg); isFile(p) {
		return p
	}
	return arg
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}

type limitedBuf struct {
	bytes.Buffer
	max  int
	over bool
}

func (b *limitedBuf) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); len(p) > room {
		b.over = true
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
