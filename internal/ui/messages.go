package ui

import (
	"strings"

	"github.com/farzeni/regia/internal/claude"
)

// Messages as the search panel shows them: a head line (who, when), then the
// message, the agent's markdown rendered. (The agent panel's conversation is
// in conv.go.)

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
