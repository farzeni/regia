package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/claude"
)

// clip keeps at most n lines (n <= 0: all), marking the cut with "…".
func clip(lines []string, n, w int) []string {
	if n > 0 && len(lines) > n {
		lines = append([]string{}, lines[:n]...)
		lines[n-1] = trunc(strings.TrimRight(lines[n-1], " ")+" …", w)
	}
	return lines
}

var mdNoise = strings.NewReplacer("**", "", "__", "", "`", "")

// taskTitle is the first meaningful line of the task message, without
// markdown: a brief that opens with "# Title" shows just the title.
func taskTitle(raw string) string {
	raw = claude.PasteRe.ReplaceAllString(raw, "[pasted] ")
	for _, l := range strings.Split(raw, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#>-* "))
		if l = strings.TrimSpace(mdNoise.Replace(l)); l != "" {
			return l
		}
	}
	return ""
}

// needsLines lays out what the agent needs from you. A list run together
// with ";" ("Decide: a; b; c") becomes a heading and one bullet per item.
func needsLines(s string, w int) []string {
	s = mdNoise.Replace(claude.Tidy(s))
	w = max(10, w)
	// items: split on ";" and after each "? " (a run of questions)
	var items []string
	for _, p := range strings.Split(s, ";") {
		for _, q := range strings.SplitAfter(p, "? ") {
			if q = strings.TrimSpace(q); q != "" {
				items = append(items, q)
			}
		}
	}
	if len(items) < 2 {
		return clip(wrap(s, w), 8, w)
	}
	var out []string
	if i := strings.Index(items[0], ": "); i > 0 && i < 40 {
		out = append(out, lipgloss.NewStyle().Bold(true).Render(items[0][:i+1]))
		items[0] = strings.TrimSpace(items[0][i+2:])
	}
	for _, it := range items {
		for j, l := range clip(wrap(it, w-2), 3, w-2) {
			if j == 0 {
				out = append(out, "• "+l)
			} else {
				out = append(out, "  "+l)
			}
		}
	}
	return clip(out, 12, w)
}
