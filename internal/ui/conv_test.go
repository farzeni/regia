package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

// An unfolded run shows its steps in a box, the run line above it.
func TestUnfoldedStepsBoxed(t *testing.T) {
	m := convModel(t)
	m.Update(tkey("o"))
	selectWhere(t, m, bigRun)
	m.Update(tkey("enter"))
	ls := strings.Split(ansi.Strip(strings.Join(m.conversationLines(m.agents["aaaa1111"], 80), "\n")), "\n")
	top := -1
	for i, l := range ls {
		if strings.Contains(l, "▾ ⚙ 7 steps") {
			top = i + 1
		}
	}
	if top < 0 || !strings.HasPrefix(strings.TrimSpace(ls[top]), "╭") {
		t.Fatalf("the box should open under the run line:\n%s", strings.Join(ls, "\n"))
	}
	bot := top + 1
	for ; bot < len(ls) && !strings.HasPrefix(strings.TrimSpace(ls[bot]), "╰"); bot++ {
		if l := strings.TrimSpace(ls[bot]); !strings.HasPrefix(l, "│") || !strings.HasSuffix(l, "│") || ansi.StringWidth(ls[bot]) != 80 {
			t.Fatalf("a step line should sit inside the box, 80 wide: %q", ls[bot])
		}
	}
	if bot-top-1 != 7 {
		t.Fatalf("7 steps in the box, got %d:\n%s", bot-top-1, strings.Join(ls[top:bot+1], "\n"))
	}
	t.Log("\n" + strings.Join(ls[top-1:bot+2], "\n"))
}

// convModel: alpha's transcript is internal/claude/testdata/steps.jsonl ("fix
// the parser", a thought, a reply, then Read, Edit, a failed Bash, Write, a
// screenshot, TodoWrite, a subagent; "Done: tests fixed."; "thanks"; a Bash
// still running).
func convModel(t *testing.T) *model {
	t.Helper()
	m := testModel(t)
	m.Update(tkey("esc"))
	goTo(t, m, "aaaa1111")
	p, _ := filepath.Abs("../claude/testdata/steps.jsonl")
	m.agents["aaaa1111"].Transcript = p
	m.h = 50
	m.View()
	return m
}

func convScreen(m *model) string { return ansi.Strip(m.View()) }

func TestRunsFoldedByDefault(t *testing.T) {
	m := convModel(t)
	s := convScreen(m)
	if !strings.Contains(s, "▸ ⚙ 7 steps · edited 2 files · ran 1 command · 1 error · 1m43s") {
		t.Fatalf("the run between the messages should be one folded line:\n%s", s)
	}
	if !strings.Contains(s, "▸ ⚙ 1 thought") || !strings.Contains(s, "ran 1 command · running") {
		t.Fatalf("a run of thoughts, and the last run running:\n%s", s)
	}
	if strings.Contains(s, "go test") {
		t.Fatalf("folded runs don't list their steps:\n%s", s)
	}
}

// selectWhere moves the cursor to the first block f picks.
func selectWhere(t *testing.T, m *model, f func(b convBlock) bool) {
	t.Helper()
	m.convFresh()
	for i, b := range m.conv.blocks {
		if f(b) {
			m.selectConvBlock(i)
			m.View()
			return
		}
	}
	t.Fatal("no such block")
}

func bigRun(b convBlock) bool { return b.kind == blockRun && len(b.run.steps) == 7 }

