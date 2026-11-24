package plugins

import (
	"bytes"
	"context"
	"sync"
	"time"
)

// Result is the last run of one plugin for one agent.
type Result struct {
	Plugin  *Manifest
	Out     Output
	Err     string // the run failed (or its output was partly wrong): why, one line
	Fetched time.Time
	fails   int
}

// OK: the run worked (Err may still hold a warning about the output).
func (r Result) OK() bool { return !r.Fetched.IsZero() && r.fails == 0 }

// registry holds the loaded plugins and their results. One per process.
var reg = struct {
	sync.Mutex
	plugins  []*Manifest
	loadErrs []LoadError
	loadedAt time.Time
	cache    map[string]Result // plugin name \x00 agent id
	inFlight map[string]bool
	starts   []StartReq // refreshes' starts, for the UI (TakeStarts)
	version  int        // bumped on every stored result (Version)
}{cache: map[string]Result{}, inFlight: map[string]bool{}}

const reloadEvery = 5 * time.Second

func key(plugin, agent string) string { return plugin + "\x00" + agent }

// Plugins returns the loaded plugins, reading the plugins directory again
// when it was last read more than a few seconds ago: a plugin you add or edit
// shows up without restarting regia. A manifest that changed drops its
// cached results.
func Plugins() []*Manifest {
	reg.Lock()
	defer reg.Unlock()
	if time.Since(reg.loadedAt) < reloadEvery {
		return reg.plugins
	}
	reg.loadedAt = time.Now()
	ps, errs := Load(Dir())
	old := map[string]*Manifest{}
	for _, p := range reg.plugins {
		old[p.Name] = p
	}
	for i, p := range ps {
		if o := old[p.Name]; o != nil && bytes.Equal(o.raw, p.raw) {
			ps[i] = o // same manifest: keep the pointer, keep results
			delete(old, p.Name)
		}
	}
	for name := range old { // removed or changed
		for k := range reg.cache {
			if len(k) > len(name) && k[:len(name)+1] == name+"\x00" {
				delete(reg.cache, k)
			}
		}
	}
	reg.plugins, reg.loadErrs = ps, errs
	return ps
}

// LoadErrors: plugin directories whose manifest is broken, as last loaded.
func LoadErrors() []LoadError {
	reg.Lock()
	defer reg.Unlock()
	return reg.loadErrs
}

// Version changes whenever a result is stored: what draws results can be
// cached until it changes.
func Version() int {
	reg.Lock()
	defer reg.Unlock()
	return reg.version
}

// Results returns the cached results for an agent, in plugin order. Never blocks on a plugin.
func Results(agentID string) []Result {
	ps := Plugins()
	reg.Lock()
	defer reg.Unlock()
	var out []Result
	for _, p := range ps {
		if r, ok := reg.cache[key(p.Name, agentID)]; ok && r.Plugin == p {
			out = append(out, r)
		}
	}
	return out
}

// due: a result fetched at r.Fetched should be asked again.
func due(r Result, now time.Time) bool {
	if r.Fetched.IsZero() {
		return true
	}
	every := r.Plugin.every()
	if r.fails > 0 {
		every = min(maxBackoff, every<<min(r.fails-1, 8))
	} else if r.Out.Every == "never" {
		return false
	} else if d, err := time.ParseDuration(r.Out.Every); err == nil && d > 0 {
		every = max(minEvery, d)
	}
	return now.Sub(r.Fetched) >= every
}

// Refresher returns the work to refresh p for the agent when it's due (nil
// otherwise), marking it in flight. build makes the input; it is called only
// when a run is due. The returned func blocks: run it in a tea.Cmd.
func Refresher(p *Manifest, agentID string, build func(last *Output) (Input, string)) func() {
	k := key(p.Name, agentID)
	reg.Lock()
	r, ok := reg.cache[k]
	if reg.inFlight[k] || (ok && r.Plugin == p && !due(r, time.Now())) {
		reg.Unlock()
		return nil
	}
	reg.inFlight[k] = true
	var last *Output
	if ok && r.OK() {
		o := r.Out
		last = &o
	}
	reg.Unlock()
	in, dir := build(last)
	in.Event = "refresh"
	return func() {
		defer func() { reg.Lock(); delete(reg.inFlight, k); reg.Unlock() }()
		out, _, err := Run(context.Background(), p, in, dir)
		if w := queueStart(p, agentID, &out); w != "" && err == nil {
			err = &Warning{[]string{w}}
		}
		store(p, agentID, out, err, false)
	}
}

// store records a run's outcome. An action's output replaces the cached
// result only when it carries something to show.
func store(p *Manifest, agentID string, out Output, err error, action bool) {
	k := key(p.Name, agentID)
	reg.Lock()
	defer reg.Unlock()
	reg.version++
	prev := reg.cache[k]
	if prev.Plugin != p {
		prev = Result{}
	}
	r := Result{Plugin: p, Fetched: time.Now()}
	_, warning := err.(*Warning)
	switch {
	case err != nil && !warning:
		r.Out, r.Err, r.fails = prev.Out, err.Error(), prev.fails+1
	case action && len(out.Fields) == 0 && out.Badge == nil && out.Attention == "" && len(out.Actions) == 0:
		prev.Fetched = time.Time{} // nothing to show: refresh soon instead
		reg.cache[k] = prev
		return
	default:
		r.Out = out
		if warning {
			r.Err = err.Error()
		}
	}
	reg.cache[k] = r
}

// RunAction runs an action (blocking) and returns the plugin's output. Its
// fields, if any, become the cached result; otherwise the plugin is asked
// for a refresh on the next tick.
func RunAction(p *Manifest, agentID, action string, in Input, dir string) (Output, error) {
	in.Event, in.Action = "action", action
	out, _, err := Run(context.Background(), p, in, dir)
	store(p, agentID, out, err, true)
	if _, warning := err.(*Warning); warning {
		err = nil
	}
	return out, err
}

// Seed stores out as p's fresh result for the agent. For tests.
func Seed(p *Manifest, agentID string, out Output) {
	reg.Lock()
	defer reg.Unlock()
	reg.version++
	reg.cache[key(p.Name, agentID)] = Result{Plugin: p, Out: out, Fetched: time.Now()}
}

// SetForTest replaces the loaded plugins (and clears results) until the next
// reload; it also stops reloading for an hour.
func SetForTest(ps []*Manifest) {
	reg.Lock()
	defer reg.Unlock()
	reg.plugins, reg.loadErrs = ps, nil
	reg.loadedAt = time.Now().Add(time.Hour)
	reg.cache = map[string]Result{}
	reg.starts = nil
}
