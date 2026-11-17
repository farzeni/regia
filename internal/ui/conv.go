package ui

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

// The agent panel's conversation is a list of blocks: your messages, the
// agent's replies, and between them runs (the tool calls in a row, folded
// into one line by default) whose steps unfold one line each, and a step
// into its detail. `o` focuses it: a cursor moves between the blocks.
//
// How a step looks and what enter does on it is per tool: see stepKinds.

type blockKind int

const (
	blockMsg  blockKind = iota // yours or the agent's
	blockRun                   // the steps between two messages, as one line
	blockStep                  // one step of an unfolded run
)

// convBlock is a block as drawn: lines [top, top+n) of the conversation.
type convBlock struct {
	kind blockKind
	key  string // stable across redraws: fold state and the cursor follow it
	msg  *claude.Msg
	// the run of a blockRun or a blockStep; on a blockMsg, the run right
	// before the agent's reply: one item, its lines from the run line through
	// its steps (the step blocks sit inside it) to the reply's end
	run    *convRun
	step   *claude.Step // blockStep
	top, n int
}

// convRun is the steps between two messages.
type convRun struct {
	key   string // "run:" + its first step's id
	steps []*claude.Step
}

// convState is an agent's conversation state, kept in memory while regia
// runs: what's unfolded and where the cursor is.
type convState struct {
	open map[string]bool // runs and steps unfolded (everything starts folded)
	ver  int             // counts fold changes: part of the render cache key
	sel  string          // the selected block's key ("" = the last one)
}

// noteLive bumps m.live when any agent's state or name changed since the last
// refresh: conversations whose steps show other agents (a spawn's live
// state) are drawn again then, and only then.
func (m *model) noteLive() {
	var sig uint64
	for id, a := range m.agents { // summed: the map's order doesn't matter
		h := fnv.New64a()
		h.Write([]byte(id + "\x00" + a.State + "\x00" + a.Name))
		sig += h.Sum64()
	}
	if sig != m.liveSig {
		m.liveSig = sig
		m.live++
	}
}

func (m *model) convOf(id string) *convState {
	if m.convs == nil {
		m.convs = map[string]*convState{}
	}
	st := m.convs[id]
	if st == nil {
		st = &convState{open: map[string]bool{}}
		m.convs[id] = st
	}
	return st
}

// convShown is the conversation the last frame drew: for keys and the mouse.
type convShown struct {
	id     string // the agent; "" when the panel shows something else
	w      int
	blocks []convBlock
	// where its lines landed in the panel: line histStart at panel row histRow
	histRow, histStart, histEnd int
}

// --- step kinds --------------------------------------------------------------

// stepCtx is what a step is drawn in: the model (read only: e.g. a spawned
// agent's live state from m.agents) and the agent whose conversation it is.
// What a step's line reads from the model must change m.live when it
// changes, or the cached conversation won't be drawn again.
type stepCtx struct {
	m *model
	a *claude.Agent
}

// stepKind draws one kind of step (by tool name) and says what enter does on
// it. A nil field falls back to the generic one (genericLine, genericDetail;
// no activate: enter folds and unfolds).
type stepKind struct {
	// line is the folded step, one line at most w wide (the indent is added).
	line func(c stepCtx, s *claude.Step, w int) string
	// detail is what an unfolded step shows under its line, w wide.
	detail func(c stepCtx, s *claude.Step, w int) []string
	// activate runs on enter (open the file, the image, the agent…).
	activate func(m *model, a *claude.Agent, s *claude.Step) tea.Cmd
}

// stepKinds by tool name (claude.ThinkingTool for thoughts). One kind may
// cover sub-cases and dispatch inside: a `claude --bg` spawn is a Bash call,
// so the Bash kind tells it apart by its command.
var stepKinds = map[string]stepKind{
	claude.ThinkingTool: {line: thoughtLine, detail: thoughtDetail},
}

