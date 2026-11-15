package group

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Throttle is how often, at most, the hook looks for news after tool calls.
// A user message always checks.
var Throttle = time.Minute

// state is what the hook remembers per session, in ~/.cache/regia/group.
type state struct {
	Intro   bool           `json:"intro"`   // the intro was given
	Checked time.Time      `json:"checked"` // last look for news
	Told    map[string]int `json:"told"`    // container -> notes from others already announced or read
	Nearest string         `json:"nearest"` // nearest container at the last look
}

func statePath(self string) string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "regia", "group", strings.ReplaceAll(fileName(self), ".jsonl", ".json"))
}

func loadState(self string) state {
	var s state
	if b, err := os.ReadFile(statePath(self)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Told == nil {
		s.Told = map[string]int{}
	}
	return s
}

func saveState(self string, s state) {
	b, _ := json.Marshal(s)
	p := statePath(self)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

// MarkRead records that self has seen every note it can see (regia context).
func MarkRead(t *Tree, pos Position) {
	s := loadState(pos.Self)
	for _, c := range pos.Visible() {
		s.Told[c], _ = fromOthers(c, pos.Self)
	}
	s.Intro = true
	saveState(pos.Self, s)
}

// HookInput is the part of a hook payload the group hook reads.
type HookInput struct {
	SessionID string `json:"session_id"`
	Event     string `json:"hook_event_name"`
}

// HandleHook reads a PostToolUse or UserPromptSubmit payload and writes the
// hook's JSON answer when there is something to say. Silent otherwise, and
// never an error: a failing hook would show up in every session.
func HandleHook(stdin io.Reader, stdout io.Writer, jobDir string, now time.Time) {
	if !Enabled() {
		return
	}
	var in HookInput
	b, _ := io.ReadAll(io.LimitReader(stdin, 64<<20)) // tool results can be big; cut, the JSON wouldn't parse
	_ = json.Unmarshal(b, &in)
	self := SelfID(jobDir, in.SessionID)
	if self == "" {
		return
	}
	t := LoadTree()
	msg := Check(t, self, in.Event, now, nil)
	if msg == "" {
		return
	}
	ev := in.Event
	if ev == "" {
		ev = "PostToolUse"
	}
	out, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]string{"hookEventName": ev, "additionalContext": msg},
	})
	_, _ = stdout.Write(out)
}

// Check decides what, if anything, to tell self now: the intro the first time
// it is in a group, then one line when there are notes from others it hasn't
// heard about or it was moved to another group. name looks up agent names
// (nil: cached listing).
func Check(t *Tree, self, event string, now time.Time, name func(string) string) string {
	pos := t.Resolve(self)
	if !pos.Grouped() {
		return ""
	}
	s := loadState(self)
	if s.Intro && event != "UserPromptSubmit" && now.Sub(s.Checked) < Throttle {
		return ""
	}
	if name == nil {
		name = cachedNames()
	}
	s.Checked = now
	defer func() { saveState(self, s) }()

	if !s.Intro {
		s.Intro, s.Nearest = true, pos.Nearest()
		total := 0
		for _, c := range pos.Visible() {
			s.Told[c], _ = fromOthers(c, self)
			total += s.Told[c]
		}
		return intro(t, pos, name, total)
	}

	var parts []string
	if n := pos.Nearest(); n != s.Nearest {
		s.Nearest = n
		parts = append(parts, "you were moved: you're now "+where(t, pos, name))
	}
	news, authors := 0, map[string]bool{}
	for _, c := range pos.Visible() {
		count, others := fromOthers(c, self)
		if count > s.Told[c] {
			for _, n := range others[min(s.Told[c], len(others)):] {
				authors[n.author()] = true
			}
			news += count - s.Told[c]
		}
		s.Told[c] = count
	}
	if news > 0 {
		names := make([]string, 0, len(authors))
		for a := range authors {
			names = append(names, a)
		}
		sort.Strings(names)
		parts = append(parts, fmt.Sprintf("%d new note%s in your group from %s", news, plural(news), strings.Join(names, ", ")))
	}
	if len(parts) == 0 {
		return ""
	}
	return "[regia] " + strings.Join(parts, "; ") + " — `regia context` to read."
}

// where describes the agent's place in one phrase.
func where(t *Tree, pos Position, name func(string) string) string {
	var ps []string
	if len(pos.Chain) > 0 {
		c := pos.Chain[0]
		if IsGroup(c) {
			ps = append(ps, fmt.Sprintf("in group %q with %d other agent%s", t.Title(c, name), len(t.Siblings(pos)), plural(len(t.Siblings(pos)))))
		} else {
			ps = append(ps, fmt.Sprintf("working under coordinator %q", t.Title(c, name)))
		}
	}
	if pos.Family {
		n := len(t.Children(pos))
		ps = append(ps, fmt.Sprintf("coordinating %d agent%s", n, plural(n)))
	}
	return strings.Join(ps, ", ")
}

func intro(t *Tree, pos Position, name func(string) string, notes int) string {
	var b strings.Builder
	// Facts only, no advice on what to write or how to weigh it: the group
	// shares a blackboard and the agents decide how to use it.
	fmt.Fprintf(&b, "[regia] You are part of a group of agents: %s. The group shares a blackboard", where(t, pos, name))
	if notes > 0 {
		fmt.Fprintf(&b, " (%d unread note%s on it)", notes, plural(notes))
	}
	b.WriteString(".\n")
	b.WriteString("- `regia context` reads it: who is in the group, what they are doing, and the notes.\n")
	b.WriteString("- `regia note \"…\"` writes one line on it for the group.\n")
	b.WriteString("A line like this one tells you when something new is written.")
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
