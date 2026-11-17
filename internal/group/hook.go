package group

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Throttle is how often, at most, the hook looks for news after tool calls.
// A user message always checks.
var Throttle = time.Minute

// ExplainEvery is how often, at most, a user message that talks about the
// group in regia's words gets the explanation again.
var ExplainEvery = 10 * time.Minute

// asksRe matches a user message that talks about the group in regia's words
// ("check your siblings", "use regia context"), in English or Italian.
var asksRe = regexp.MustCompile(`(?i)\bregia (context|note)\b|\b(siblings?|coordinator|coordinatore|blackboard|lavagna|fratell[io]|sorell[ae])\b|\b(your|my|our|the|this|tuo|nostro|il) (group|gruppo)\b`)

// Asks says whether a user message talks about the group.
func Asks(prompt string) bool { return asksRe.MatchString(prompt) }

// state is what the hook remembers per session, in ~/.cache/regia/group.
type state struct {
	Intro   bool                 `json:"intro"`   // the intro was given
	Checked time.Time            `json:"checked"` // last look for news
	Seen    map[string]time.Time `json:"seen"`    // container -> last change from others already announced or read
	Nearest string               `json:"nearest"` // nearest container at the last look
	// Explained is when a user message about the group was last answered.
	Explained time.Time `json:"explained,omitzero"`
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
	if s.Seen == nil {
		s.Seen = map[string]time.Time{}
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
		s.Seen[c] = latest(fromOthers(c, pos.Self))
	}
	s.Intro = true
	saveState(pos.Self, s)
}

// HookInput is the part of a hook payload the group hook reads.
type HookInput struct {
	SessionID string `json:"session_id"`
	Event     string `json:"hook_event_name"`
	Prompt    string `json:"prompt"` // UserPromptSubmit
	Source    string `json:"source"` // SessionStart: startup, resume, clear, compact
}

// HandleHook reads a PostToolUse, UserPromptSubmit or SessionStart payload and writes the
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
	var msg string
	switch {
	case in.Event == "SessionStart" && (in.Source == "compact" || in.Source == "clear"):
		msg = Reintro(t, self, now, nil)
	case in.Event == "UserPromptSubmit" && Asks(in.Prompt):
		msg = Answer(t, self, now, nil)
	}
	if msg == "" {
		msg = Check(t, self, in.Event, now, nil)
	}
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
			others := fromOthers(c, self)
			for _, n := range others { // all of them the first time; on a reintro, the unread ones
				if n.touched().After(s.Seen[c]) {
					total++
				}
			}
			s.Seen[c] = latest(others)
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
		others := fromOthers(c, self)
		for _, n := range others {
			if n.touched().After(s.Seen[c]) {
				authors[n.author()] = true
				news++
			}
		}
		s.Seen[c] = latest(others)
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

// Reintro gives a grouped agent the intro again, as its conversation lost it
// (compacted or cleared).
func Reintro(t *Tree, self string, now time.Time, name func(string) string) string {
	if !t.Resolve(self).Grouped() {
		return ""
	}
	s := loadState(self)
	s.Intro = false
	saveState(self, s)
	return Check(t, self, "SessionStart", now, name)
}

// Answer is for a user message that talks about the group ("check your
// siblings", "use regia context"): the intro again for a grouped agent, so it
// knows the words and the commands; for another, that it is in no group.
// At most once every ExplainEvery: "" then, and the usual Check applies.
func Answer(t *Tree, self string, now time.Time, name func(string) string) string {
	s := loadState(self)
	if now.Sub(s.Explained) < ExplainEvery {
		return ""
	}
	s.Explained = now
	if !t.Resolve(self).Grouped() {
		saveState(self, s)
		return "[regia] You are not in a regia group: you have no coordinator, no siblings and no shared notes " +
			"(those exist only for agents the user grouped in regia, or that a coordinator agent started). " +
			"`regia context` shows this too."
	}
	s.Intro = false
	saveState(self, s)
	return Check(t, self, "UserPromptSubmit", now, name)
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
	// The words the user may use, so "check your siblings" needs no explaining.
	b.WriteString("Your coordinator is the agent that started you (or that you were placed under); your siblings are the other agents in your group or under your coordinator.\n")
	b.WriteString("- `regia context` shows your group, coordinator and siblings with what they are doing, and the blackboard's notes.\n")
	b.WriteString("- `regia note \"…\"` writes one line on the blackboard for the whole group.\n")
	b.WriteString("A line like this one tells you when something new is written.")
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