func kindOf(s *claude.Step) stepKind {
	k, ok := stepKinds[s.Name]
	if !ok && s.Res != nil && len(s.Res.Images) > 0 {
		k = imageKind
	}
	if k.line == nil {
		k.line = genericLine
	}
	if k.detail == nil {
		k.detail = genericDetail
	}
	return k
}

// toolLabel: an MCP tool as server:tool (mcp__browser__take_screenshot →
// browser:take_screenshot).
func toolLabel(name string) string {
	if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
		if i := strings.Index(rest, "__"); i > 0 {
			return rest[:i] + ":" + rest[i+2:]
		}
	}
	return name
}

// stepGlyph: • done, ✗ failed, ◆ running.
func stepGlyph(s *claude.Step) string {
	switch {
	case s.Failed():
		return stErr.Render("✗")
	case s.Running():
		return lipgloss.NewStyle().Foreground(cBusy).Render("◆")
	}
	return stDim.Render("•")
}

// genericLine is "• ToolName short-input".
func genericLine(_ stepCtx, s *claude.Step, w int) string {
	name := toolLabel(s.Name)
	in := strings.Join(strings.Fields(s.ShortInput()), " ")
	return stepGlyph(s) + " " + name + " " + stDim.Render(trunc(in, w-3-len(name)))
}

// genericDetail is the input fields, then the head of the result.
func genericDetail(_ stepCtx, s *claude.Step, w int) []string {
	var out []string
	for _, k := range sortedKeys(s.In) {
		out = append(out, stDim.Render(trunc(k+": "+s.In[k], w)))
	}
	switch {
	case s.Running():
		out = append(out, lipgloss.NewStyle().Foreground(cBusy).Render("running…"))
	case s.Res != nil:
		if n := len(s.Res.Images); n > 0 {
			out = append(out, stDim.Render(count(n, "image")))
		}
		st := stDim
		if s.Failed() {
			st = stErr
		}
		ls := strings.Split(strings.TrimRight(s.Out, "\n"), "\n")
		const head = 8
		for _, l := range ls[:min(len(ls), head)] {
			out = append(out, st.Render(trunc(l, w)))
		}
		if len(ls) > head {
			out = append(out, stDim.Render(fmt.Sprintf("… +%d lines", len(ls)-head)))
		}
	}
	return out
}

func thoughtLine(_ stepCtx, s *claude.Step, w int) string {
	l := "… thought"
	if t := strings.TrimSpace(s.Text); t != "" {
		l += ": " + strings.Join(strings.Fields(t), " ")
	}
	return stDim.Render(trunc(l, w))
}

