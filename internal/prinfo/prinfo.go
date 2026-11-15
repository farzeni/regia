// Package prinfo reads the status of an agent's pull request with `gh`
// (number, state, CI checks, review decision), cached per PR. Open PRs are
// refreshed at most every 60 s; merged and closed ones are never asked again.
// When gh is missing or not authenticated, it quietly returns nothing.
package prinfo

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Info is what the card shows about a PR.
type Info struct {
	OK      bool
	Number  int
	URL     string
	State   string // "draft", "open", "merged", "closed"
	Checks  string // "passing", "failing", "pending", "" (none)
	Review  string // "approved", "changes", "requested", ""
	Title   string
	Fetched time.Time
}

// Final reports a PR that can no longer change (merged or closed).
func (i Info) Final() bool { return i.State == "merged" || i.State == "closed" }

// NeedsLook reports an open PR whose CI fails or that has changes requested.
func (i Info) NeedsLook() bool {
	return i.OK && !i.Final() && (i.Checks == "failing" || i.Review == "changes")
}

// Reason is the inbox line for NeedsLook ("" when there is nothing to look at).
func (i Info) Reason() string {
	var r []string
	if i.Checks == "failing" {
		r = append(r, "CI failing")
	}
	if i.Review == "changes" {
		r = append(r, "changes requested")
	}
	if len(r) == 0 || !i.NeedsLook() {
		return ""
	}
	return "PR #" + strconv.Itoa(i.Number) + ": " + strings.Join(r, ", ")
}

const (
	openTTL = 60 * time.Second
	noneTTL = 10 * time.Minute // no PR for this branch (yet): asked again rarely
)

// due reports whether cached info i, fetched at i.Fetched, should be asked again.
func due(i Info) bool {
	switch {
	case i.Final():
		return false
	case !i.OK:
		return time.Since(i.Fetched) > noneTTL
	}
	return time.Since(i.Fetched) > openTTL
}

var (
	mu       sync.Mutex
	cache    = map[string]Info{}
	inFlight = map[string]bool{}
	ghBroken bool // gh missing or unauthenticated: stop asking
	lookPath = exec.LookPath
	run      = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, "gh", args...)
		c.Dir = dir
		return c.Output()
	}
)

func key(dir, ref string) string { return dir + "\x00" + ref }

// Cached returns the last known info for ref (a PR URL, number or branch) in
// dir, and whether a Fetch is due. Never blocks.
func Cached(dir, ref string) (Info, bool) {
	mu.Lock()
	defer mu.Unlock()
	k := key(dir, ref)
	i, ok := cache[k]
	if ghBroken || inFlight[k] {
		return i, false
	}
	if !ok {
		return i, true
	}
	return i, due(i)
}

// Refresher returns the work to refresh ref in dir when it's due (nil when
// it isn't, gh is unusable, or a fetch is already running), marking it in
// flight right away so callers on every frame don't start it twice. The
// returned func blocks on gh: run it in a tea.Cmd.
func Refresher(dir, ref string) func() {
	if dir == "" || ref == "" {
		return nil
	}
	k := key(dir, ref)
	mu.Lock()
	defer mu.Unlock()
	i, ok := cache[k]
	if ghBroken || inFlight[k] || (ok && !due(i)) {
		return nil
	}
	inFlight[k] = true
	return func() { fetch(dir, ref) }
}

// Seed stores i as the fresh status of ref in dir, as if gh had just
// answered. For tests of the packages that show it.
func Seed(dir, ref string, i Info) {
	i.Fetched = time.Now()
	mu.Lock()
	cache[key(dir, ref)] = i
	mu.Unlock()
}

// Fetch runs gh (blocking: call it from a tea.Cmd) and caches the result.
func Fetch(dir, ref string) Info {
	k := key(dir, ref)
	mu.Lock()
	if ghBroken || inFlight[k] {
		i := cache[k]
		mu.Unlock()
		return i
	}
	inFlight[k] = true
	mu.Unlock()
	return fetch(dir, ref)
}

// fetch does the work; the caller has marked ref in flight.
func fetch(dir, ref string) Info {
	k := key(dir, ref)
	defer func() { mu.Lock(); delete(inFlight, k); mu.Unlock() }()

	if _, err := lookPath("gh"); err != nil {
		mu.Lock()
		ghBroken = true
		mu.Unlock()
		return Info{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := run(ctx, dir, "pr", "view", ref, "--json",
		"number,url,state,isDraft,title,reviewDecision,statusCheckRollup")
	info := Info{}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && authError(string(ee.Stderr)) {
			mu.Lock()
			ghBroken = true
			mu.Unlock()
		}
		// no PR for this ref (yet): retried after noneTTL
	} else {
		info = Parse(out)
	}
	info.Fetched = time.Now()
	mu.Lock()
	cache[k] = info
	mu.Unlock()
	return info
}

func authError(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "gh auth login") || strings.Contains(s, "not logged") ||
		strings.Contains(s, "authentication")
}

type rawPR struct {
	Number         int    `json:"number"`
	URL            string `json:"url"`
	State          string `json:"state"`
	IsDraft        bool   `json:"isDraft"`
	Title          string `json:"title"`
	ReviewDecision string `json:"reviewDecision"`
	Checks         []struct {
		Status     string `json:"status"`     // CheckRun: QUEUED, IN_PROGRESS, COMPLETED
		Conclusion string `json:"conclusion"` // CheckRun: SUCCESS, FAILURE, …
		State      string `json:"state"`      // StatusContext: SUCCESS, FAILURE, PENDING, ERROR
	} `json:"statusCheckRollup"`
}

// Parse turns `gh pr view --json` output into Info; every field is optional.
func Parse(b []byte) Info {
	var r rawPR
	if json.Unmarshal(b, &r) != nil || r.Number == 0 {
		return Info{}
	}
	i := Info{OK: true, Number: r.Number, URL: r.URL, Title: r.Title}
	switch strings.ToUpper(r.State) {
	case "MERGED":
		i.State = "merged"
	case "CLOSED":
		i.State = "closed"
	default:
		i.State = "open"
		if r.IsDraft {
			i.State = "draft"
		}
	}
	switch strings.ToUpper(r.ReviewDecision) {
	case "APPROVED":
		i.Review = "approved"
	case "CHANGES_REQUESTED":
		i.Review = "changes"
	case "REVIEW_REQUIRED":
		i.Review = "requested"
	}
	fail, pend, pass := false, false, false
	for _, c := range r.Checks {
		v := strings.ToUpper(c.Conclusion)
		if v == "" {
			v = strings.ToUpper(c.State)
		}
		st := strings.ToUpper(c.Status)
		switch {
		case v == "FAILURE" || v == "ERROR" || v == "TIMED_OUT" || v == "CANCELLED" ||
			v == "ACTION_REQUIRED" || v == "STARTUP_FAILURE":
			fail = true
		case (st != "" && st != "COMPLETED") || v == "PENDING" || v == "EXPECTED" || v == "":
			pend = true
		default:
			pass = true
		}
	}
	switch {
	case fail:
		i.Checks = "failing"
	case pend:
		i.Checks = "pending"
	case pass:
		i.Checks = "passing"
	}
	return i
}
