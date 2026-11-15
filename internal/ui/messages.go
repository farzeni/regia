package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/farzeni/regia/internal/claude"
)

// The conversation from the transcript, as the panels show it: a head line
// (who, when), then the message, the agent's markdown rendered.

// messageHead is "▸ agent 14:05" (bold for the message that matters).
func messageHead(msg claude.Msg, bold bool) string {
	return roleStyle(msg.Role).Bold(bold).Render("▸ "+roleTag[msg.Role]) + " " + stDim.Render(clock(msg.At))
}

// messageLines renders a message w wide; md reports whether it was rendered
// as markdown (the agent's text) rather than wrapped as plain text (your
// messages and tool output, where every line break counts).
func messageLines(msg claude.Msg, w int) (lines []string, md bool) {
	if msg.Role == claude.RoleAgent {
		return renderMarkdown(msg.Text, w), true
	}
	return wrap(strings.TrimRight(msg.Text, "\n"), w), false
}

// yours renders one of your messages for the history: pastes folded, in your
// tint.
func yours(msg claude.Msg, w int) []string {
	text := claude.PasteRe.ReplaceAllString(msg.Text, "[pasted text]")
	var out []string
	for _, l := range wrap(strings.TrimRight(text, "\n"), w) {
		out = append(out, stYou.Render(strings.TrimRight(l, " ")))
	}
	return out
}

// latestReply is the agent's last text message, unless you wrote after it
// (then it's working on yours, and its last reply is old news).
func latestReply(a *claude.Agent) *claude.Msg {
	msgs := claude.Messages(claude.TranscriptPath(a))
	for i := len(msgs) - 1; i >= 0; i-- {
		switch msgs[i].Role {
		case claude.RoleAgent:
			return &msgs[i]
		case claude.RoleYou:
			return nil
		}
	}
	return nil
}

// replyLines is the latest reply in full, for under the compact history:
// below a divider when there's history above it (after), else just its head.
func replyLines(msg *claude.Msg, w int, after bool) []string {
	const label = "latest reply · t: whole conversation"
	var out []string
	if after {
		rule := "── " + label + " "
		rule += strings.Repeat("─", max(0, w-runewidth.StringWidth(rule)))
		out = []string{"", stDim.Render(trunc(rule, w)), messageHead(*msg, true)}
	} else {
		out = []string{"", messageHead(*msg, true) + stDim.Render("   "+label)}
	}
	lines, _ := messageLines(*msg, max(10, w-2))
	for _, l := range lines {
		out = append(out, "  "+l)
	}
	return out
}

// maxMessages caps how far back the whole conversation goes in the panel.
const maxMessages = 300

type convKey struct {
	path string
	n, w int
}

var convCache struct {
	key   convKey
	lines []string
	top   int
}

// conversationLines is the whole conversation (t): your messages and the
// agent's replies, tool calls left out, oldest first, with day separators.
// top is where the agent's last message starts (0 if none). Cached until the
// transcript grows or the width changes.
func conversationLines(a *claude.Agent, w int) (lines []string, top int) {
	path := claude.TranscriptPath(a)
	msgs := claude.Messages(path)
	k := convKey{path, len(msgs), w}
	if convCache.key == k && convCache.lines != nil {
		return convCache.lines, convCache.top
	}
	var conv []claude.Msg
	for _, msg := range msgs {
		if msg.Role != claude.RoleTool {
			conv = append(conv, msg)
		}
	}
	conv = conv[max(0, len(conv)-maxMessages):]
	var out []string
	lastDay := ""
	for _, msg := range conv {
		if d := dayLabel(msg.At); d != lastDay {
			lastDay = d
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, stDim.Render(trunc("── "+d+" "+strings.Repeat("─", w), w)))
		}
		if msg.Role == claude.RoleAgent {
			top = len(out)
		}
		out = append(out, "", messageHead(msg, msg.Role == claude.RoleYou))
		var lines []string
		if msg.Role == claude.RoleYou {
			lines = yours(msg, max(10, w-2))
		} else {
			lines, _ = messageLines(msg, max(10, w-2))
		}
		for _, l := range lines {
			out = append(out, "  "+l)
		}
	}
	if len(out) == 0 {
		out, top = []string{stDim.Render("no conversation found")}, 0
	}
	convCache.key, convCache.lines, convCache.top = k, out, top
	return out, top
}
