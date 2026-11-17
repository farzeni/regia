package ui

import (
	"strings"
	"time"

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

// maxMessages caps how far back the conversation goes in the panel.
const maxMessages = 300

type convKey struct {
	path, day string
	n, w      int
}

// convCache keeps the last few agents' rendered conversations: the panel is
// drawn 8 times a second while an agent works, and going back and forth
// between a few agents shouldn't render their markdown again.
var convCache [4]struct {
	key   convKey
	lines []string
}
var convNext int

// conversationLines is the agent panel's body: your messages and the agent's
// replies, tool calls left out, oldest first, with day separators. Cached
// until the transcript grows or the width or the day changes.
func conversationLines(a *claude.Agent, w int) []string {
	path := claude.TranscriptPath(a)
	msgs := claude.Messages(path)
	n := 0 // messages shown: a busy agent's tool calls don't render it again
	for _, msg := range msgs {
		if msg.Role != claude.RoleTool {
			n++
		}
	}
	k := convKey{path, time.Now().Format(time.DateOnly), n, w}
	slot := -1
	for i, c := range convCache {
		if c.key == k && c.lines != nil {
			return c.lines
		}
		if c.key.path == path && c.key.w == w {
			slot = i // this agent's stale render: replace it
		}
	}
	conv := make([]claude.Msg, 0, n)
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
		out = []string{stDim.Render("no conversation yet")}
	}
	if slot < 0 {
		slot, convNext = convNext, (convNext+1)%len(convCache)
	}
	convCache[slot].key, convCache[slot].lines = k, out
	return out
}
