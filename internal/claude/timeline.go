package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Event is one state change of a job, from ~/.claude/jobs/<id>/timeline.jsonl
// (internal format, every field optional). The file has a line per status
// report; only changes are kept: started working, waiting (with the question),
// done, failed. Consecutive reports in the same state fold into the first.
type Event struct {
	At     time.Time
	State  string // normalized: busy blocked done failed
	Detail string
}

// TimelineInfo is what the group timeline needs from a job's timeline file.
type TimelineInfo struct {
	Events []Event
	// Links maps each PR/MR URL the job mentioned to when it first did: the
	// state file lists a job's PRs without a date.
	Links map[string]time.Time
}

type rawEvent struct {
	At     string `json:"at"`
	State  string `json:"state"`
	Detail string `json:"detail"`
	Text   string `json:"text"`
}

type tlEntry struct {
	size int64
	mod  time.Time
	info TimelineInfo
}

var (
	tlMu    sync.Mutex
	tlCache = map[string]tlEntry{}
	prURL   = regexp.MustCompile(`https?://[^\s)\]>"'` + "`" + `]+/(?:pull|merge_requests)/\d+`)
)

// maxEvents caps what a job contributes: the timeline shows what happened
// lately, not an agent's whole life.
const maxEvents = 200

// ReadTimeline returns a job's state changes, oldest first; parsed again only
// when the file changed. A missing or unreadable file gives nothing.
func ReadTimeline(id string) TimelineInfo {
	if id == "" {
		return TimelineInfo{}
	}
	return readTimelineFile(filepath.Join(Dir(), "jobs", id, "timeline.jsonl"))
}

func readTimelineFile(path string) TimelineInfo {
	fi, err := os.Stat(path)
	if err != nil {
		return TimelineInfo{}
	}
	tlMu.Lock()
	e, ok := tlCache[path]
	tlMu.Unlock()
	if ok && e.size == fi.Size() && e.mod.Equal(fi.ModTime()) {
		return e.info
	}
	f, err := os.Open(path)
	if err != nil {
		return TimelineInfo{}
	}
	defer f.Close()
	info := parseTimeline(f)
	tlMu.Lock()
	tlCache[path] = tlEntry{size: fi.Size(), mod: fi.ModTime(), info: info}
	tlMu.Unlock()
	return info
}

func parseTimeline(f *os.File) TimelineInfo {
	var info TimelineInfo
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	last := ""
	for sc.Scan() {
		var r rawEvent
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, r.At)
		if err != nil {
			continue
		}
		for _, s := range []string{r.Detail, r.Text} {
			for _, u := range prURL.FindAllString(s, -1) {
				if info.Links == nil {
					info.Links = map[string]time.Time{}
				}
				if _, seen := info.Links[u]; !seen {
					info.Links[u] = at
				}
			}
		}
		st := normState(r.State)
		if st == "running" {
			st = "busy"
		}
		if st != "busy" && st != "blocked" && st != "done" && st != "failed" {
			continue
		}
		if st == last {
			continue
		}
		last = st
		info.Events = append(info.Events, Event{At: at, State: st, Detail: Tidy(r.Detail)})
	}
	if n := len(info.Events); n > maxEvents {
		info.Events = info.Events[n-maxEvents:]
	}
	return info
}