func TestFocusUnfoldRunAndStep(t *testing.T) {
	m := convModel(t)
	m.Update(tkey("o"))
	if !m.convFocus || !strings.Contains(m.View(), "┃") {
		t.Fatal("o should focus the conversation and light the divider")
	}
	selectWhere(t, m, bigRun)
	m.Update(tkey("enter"))
	s := convScreen(m)
	for _, want := range []string{"▾ ⚙ 7 steps", "◦ read /repo/a.go 10-49", "✎ /repo/a.go:3 +2 −1", "$ go test ./... ✗", "▣ screenshot 4×3", "⇢ subagent: review panel (done)"} {
		if !strings.Contains(s, want) {
			t.Errorf("unfolded run lacks %q:\n%s", want, s)
		}
	}
	// j moves onto the first step; space unfolds it: its detail
	m.Update(tkey("j"))
	m.Update(tkey(" "))
	s = convScreen(m)
	if !strings.Contains(s, "lines 10-49 of 120") {
		t.Fatalf("an unfolded step shows its input and result head:\n%s", s)
	}
	// k back on the run, space folds it again, steps gone
	m.Update(tkey("k"))
	m.Update(tkey(" "))
	if s = convScreen(m); strings.Contains(s, "◦ read") {
		t.Fatalf("space should fold the run:\n%s", s)
	}
	// esc gives the keys back to the list, its cursor where it was
	m.Update(tkey("esc"))
	if m.convFocus || strings.Contains(m.View(), "┃") || m.cursorID != "aaaa1111" {
		t.Fatalf("esc should leave the conversation, on alpha (at %s)", m.cursorID)
	}
	m.Update(tkey("j"))
	if m.cursorID != "bbbb2222" {
		t.Fatalf("after esc j moves the list again (at %s)", m.cursorID)
	}
}

func TestFoldStateIsPerAgent(t *testing.T) {
	m := convModel(t)
	m.Update(tkey("o"))
	selectWhere(t, m, bigRun)
	m.Update(tkey("enter"))
	m.Update(tkey("esc"))
	m.Update(tkey("j")) // beta
	m.View()
	m.Update(tkey("k")) // back on alpha: still unfolded
	if s := convScreen(m); !strings.Contains(s, "◦ read /repo/a.go") {
		t.Fatalf("alpha's unfolded run should stay unfolded:\n%s", s)
	}
	if st := m.convOf("bbbb2222"); len(st.open) != 0 {
		t.Fatalf("beta has its own fold state: %v", st.open)
	}
}

func TestFocusMovesAndFollows(t *testing.T) {
	m := convModel(t)
	m.h = 19 // a short panel: the top isn't on screen
	m.View()
	m.Update(tkey("o"))
	if i := m.convSel(); i != len(m.conv.blocks)-1 {
		t.Fatalf("focus starts on the last block, got %d of %d", i, len(m.conv.blocks))
	}
	m.Update(tkey("g"))
	if s := convScreen(m); m.convSel() != 0 || !strings.Contains(s, "fix the parser") {
		t.Fatalf("g selects the first block and shows it:\n%s", s)
	}
	m.Update(tkey("G"))
	if s := convScreen(m); !strings.Contains(s, "running") {
		t.Fatalf("G goes back to the end:\n%s", s)
	}
	// the cursor's bar marks the selected block
	if !strings.Contains(convScreen(m), "▌") {
		t.Fatal("the selected block should be marked")
	}
}

func TestCopyAndReply(t *testing.T) {
	m := convModel(t)
	var got string
	prev := copyText
	copyText = func(s string) error { got = s; return nil }
	t.Cleanup(func() { copyText = prev })

	m.Update(tkey("o"))
	selectWhere(t, m, func(b convBlock) bool { return b.kind == blockMsg && b.msg.Text == "Done: tests fixed." })
	m.Update(tkey("c"))
	if got != "Done: tests fixed." {
		t.Fatalf("c copied %q", got)
	}
	selectWhere(t, m, bigRun)
	m.Update(tkey("enter"))
	selectWhere(t, m, func(b convBlock) bool { return b.kind == blockStep && b.step.Name == "Bash" && b.step.Failed() })
	m.Update(tkey("c"))
	if !strings.Contains(got, "go test ./...") || !strings.Contains(got, "Exit code 1") {
		t.Fatalf("c on a step copies the call and its output: %q", got)
	}
	// r: the selected block quoted in the input box, the cursor two lines
	// below, the input focused (a step too)
	m.Update(tkey("r"))
	if v := m.ta.Value(); !strings.HasPrefix(v, "> ") || !strings.Contains(v, "\n> ") || !strings.HasSuffix(v, "\n\n") || m.convFocus || !m.typing || got != strings.TrimSuffix(v, "\n\n") {
		t.Fatalf("r on a step: input %q copied %q focus %v typing %v", v, got, m.convFocus, m.typing)
	}
	m.clearDraft()
	m.Update(tkey("esc")) // back to the list
	m.Update(tkey("o"))
	selectWhere(t, m, func(b convBlock) bool { return b.kind == blockMsg && b.msg.Text == "Looking at it." })
	m.Update(tkey("r"))
	m.Update(tkey("ok"))
	if v := m.ta.Value(); v != "> Looking at it.\n\nok" || !m.typing {
		t.Fatalf("r on a message, then typing: input %q typing %v", v, m.typing)
	}
	// i: from the conversation to the input
	m.clearDraft()
	m.typing = false
	m.Update(tkey("o"))
	m.Update(tkey("i"))
	if m.convFocus || !m.typing || m.ta.Value() != "" {
		t.Fatalf("i should leave the conversation for the input: focus %v typing %v input %q", m.convFocus, m.typing, m.ta.Value())
	}
}

