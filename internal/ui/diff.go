package ui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// Diff lines as the panel draws them (an edit step's patch). The agent's whole
// diff opens in your own tools from the changes tab (changes.go).

var (
	stAdd  = lipgloss.NewStyle().Foreground(cBusy)
	stDel  = lipgloss.NewStyle().Foreground(cFailed)
	stHunk = lipgloss.NewStyle().Foreground(cAccent)
)

// bodyLine colors a line of a unified diff, cut to w.
func bodyLine(l string, w int) string {
	t := trunc(l, w)
	switch {
	case strings.HasPrefix(l, "diff --git "):
		return stBold.Render(t)
	case strings.HasPrefix(l, "+++ "), strings.HasPrefix(l, "--- "),
		strings.HasPrefix(l, "index "), strings.HasPrefix(l, "new file"), strings.HasPrefix(l, "deleted file"),
		strings.HasPrefix(l, "similarity "), strings.HasPrefix(l, "rename "), strings.HasPrefix(l, "old mode"),
		strings.HasPrefix(l, "new mode"), strings.HasPrefix(l, "Binary files"), strings.HasPrefix(l, `\ No newline`):
		return stDim.Render(t)
	case strings.HasPrefix(l, "@@"):
		return stHunk.Render(t)
	case strings.HasPrefix(l, "+"):
		return stAdd.Render(t)
	case strings.HasPrefix(l, "-"):
		return stDel.Render(t)
	}
	return t
}

// clean makes a line of someone's file safe to draw: tabs become spaces and
// control characters (escape sequences, \r) are dropped.
func clean(l string) string {
	if !strings.ContainsFunc(l, func(r rune) bool { return r == '\t' || unicode.IsControl(r) }) {
		return l
	}
	var b strings.Builder
	for _, r := range l {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
