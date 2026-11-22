package group

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
var asksRe = regexp.MustCompile(`(?i)\bregia context\b|\b(siblings?|coordinator|coordinatore|fratell[io]|sorell[ae])\b|\b(your|my|our|the|this|tuo|nostro|il) (group|gruppo)\b`)

// Asks says whether a user message talks about the group.
func Asks(prompt string) bool { return asksRe.MatchString(prompt) }

// state is what the hook remembers per session, in ~/.cache/regia/group.
type state struct {
	Intro   bool      `json:"intro"`   // the intro was given
	Checked time.Time `json:"checked"` // last look for news
	Nearest string    `json:"nearest"` // nearest container at the last look
	// Explained is when a user message about the group was last answered.
	Explained time.Time `json:"explained,omitzero"`
}

func statePath(self string) string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "regia", "group", fileName(self))
}

// fileName makes an agent id safe as a file name.
func fileName(id string) string {
	r := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "..", "_")
	return r.Replace(id) + ".json"
}

func loadState(self string) state {
	var s state
	if b, err := os.ReadFile(statePath(self)); err == nil {
		_ = json.Unmarshal(b, &s)
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

// MarkRead records that self has read its context (regia context): the hook
// needn't introduce the group to it.
func MarkRead(pos Position) {
	s := loadState(pos.Self)
	s.Intro, s.Nearest = true, pos.Nearest()
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
// it is in a group, then one line when it was moved to another group. name
// looks up agent names (nil: cached listing).
func Check(t *Tree, self, event string, now time.Time, name func(string) string) string {
	pos := t.Resolve(self)
	if !t.aware(pos) {
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
		return intro(t, pos, name)
	}
	if n := pos.Nearest(); n != s.Nearest {
		s.Nearest = n
		return "[regia] you were moved: you're now " + where(t, pos, name) + " — `regia context` shows who is there."
	}
	return ""
}

// Reintro gives a grouped agent the intro again, as its conversation lost it
// (compacted or cleared).
func Reintro(t *Tree, self string, now time.Time, name func(string) string) string {
	if !t.aware(t.Resolve(self)) {
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
	if !t.aware(t.Resolve(self)) {
		saveState(self, s)
		return "[regia] You are not in a regia group: you have no coordinator and no siblings " +
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

// aware says whether the hook has anything to tell this agent: it is in a
// group, coordinates agents, or the user granted it part of the tree.
func (t *Tree) aware(pos Position) bool {
	return pos.Grouped() || t.L.Grants[pos.Self] != ""
}

func intro(t *Tree, pos Position, name func(string) string) string {
	var b strings.Builder
	if pos.Grouped() {
		fmt.Fprintf(&b, "[regia] You are part of a group of agents: %s.\n", where(t, pos, name))
		// The words the user may use, so "check your siblings" needs no explaining.
		b.WriteString("Your coordinator is the agent that started you (or that you were placed under); your siblings are the other agents in your group or under your coordinator.\n")
		b.WriteString("- `regia context` shows your group, coordinator and siblings with what they are doing.\n")
	} else {
		b.WriteString("[regia] You run under regia, the control room where the user watches and organizes their agents.\n")
	}
	// Agents that may organize the tree are told how; workers aren't.
	tools := "the regia MCP tools (whoami, tree, agent, create_group, move, archive…); the regia:organize skill explains how"
	switch g := t.L.Grants[pos.Self]; {
	case g == Everything:
		b.WriteString("- The user granted you the whole tree: you can read and organize it with " + tools + ".\n")
	case g != "":
		fmt.Fprintf(&b, "- The user granted you group %q: you can read and organize it with %s.\n", t.Title(g, name), tools)
	case pos.Family:
		b.WriteString("- You can organize the agents under you with " + tools + ".\n")
	}
	if pos.Grouped() {
		b.WriteString("A line like this one tells you when you are moved to another group.")
	}
	return strings.TrimRight(b.String(), "\n")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