func thoughtDetail(_ stepCtx, s *claude.Step, w int) []string {
	if strings.TrimSpace(s.Text) == "" {
		return []string{stDim.Render(trunc("(not in the transcript)", w))}
	}
	var out []string
	for _, l := range wrap(s.Text, w) {
		out = append(out, stDim.Render(strings.TrimRight(l, " ")))
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// --- runs ----------------------------------------------------------------------

// editTools change a file; the run line counts the files.
var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

// runLine is "▸ ⚙ 14 steps · edited 3 files · ran 5 commands · 1 error · 2m10s".
func runLine(r *convRun, open bool, w int) string {
	steps, cmds, errs := 0, 0, 0
	files := map[string]bool{}
	var first, last time.Time
	running := false
	for _, s := range r.steps {
		if first.IsZero() {
			first = s.At
		}
		for _, t := range []time.Time{s.At, s.End} {
			if t.After(last) {
				last = t
			}
		}
		running = running || s.Running()
		if s.Name == claude.ThinkingTool {
			continue
		}
		steps++
		if s.Failed() {
			errs++
		}
		if s.Name == "Bash" {
			cmds++
		}
		if editTools[s.Name] {
			p := s.In["file_path"]
			if s.Res != nil && s.Res.FilePath != "" {
				p = s.Res.FilePath
			}
			if p == "" {
				p = s.In["notebook_path"]
			}
			files[p] = true
		}
	}
	parts := []string{count(steps, "step")}
	if steps == 0 {
		parts = []string{count(len(r.steps), "thought")}
	}
	if len(files) > 0 {
		parts = append(parts, "edited "+count(len(files), "file"))
	}
	if cmds > 0 {
		parts = append(parts, "ran "+count(cmds, "command"))
	}
	s := strings.Join(parts, " · ")
	e := ""
	if errs > 0 {
		e = " · " + count(errs, "error")
	}
	d := ""
	if dur := last.Sub(first); dur >= time.Second && !first.IsZero() {
		d = " · " + fmtDur(dur)
	}
	if running {
		d += " · running"
	}
	fold := "▸ "
	if open {
		fold = "▾ "
	}
	tail := lipgloss.Width(e + d)
	l := stDim.Render(fold+"⚙ "+trunc(s, w-4-tail)) + stErr.Render(e) + stDim.Render(d)
	if ansi.StringWidth(l) > w || w-4-tail < lipgloss.Width(parts[0]) { // a narrow panel: the counts don't fit next to the errors and time
		l = ansi.Truncate(stDim.Render(fold+"⚙ "+s)+stErr.Render(e)+stDim.Render(d), max(0, w), "…")
	}
	return l
}

// fmtDur is 45s, 2m10s, 1h05m.
func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// --- messages --------------------------------------------------------------------

// bubblePadX is the space between your message's text and its box, each side
// (one blank row above and below).
const bubblePadX = 2

// stBubbleEdge draws the box around your message: your tint, dimmed.
var stBubbleEdge = lipgloss.NewStyle().Foreground(lipgloss.Color("97"))

// yourBubble is one of your messages: pushed to the right edge, at most 3/4
// of the width, in your tint with room around the text, in a rounded box (no
// background); the head line right-aligned too.
func yourBubble(msg *claude.Msg, w int) []string {
	text := claude.PasteRe.ReplaceAllString(msg.Text, "[pasted text]")
	inner := max(10, w*3/4-2-2*bubblePadX)
	ls := wrap(strings.TrimRight(text, "\n"), inner)
	bw := 0
	for i, l := range ls {
		ls[i] = strings.TrimRight(l, " ")
		bw = max(bw, lipgloss.Width(ls[i]))
	}
	bw += 2 * bubblePadX
	fill := strings.Repeat(" ", bw) // a padding row
	edge := func(l, r string) string { return stBubbleEdge.Render(l + strings.Repeat("─", bw) + r) }
	side := stBubbleEdge.Render("│")
	box := []string{edge("╭", "╮"), side + fill + side}
	sp := strings.Repeat(" ", bubblePadX)
	for _, l := range ls {
		box = append(box, side+sp+stYou.Render(l)+strings.Repeat(" ", bw-bubblePadX-lipgloss.Width(l))+side)
	}
	box = append(box, side+fill+side, edge("╰", "╯"))
	left := strings.Repeat(" ", max(0, w-bw-2))
	head := roleStyle(claude.RoleYou).Bold(true).Render("you") + " " + stDim.Render(humanTime(msg.At))
	out := []string{strings.Repeat(" ", max(0, w-lipgloss.Width(head))) + head}
	for _, l := range box {
		out = append(out, left+l)
	}
	return out
}

// agentLines is one of the agent's replies: its markdown (paths and URLs
// linked when links are on; relative paths are in dir). Its time is drawn
// by drawConv: at the end of the run line above it, or on a line of its own.
func agentLines(msg *claude.Msg, w int, dir string) []string {
	out := agentMarkdown(msg.Text, max(10, w), dir)
	for len(out) > 0 && strings.TrimSpace(ansi.Strip(out[len(out)-1])) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// --- the whole conversation ----------------------------------------------------

type convKey struct {
	path, day string
	n, w, ver int
	live      int // m.live: steps showing other agents' state
}

// convCache keeps the last few agents' rendered conversations: the panel is
// drawn 8 times a second while an agent works, and going back and forth
// between a few agents shouldn't render them again. A new message or step,
// a fold, an agent's state or name (m.live), the width or the day draws it
// again.
var convCache [4]struct {
	key    convKey
	lines  []string
	blocks []convBlock
}
var convNext int

// conversationLines is the agent panel's body (see the top of this file),
// oldest first, with day separators. In focus, the selected block is marked
// with a bar in its first column.
func (m *model) conversationLines(a *claude.Agent, w int) []string {
	lines, blocks := m.renderConv(a, w)
	m.conv.id, m.conv.w, m.conv.blocks = a.ID, w, blocks
	if !m.convFocus || len(blocks) == 0 {
		return lines
	}
	i := m.convSel()
	b := blocks[i]
	out := append([]string(nil), lines...)
	bar := stCursor.Render("▌") // not the divider's purple: the two read apart
	for j := b.top; j < b.top+b.n && j < len(out); j++ {
		out[j] = bar + out[j][1:] // block lines start with a plain space
	}
	return out
}

func (m *model) renderConv(a *claude.Agent, w int) ([]string, []convBlock) {
	path := claude.TranscriptPath(a)
	msgs := claude.Messages(path)
	st := m.convOf(a.ID)
	k := convKey{path, time.Now().Format("2006-01-02 15:04"), // "3 minutes ago" moves on: drawn again each minute
		len(msgs), w, st.ver, m.live}
	slot := -1
	for i, c := range convCache {
		if c.key == k && c.lines != nil {
			return c.lines, c.blocks
		}
		if c.key.path == path && c.key.w == w {
			slot = i // this agent's stale render: replace it
		}
	}
	lines, blocks := drawConv(stepCtx{m, a}, claude.Conversation(msgs), st.open, w)
	if slot < 0 {
		slot, convNext = convNext, (convNext+1)%len(convCache)
	}
	convCache[slot].key, convCache[slot].lines, convCache[slot].blocks = k, lines, blocks
	return lines, blocks
}

// maxMessages caps how far back the conversation goes in the panel.
const maxMessages = 300

// drawConv lays the entries out as blocks, w wide: every block line starts
// with a one-column gutter (a space; the cursor's bar in focus).
func drawConv(c stepCtx, es []claude.Entry, open map[string]bool, w int) ([]string, []convBlock) {
	// from the maxMessages-th message from the end
	start, n := 0, 0
	for i := len(es) - 1; i >= 0; i-- {
		if es[i].Msg != nil {
			if n++; n == maxMessages {
				start = i
				break
			}
		}
	}
	es = es[start:]
	cw := w - 1 // the gutter
	var out []string
	var blocks []convBlock
	lastDay := ""
	day := func(t time.Time) {
		if d := dayLabel(t); d != lastDay && !t.IsZero() {
			lastDay = d
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, stDim.Render(trunc("── "+d+" "+strings.Repeat("─", w), w)))
		}
	}
	add := func(b convBlock, ls []string) {
		b.top, b.n = len(out), len(ls)
		for _, l := range ls {
			out = append(out, " "+l)
		}
		blocks = append(blocks, b)
	}
	// the agent's reply right after a run (same day): the two are one item,
	// the reply's time at the end of the run line, the reply right under it
	replyAfter := func(i int) *claude.Msg {
		if i < len(es) && es[i].Msg != nil && es[i].Msg.Role == claude.RoleAgent && dayLabel(es[i].Msg.At) == lastDay {
			return es[i].Msg
		}
		return nil
	}
	timeLine := func(msg *claude.Msg) []string {
		if msg.At.IsZero() {
			return nil
		}
		t := humanTime(msg.At)
		return []string{strings.Repeat(" ", max(0, cw-len(t))) + stDim.Render(t)}
	}
	for i := 0; i < len(es); {
		if msg := es[i].Msg; msg != nil {
			day(msg.At)
			out = append(out, "")
			var ls []string
			if msg.Role == claude.RoleYou {
				ls = yourBubble(msg, cw)
			} else {
				ls = append(timeLine(msg), agentLines(msg, cw, c.a.Cwd)...)
			}
			add(convBlock{kind: blockMsg, key: msgKey(msg), msg: msg}, ls)
			i++
			continue
		}
		r := &convRun{}
		for ; i < len(es) && es[i].Step != nil; i++ {
			r.steps = append(r.steps, es[i].Step)
		}
		r.key = "run:" + r.steps[0].ID
		day(r.steps[0].At)
		out = append(out, "")
		reply := replyAfter(i)
		line := runLine(r, open[r.key], cw)
		timed := false
		if reply != nil && !reply.At.IsZero() && cw-len(humanTime(reply.At)) >= 30 { // narrower: the reply's time on its own line
			t := humanTime(reply.At)
			line = pad(runLine(r, open[r.key], max(1, cw-len(t)-2)), cw-len(t)) + stDim.Render(t)
			timed = true
		}
		item := len(blocks) // the run's block, or the run + reply's
		if reply != nil {
			add(convBlock{kind: blockMsg, key: msgKey(reply), msg: reply, run: r}, []string{line})
		} else {
			add(convBlock{kind: blockRun, key: r.key, run: r}, []string{line})
		}
		if open[r.key] { // the unfolded steps, in a box
			iw := max(4, cw-4)
			side := stDim.Render("│")
			boxed := func(l string) string { return side + " " + pad(l, iw) + " " + side }
			out = append(out, " "+stDim.Render("╭"+strings.Repeat("─", iw+2)+"╮"))
			for _, s := range r.steps {
				k := kindOf(s)
				ls := []string{boxed(k.line(c, s, iw))}
				if open[s.ID] {
					for _, l := range k.detail(c, s, iw-2) {
						ls = append(ls, boxed("  "+l))
					}
				}
				add(convBlock{kind: blockStep, key: s.ID, run: r, step: s}, ls)
			}
			out = append(out, " "+stDim.Render("╰"+strings.Repeat("─", iw+2)+"╯"))
		}
		if reply == nil {
			continue
		}
		var ls []string
		if !timed {
			ls = timeLine(reply)
		}
		for _, l := range append(ls, agentLines(reply, cw, c.a.Cwd)...) {
			out = append(out, " "+l)
		}
		blocks[item].n = len(out) - blocks[item].top // the item spans the run line, its steps, the reply
		i++
	}
	if len(out) == 0 {
		out = []string{stDim.Render("no conversation yet")}
	}
	return out, blocks
}

// msgKey names a message across redraws: when it was written and its start.
func msgKey(msg *claude.Msg) string {
	return fmt.Sprintf("msg:%d:%d:%s", msg.At.UnixNano(), msg.Role, trunc(msg.Text, 24))
}

// --- focus and the block cursor ---------------------------------------------------

// convSel is the selected block's index in m.conv.blocks (the last one when
// the selection is gone, e.g. folded away).
func (m *model) convSel() int {
	st := m.convOf(m.conv.id)
	for i, b := range m.conv.blocks {
		if b.key == st.sel {
			return i
		}
	}
	return max(0, len(m.conv.blocks)-1)
}

// convFresh draws the selected agent's conversation again if the last frame
// showed something else, so keys act on what is shown.
func (m *model) convFresh() bool {
	r := m.selected()
	if r == nil || r.Agent == nil || m.conv.id != r.ID || m.conv.w == 0 {
		return false
	}
	m.conversationLines(r.Agent, m.conv.w)
	return len(m.conv.blocks) > 0
}

// convHint is what the foot says when the conversation takes the keys.
const convHint = "conversation: ↑↓ messages · shift+↑↓ steps too · ctrl+↑↓ yours · enter/space fold · c copy · r quote · i input · alt+enter attach · ←/esc back"

// focusConv is `o`: the conversation takes the keys, the cursor on the last
// block (or where it was).
func (m *model) focusConv() {
	if !m.convFresh() {
		m.flash("no conversation to focus")
		return
	}
	m.convFocus, m.convReveal, m.typing = true, true, false // → from an empty input box too
	m.flash(convHint)
}

// selectConvBlock selects block i of what is shown and brings it into view.
// (The mouse uses it with convBlockAtRow.)
func (m *model) selectConvBlock(i int) {
	if len(m.conv.blocks) == 0 {
		return
	}
	i = max(0, min(len(m.conv.blocks)-1, i))
	m.convOf(m.conv.id).sel = m.conv.blocks[i].key
	m.convReveal = true
}

// selectBlockAt selects the block drawn at line l of the conversation (as
// conversationLines returned it) and focuses the conversation; false when no
// block is there (a blank line, a day separator).
func (m *model) selectBlockAt(l int) bool {
	if i := m.blockAtLine(l); i >= 0 {
		m.selectConvBlock(i)
		m.convFocus = true
		return true
	}
	return false
}

// blockAtLine is the innermost block drawn at line l (a step inside a run +
// reply item is the step), or -1.
func (m *model) blockAtLine(l int) int {
	at := -1
	for i, b := range m.conv.blocks {
		if l >= b.top && l < b.top+b.n {
			at = i // the blocks inside an item come after it
		}
	}
	return at
}

// convBlockAtRow is the block drawn at panel row y (0 = the panel's top), or
// -1: for clicks.
func (m *model) convBlockAtRow(y int) int {
	l := m.conv.histStart + y - m.conv.histRow
	if m.conv.id == "" || y < m.conv.histRow || l >= m.conv.histEnd {
		return -1
	}
	return m.blockAtLine(l)
}

// toggleConv folds or unfolds a run or a step.
func (m *model) toggleConv(b convBlock) {
	key := b.key
	if b.kind == blockMsg { // a reply folds the run that comes with it
		if b.run == nil {
			return
		}
		key = b.run.key
	}
	st := m.convOf(m.conv.id)
	st.open[key] = !st.open[key]
	if !st.open[key] {
		delete(st.open, key)
	}
	st.ver++
	st.sel = b.key
	m.convReveal = true
}

// activateConv is enter: a step's own action when its kind has one, else
// fold / unfold.
func (m *model) activateConv(b convBlock) tea.Cmd {
	if b.kind == blockStep {
		if k := kindOf(b.step); k.activate != nil {
			if a := m.agents[m.conv.id]; a != nil {
				return k.activate(m, a, b.step)
			}
		}
	}
	m.toggleConv(b)
	return nil
}

// blockText is what y copies: a message's text, a step's call and output, a
// run's lines.
func blockText(c stepCtx, b convBlock) string {
	switch b.kind {
	case blockMsg:
		return b.msg.Text
	case blockStep:
		return strings.TrimSpace(b.step.Text + "\n\n" + b.step.Out)
	}
	ls := []string{ansi.Strip(runLine(b.run, false, 200))[len("▸ "):]}
	for _, s := range b.run.steps {
		ls = append(ls, ansi.Strip(kindOf(s).line(c, s, 200)))
	}
	return strings.Join(ls, "\n")
}

// quoteReply is r: the selected block quoted in the input box (and on the
// clipboard), the cursor two lines below it, ready for your words.
func (m *model) quoteReply(b convBlock) {
	text := strings.TrimSpace(blockText(stepCtx{m, m.agents[m.conv.id]}, b))
	if text == "" {
		return
	}
	q := "> " + strings.ReplaceAll(text, "\n", "\n> ")
	if err := copyText(q); err != nil {
		m.flash("clipboard: %v", err)
	}
	v := m.draft()
	if strings.TrimSpace(v) != "" { // below what you had typed
		v = strings.TrimRight(v, "\n") + "\n\n"
	} else {
		v = ""
	}
	m.ta.SetValue(v + q + "\n\n")
	m.convFocus, m.typing = false, true
}

// updateConv handles keys while the conversation has focus.
func (m *model) updateConv(k tea.KeyMsg) tea.Cmd {
	if !m.convFresh() {
		m.convFocus = false
		return m.updateNormal(k)
	}
	i := m.convSel()
	b := m.conv.blocks[i]
	page := max(1, m.bodyHeight()/2)
	switch k.String() {
	case "ctrl+c":
		return tea.Quit
	case "?":
		m.help = !m.help
	case "esc", "o", "q", "left", "h": // back to the list
		m.convFocus = false
		m.status = ""
	case "j", "shift+down": // every block: runs and steps too
		m.selectConvBlock(i + 1)
	case "k", "shift+up":
		m.selectConvBlock(i - 1)
	case "up", "down": // message to message, over the steps
		m.jumpConv(i, k.String() == "down", func(b convBlock) bool { return b.kind == blockMsg })
	case "ctrl+up", "ctrl+down", "alt+up", "alt+down", "[", "]": // your turns
		down := k.String() == "ctrl+down" || k.String() == "alt+down" || k.String() == "]"
		m.jumpConv(i, down, func(b convBlock) bool { return b.kind == blockMsg && b.msg.Role == claude.RoleYou })
	case "ctrl+d", "ctrl+u": // half a screen
		m.selectConvBlock(m.convBlockNear(b.top + map[bool]int{true: page, false: -page}[k.String() == "ctrl+d"]))
	case "g", "home":
		m.selectConvBlock(0)
	case "G", "end":
		m.selectConvBlock(len(m.conv.blocks))
	case "pgup":
		m.panelScroll += page
	case "pgdown":
		m.panelScroll = max(0, m.panelScroll-page)
	case " ":
		m.toggleConv(b)
	case "enter":
		return m.activateConv(b)
	case "c":
		if err := copyText(blockText(stepCtx{m, m.agents[m.conv.id]}, b)); err != nil {
			m.flash("clipboard: %v", err)
		} else {
			m.flash("copied")
		}
	case "r":
		m.quoteReply(b)
	case "i": // to the input, out of the conversation
		m.convFocus, m.typing = false, true
		m.status = ""
	case "alt+enter": // into the agent's session (shift+enter can't be told from enter)
		a := m.agents[m.conv.id]
		if a == nil {
			return nil
		}
		if a.Interactive {
			m.flash("interactive session: open it in its own terminal (pid shown in `claude agents`)")
			return nil
		}
		m.convFocus, m.typing = false, false
		return attachSession(a.ID)
	}
	return nil
}

// jumpConv selects the nearest block after (down) or before block i that
// matches; with none, the last or first block.
func (m *model) jumpConv(i int, down bool, match func(convBlock) bool) {
	d, end := -1, 0
	if down {
		d, end = 1, len(m.conv.blocks)-1
	}
	for j := i + d; j >= 0 && j < len(m.conv.blocks); j += d {
		if match(m.conv.blocks[j]) {
			m.selectConvBlock(j)
			return
		}
	}
	m.selectConvBlock(end)
}

// convBlockNear is the index of the block drawn at line l of the
// conversation, or the nearest one.
func (m *model) convBlockNear(l int) int {
	for i, b := range m.conv.blocks {
		if l < b.top+b.n {
			return i
		}
	}
	return len(m.conv.blocks) - 1
}

// revealConv scrolls the panel so the selected block shows (its top first),
// once after the cursor moved. hist has len lines and room rows.
func (m *model) revealConv(n, room int) {
	if !m.convReveal || !m.convFocus || m.conv.id != m.cursorID || len(m.conv.blocks) == 0 {
		return
	}
	m.convReveal = false
	b := m.conv.blocks[m.convSel()]
	start := n - m.panelScroll - room
	top, bot := max(0, b.top-1), b.top+b.n+1 // a line of margin: the ↑/↓ marks
	switch {
	case top < start:
		start = top
	case bot > start+room:
		start = min(top, bot-room)
	}
	m.panelScroll = max(0, min(n-room, n-start-room))
}
