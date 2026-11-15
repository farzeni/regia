package gitinfo

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Commit is one commit from a branch's log.
type Commit struct {
	Hash    string
	Subject string
	At      time.Time
}

// LogKey names one log: a directory, the ref to read (a branch, or "HEAD")
// and how far back.
type LogKey struct {
	Dir, Ref string
	Since    time.Time
}

const (
	logTTL = 30 * time.Second
	logMax = 100
)

type logEntry struct {
	commits []Commit
	at      time.Time
}

var (
	logMu       sync.Mutex
	logCache    = map[LogKey]logEntry{}
	logInFlight = map[LogKey]bool{}
)

// CachedLog returns the last log read for k, if any.
func CachedLog(k LogKey) ([]Commit, bool) {
	logMu.Lock()
	defer logMu.Unlock()
	e, ok := logCache[k]
	return e.commits, ok
}

// LogRefresher returns a function that reads k's log into the cache, or nil
// when the cached one is fresh or a read is already running. The caller runs
// it in the background.
func LogRefresher(k LogKey) func() {
	if k.Dir == "" {
		return nil
	}
	logMu.Lock()
	e, ok := logCache[k]
	if (ok && time.Since(e.at) < logTTL) || logInFlight[k] {
		logMu.Unlock()
		return nil
	}
	logInFlight[k] = true
	logMu.Unlock()
	return func() {
		c := ReadLog(k)
		logMu.Lock()
		logCache[k] = logEntry{c, time.Now()}
		delete(logInFlight, k)
		logMu.Unlock()
	}
}

// ReadLog lists the commits on k.Ref since k.Since, newest first (at most
// logMax). A directory that is gone or isn't a repository gives nothing.
func ReadLog(k LogKey) []Commit {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ref := k.Ref
	if ref == "" {
		ref = "HEAD"
	}
	args := []string{"log", "--no-merges", "-n", strconv.Itoa(logMax), "--format=%h%x00%s%x00%ct"}
	if !k.Since.IsZero() {
		args = append(args, "--since="+strconv.FormatInt(k.Since.Unix(), 10))
	}
	out, err := git(ctx, k.Dir, append(args, ref, "--")...)
	if err != nil {
		return nil
	}
	return parseLog(out)
}

func parseLog(out string) []Commit {
	var cs []Commit
	for _, l := range strings.Split(out, "\n") {
		p := strings.SplitN(l, "\x00", 3)
		if len(p) != 3 {
			continue
		}
		ts, err := strconv.ParseInt(strings.TrimSpace(p[2]), 10, 64)
		if err != nil {
			continue
		}
		cs = append(cs, Commit{Hash: p[0], Subject: p[1], At: time.Unix(ts, 0)})
	}
	return cs
}