// writeConv gives alpha a transcript of raw JSONL lines.
func writeConv(t *testing.T, m *model, lines ...string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "conv.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.agents["aaaa1111"].Transcript = p
}

// A message, three tool calls (one fails), a message: exactly one run line
// between the two messages.
func TestRunLineBetweenMessages(t *testing.T) {
	m := testModel(t)
	m.Update(tkey("esc"))
	goTo(t, m, "aaaa1111")
	use := func(ts, id, cmd string) string {
		return `{"type":"assistant","timestamp":"2026-11-14T09:` + ts + `Z","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"Bash","input":{"command":"` + cmd + `"}}]}}`
	}
	res := func(ts, id, out string, failed bool) string {
		e := ""
		if failed {
			e = `,"is_error":true`
		}
		return `{"type":"user","timestamp":"2026-11-14T09:` + ts + `Z","message":{"content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"` + out + `"` + e + `}]}}`
	}
	writeConv(t, m,
		`{"type":"user","timestamp":"2026-11-14T09:00:00Z","message":{"content":"run the checks"}}`,
		use("00:05", "t1", "go build"), res("00:20", "t1", "ok", false),
		use("00:21", "t2", "go vet"), res("00:40", "t2", "vet: bad", true),
		use("00:41", "t3", "go test"), res("02:15", "t3", "ok", false),
		`{"type":"assistant","timestamp":"2026-11-14T09:02:20Z","message":{"content":[{"type":"text","text":"Checks done."}]}}`,
	)
	s := ansi.Strip(m.View())
	var between []string
	in, mine := false, false
	for _, l := range strings.Split(s, "\n") {
		switch {
		case strings.Contains(l, "run the checks"):
			mine = true
		case mine && strings.Contains(l, "╰"): // your message's box closes
			in, mine = true, false
		case strings.Contains(l, "Checks done."): // the agent's reply
			in = false
		case in && strings.TrimSpace(strings.SplitN(l, "│", 2)[len(strings.SplitN(l, "│", 2))-1]) != "":
			between = append(between, strings.TrimSpace(strings.SplitN(l, "│", 2)[1]))
		}
	}
	// the run line, the reply's time at its end
	if len(between) != 1 || !regexp.MustCompile(`^▸ ⚙ 3 steps · ran 3 commands · 1 error · 2m10s +\d\d:\d\d$`).MatchString(between[0]) {
		t.Fatalf("one run line between the messages, got %q:\n%s", between, s)
	}
	if strings.Contains(s, "go vet") {
		t.Fatalf("the run is folded:\n%s", s)
	}
}

func TestYourMessageIsARightBubble(t *testing.T) {
	m := convModel(t)
	w := 60
	writeConv(t, m, `{"type":"user","timestamp":"2026-11-14T09:00:00Z","message":{"content":"`+strings.Repeat("a long message that wraps ", 12)+`"}}`)
	for _, l := range m.conversationLines(m.agents["aaaa1111"], w) {
		p := strings.TrimRight(ansi.Strip(l), " ")
		if !strings.Contains(p, "wraps") {
			continue
		}
		text := strings.TrimSpace(p)
		if ansi.StringWidth(l) != w || ansi.StringWidth(text) > w*3/4 {
			t.Fatalf("your message should end at the right edge, at most 3/4 wide: %d wide, %d of text: %q", ansi.StringWidth(l), ansi.StringWidth(text), p)
		}
	}
	m = convModel(t)
	ls := m.conversationLines(m.agents["aaaa1111"], w)
	found := false
	for i, l := range ls {
		p := strings.TrimRight(ansi.Strip(l), " ")
		if strings.HasSuffix(p, "fix the parser  │") { // padded, in its box
			found = true
			if ansi.StringWidth(l) != w || !strings.HasPrefix(p, "      ") {
				t.Fatalf("your message should be pushed to the right edge: %q", p)
			}
			if top := ansi.Strip(ls[i-2]); !strings.HasSuffix(top, "╮") || !strings.HasSuffix(strings.TrimRight(ansi.Strip(ls[i+1]), " "), "│") {
				t.Fatalf("your message should sit in a box with a padding row: %q", top)
			}
			if h := strings.TrimRight(ansi.Strip(ls[i-3]), " "); !strings.Contains(h, "you ") || ansi.StringWidth(h) != w {
				t.Fatalf("the head line should be right-aligned above it: %q", h)
			}
		}
	}
	if !found {
		t.Fatal("no bubble")
	}
}

