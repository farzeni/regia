package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Every conversation on disk, for searching the whole history: the
// transcripts in ~/.claude/projects/<project>/<session>.jsonl, including
// sessions that `claude agents --all` no longer lists (removed, old
// interactive ones). A subagent's transcript sits a level deeper and is left
// out: it's part of its session.

// SessionMeta is what a transcript says about its session besides the
// conversation: the title Claude Code gave it (or you did, with /rename) and
// the directory it ran in.
type SessionMeta struct {
	Title  string
	Cwd    string
	custom bool // Title is yours: Claude Code's own doesn't replace it
}

var (
	aiTitleKey     = []byte(`"aiTitle"`)
	customTitleKey = []byte(`"customTitle"`)
	cwdKey         = []byte(`"cwd":"`)
)

// read picks the title and directory up from a transcript line: the last
// title wins (a custom one over Claude Code's own), the first directory.
func (sm *SessionMeta) read(line []byte) {
	custom := bytes.Contains(line, customTitleKey)
	title := custom || bytes.Contains(line, aiTitleKey)
	where := sm.Cwd == "" && bytes.Contains(line, cwdKey)
	if !title && !where {
		return
	}
	var e struct {
		Cwd         string `json:"cwd"`
		AiTitle     string `json:"aiTitle"`
		CustomTitle string `json:"customTitle"`
	}
	if json.Unmarshal(line, &e) != nil {
		return
	}
	if sm.Cwd == "" {
		sm.Cwd = e.Cwd
	}
	switch {
	case e.CustomTitle != "":
		sm.Title = e.CustomTitle
		sm.custom = true
	case e.AiTitle != "" && !sm.custom:
		sm.Title = e.AiTitle
	}
}

// Meta is a transcript's SessionMeta, from what Messages has read of it
// (it reads it if it hasn't yet).
func Meta(path string) SessionMeta {
	Messages(path)
	mcMu.Lock()
	c := mcMap[path]
	mcMu.Unlock()
	if c == nil {
		return SessionMeta{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.meta
}

// HistorySources is every session transcript on disk whose session id is not
// in skip (the sessions already listed), with the session id as its ID. A
// session found in two projects (it moved into a worktree) counts once, its
// bigger transcript.
func HistorySources(skip map[string]bool) []Source {
	paths, _ := filepath.Glob(filepath.Join(Dir(), "projects", "*", "*.jsonl"))
	best := map[string]string{}
	size := map[string]int64{}
	for _, p := range paths {
		sid := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		if skip[sid] {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if _, ok := best[sid]; !ok || fi.Size() > size[sid] {
			best[sid], size[sid] = p, fi.Size()
		}
	}
	srcs := make([]Source, 0, len(best))
	for sid, p := range best {
		srcs = append(srcs, Source{ID: sid, Path: p})
	}
	return srcs
}

// FirstPrompt is the first thing you wrote in a conversation, on one line:
// the name of a session without a title.
func FirstPrompt(msgs []Msg) string {
	for _, m := range msgs {
		if m.Role == RoleYou {
			if s := strings.Join(strings.Fields(PasteRe.ReplaceAllString(m.Text, "[paste]")), " "); s != "" {
				return s
			}
		}
	}
	return ""
}
