package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type helpSection struct {
	title string
	keys  [][2]string
}

var helpSections = []helpSection{
	{"input  (has focus at start)", [][2]string{
		{"type + enter", "start agent by the selection"},
		{"↑/↓ pgup/pgdn", "select (= where it goes)"},
		{"enter (empty)", "attach (← comes back)"},
		{"esc", "clear · again: command keys"},
		{"ctrl+w/u", "delete word / line"},
		{"ctrl+f", "filter"},
		{"ctrl+c", "quit (empty input)"},
		{"i  c  C", "type again (C: inside)"},
	}},
	{"navigate", [][2]string{
		{"j/k ↑/↓", "move"},
		{"g / G", "top / bottom"},
		{"ctrl+d/u", "half page"},
		{"b", "next agent waiting for you"},
		{"tab", "tree ⇄ inbox"},
		{"/  esc", "filter / clear"},
		{"←/→ h/l space", "fold / unfold"},
	}},
	{"sessions", [][2]string{
		{"enter", "attach"},
		{"o", "terminal output (logs)"},
		{"y / Y", "copy reply / session id"},
		{"s", "stop (conversation kept)"},
		{"a", "show / hide finished"},
		{"R", "refresh now"},
		{"q", "quit"},
	}},
	{"organize  (~/.config/regia/layout.json)", [][2]string{
		{"J / K", "move down / up (siblings)"},
		{"> / <", "nest under above / un-nest"},
		{"m then p / P", "move below / inside target"},
		{"n", "new group"},
		{"r", "rename (alias)"},
		{"d", "remove group"},
	}},
}

func renderSections(ss []helpSection) []string {
	var out []string
	for i, s := range ss {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, stHead.Render(s.title))
		for _, k := range s.keys {
			out = append(out, stAccent.Render(fmt.Sprintf("  %-15s", k[0]))+k[1])
		}
	}
	return out
}

// helpModal renders the key reference as a rounded box: two columns when
// the screen is wide enough, one otherwise (cut to the screen height).
func helpModal(w, h int) string {
	var body string
	left, right := renderSections(helpSections[:2]), renderSections(helpSections[2:])
	colW := 0
	for _, l := range append(append([]string{}, left...), right...) {
		colW = max(colW, lipgloss.Width(l))
	}
	if w >= 2*colW+10 {
		var ls []string
		for i := 0; i < max(len(left), len(right)); i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			ls = append(ls, pad(l, colW+4)+r)
		}
		body = strings.Join(ls, "\n")
	} else {
		ls := renderSections(helpSections)
		if len(ls) > h-6 && h > 8 {
			ls = append(ls[:h-8], stDim.Render("…  (taller window shows the rest)"))
		}
		body = strings.Join(ls, "\n")
	}
	title := stBold.Render("keys") + stDim.Render("   esc / ? closes")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(0, 2).MaxWidth(w).Render(title + "\n\n" + body)
}

// overlay draws box centred over base (both multi-line, possibly styled),
// keeping the base visible around it.
func overlay(base, box string, w, h int) string {
	bl := strings.Split(base, "\n")
	ol := strings.Split(box, "\n")
	bw := 0
	for _, l := range ol {
		bw = max(bw, ansi.StringWidth(l))
	}
	top := max(0, (len(bl)-len(ol))/2)
	left := max(0, (w-bw)/2)
	for i, l := range ol {
		y := top + i
		if y >= len(bl) {
			break
		}
		row := bl[y]
		if pw := ansi.StringWidth(row); pw < left+bw {
			row += strings.Repeat(" ", left+bw-pw)
		}
		l += strings.Repeat(" ", bw-ansi.StringWidth(l))
		bl[y] = ansi.Truncate(row, left, "") + "\x1b[0m" + l + "\x1b[0m" + ansi.TruncateLeft(row, left+bw, "")
	}
	return strings.Join(bl, "\n")
}