// A step kind registered by tool name draws the step and runs on enter.
func TestStepKindHook(t *testing.T) {
	m := convModel(t)
	var ran *claude.Step
	prevEdit := stepKinds["Edit"]
	stepKinds["Edit"] = stepKind{
		line: func(c stepCtx, s *claude.Step, w int) string { return "✎ " + s.Res.FilePath + " of " + c.a.Name },
		activate: func(m *model, a *claude.Agent, s *claude.Step) tea.Cmd {
			ran = s
			return nil
		},
	}
	t.Cleanup(func() { stepKinds["Edit"] = prevEdit })
	m.Update(tkey("o"))
	selectWhere(t, m, bigRun)
	m.Update(tkey("enter"))
	if s := convScreen(m); !strings.Contains(s, "✎ /repo/a.go of alpha") || !strings.Contains(s, "◦ read /repo/a.go") {
		t.Fatalf("the Edit kind's line should be used, Read's own for Read:\n%s", s)
	}
	selectWhere(t, m, func(b convBlock) bool { return b.kind == blockStep && b.step.Name == "Edit" })
	m.Update(tkey("enter"))
	if ran == nil || ran.Res.Added != 2 {
		t.Fatal("enter should run the kind's activate")
	}
	if m.convOf("aaaa1111").open[ran.ID] {
		t.Fatal("a step with an action doesn't unfold on enter")
	}
}

// A step that shows another agent's state is drawn again when it changes,
// though the transcript didn't grow.
func TestStepSeesLiveAgents(t *testing.T) {
	m := convModel(t)
	prevRead := stepKinds["Read"]
	stepKinds["Read"] = stepKind{line: func(c stepCtx, s *claude.Step, w int) string {
		return "beta is " + c.m.agents["bbbb2222"].State
	}}
	t.Cleanup(func() { stepKinds["Read"] = prevRead })
	m.Update(tkey("o"))
	selectWhere(t, m, bigRun)
	m.Update(tkey("enter"))
	if s := convScreen(m); !strings.Contains(s, "beta is done") {
		t.Fatalf("no live line:\n%s", s)
	}
	m.agents["bbbb2222"].State = "busy"
	m.noteLive()
	if s := convScreen(m); !strings.Contains(s, "beta is busy") {
		t.Fatalf("the conversation should be drawn again when an agent's state changes:\n%s", s)
	}
}

// The mouse finds the block under a panel row.
func TestConvBlockAtRow(t *testing.T) {
	m := convModel(t)
	m.View()
	hit := 0
	for y := range m.h {
		if i := m.convBlockAtRow(y); i >= 0 {
			hit++
			b := m.conv.blocks[i]
			if l := m.conv.histStart + y - m.conv.histRow; l < b.top || l >= b.top+b.n {
				t.Fatalf("row %d maps to block %d at %d+%d", y, i, b.top, b.n)
			}
		}
	}
	if hit == 0 {
		t.Fatal("no block under any row")
	}
	if m.convBlockAtRow(0) != -1 {
		t.Fatal("the card isn't a block")
	}
	// selectBlockAt: a line of the conversation selects its block, and focuses
	b := m.conv.blocks[2]
	if !m.selectBlockAt(b.top+b.n-1) || !m.convFocus || m.convSel() != 2 {
		t.Fatalf("selectBlockAt should select block 2 (got %d, focus %v)", m.convSel(), m.convFocus)
	}
	if m.selectBlockAt(-1) {
		t.Fatal("no block above the conversation")
	}
}

// → on an agent focuses its conversation; ← gives the keys back to the list.
func TestArrowsFocusConversation(t *testing.T) {
	m := convModel(t)
	m.View()
	m.Update(tkey("right"))
	if !m.convFocus {
		t.Fatal("→ on an agent should focus the conversation")
	}
	m.Update(tkey("left"))
	if m.convFocus || m.cursorID != "aaaa1111" {
		t.Fatalf("← should give the keys back to the list, on alpha (at %s)", m.cursorID)
	}
	m.Update(tkey("i")) // from the empty input box too
	m.Update(tkey("right"))
	if !m.convFocus || m.typing {
		t.Fatalf("→ from an empty input: focus %v typing %v", m.convFocus, m.typing)
	}
}

// ↑↓ jumps message to message over the steps, ctrl+↑↓ to your
// messages, ctrl+d/u half a screen.
func TestConvFastJumps(t *testing.T) {
	m := convModel(t)
	m.Update(tkey("o"))
	m.Update(tkey("g"))
	bs := m.conv.blocks
	var msgs, yours []int
	for i, b := range bs {
		if b.kind == blockMsg {
			msgs = append(msgs, i)
			if b.msg.Role == claude.RoleYou {
				yours = append(yours, i)
			}
		}
	}
	if len(msgs) < 3 || len(yours) < 2 {
		t.Fatalf("fixture: %d messages, %d yours", len(msgs), len(yours))
	}
	for _, want := range msgs[1:] {
		if m.convSel() == want {
			continue
		}
		m.Update(tkey("down"))
		if got := m.convSel(); got != want {
			t.Fatalf("↓: block %d, want message at %d (messages %v)", got, want, msgs)
		}
	}
	m.Update(tkey("g"))
	for _, want := range yours {
		if m.convSel() == want {
			continue
		}
		m.Update(tkey("ctrl+down"))
		if got := m.convSel(); got != want {
			t.Fatalf("ctrl+↓: block %d, want your message at %d (yours %v)", got, want, yours)
		}
	}
	m.Update(tkey("ctrl+up"))
	if got := m.convSel(); yours[len(yours)-1] > yours[0] && got >= yours[len(yours)-1] {
		t.Fatalf("ctrl+↑ should go back to an earlier message of yours (at %d)", got)
	}
	m.Update(tkey("g"))
	m.Update(tkey("ctrl+d"))
	if m.convSel() == 0 || !m.convFocus {
		t.Fatal("ctrl+d should move the block cursor down, staying in the conversation")
	}
	m.Update(tkey("ctrl+u"))
	if m.convSel() != 0 {
		t.Fatalf("ctrl+u should come back to the top (at %d)", m.convSel())
	}
	// shift+↓ / j: every block, the run after the first message too
	m.Update(tkey("shift+down"))
	m.Update(tkey("j"))
	if got := m.convSel(); got != 2 {
		t.Fatalf("shift+↓ then j: block %d, want 2", got)
	}
	m.Update(tkey("shift+up"))
	if got := m.convSel(); got != 1 || bs[1].kind == blockMsg {
		t.Fatalf("shift+↑ should land on the run between messages (block %d)", got)
	}
}

// ? lists the conversation's keys, and still fits a 30-row terminal.
func TestHelpListsConversationKeys(t *testing.T) {
	m := testModel(t)
	m.Update(tkey("esc"))
	m.Update(tkey("?"))
	v := ansi.Strip(m.View())
	for _, want := range []string{"o / →: focus the conversation", "quote in the input", "D: the changes tab", "full diff in your tool", "or click", "keys   esc", "pick its model", "refresh", "group's dir"} {
		if !strings.Contains(v, want) {
			t.Fatalf("help lacks %q:\n%s", want, v)
		}
	}
}

func TestRunLineDurations(t *testing.T) {
	for d, want := range map[string]string{"45s": "45s", "2m10s": "2m10s", "1h5m": "1h05m"} {
		dd, _ := time.ParseDuration(d)
		if got := fmtDur(dd); got != want {
			t.Errorf("fmtDur(%s) = %s", d, got)
		}
	}
}
