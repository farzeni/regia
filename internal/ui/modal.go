package ui

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
	{"input  (i, c, C)", [][2]string{
		{"type + enter", "start agent by the selection"},
		{"alt+⏎  ctrl+j", "new line (or \\ then ⏎)"},
		{"paste", "long: [Pasted text #n]"},
		{"↑/↓  ctrl+↑/↓", "select (= where it goes)"},
		{"enter (empty)", "attach (← comes back)"},
		{"esc", "clear · leave the input · quit"},
		{"alt+⌫ ctrl+w", "delete word (alt+d: next)"},
		{"ctrl+a/e alt+←→", "line start, end / by word"},
		{"ctrl+k / ctrl+u", "cut to end / start"},
	}},
	{"navigate", [][2]string{
		{"j/k ↑/↓", "move"},
		{"[ ]  ctrl+↑/↓", "previous / next group"},
		{"g G  ctrl+d/u", "top, bottom / half page"},
		{"pgup / pgdn", "scroll the panel's history"},
		{"b", "next agent waiting for you"},
		{"tab", "tree ⇄ inbox"},
		{"/ ctrl+f  esc", "filter / clear"},
		{"←/→ h/l space", "fold / unfold"},
		{"z", "fold all / unfold all"},
		{"- / +  drag │", "list narrower / wider"},
		{"R", "refresh now"},
	}},
	{"sessions", [][2]string{
		{"enter", "attach"},
		{"o", "terminal output (logs)"},
		{"y / Y", "copy reply / session id"},
		{"s", "stop (conversation kept)"},
		{"x", "remove from list (claude rm)"},
		{"a", "show / hide finished"},
		{"f", "pin (stays when done) / unpin"},
		{"e  A", "archive (group: all) / show it"},
		{"v", "select: space *, then e x m n f"},
		{"esc  ctrl+c", "quit (with nothing to clear)"},
	}},
	{"organize  (~/.config/regia/layout.json)", [][2]string{
		{"J / K", "move down / up (siblings)"},
		{"> / <", "nest under above / un-nest"},
		{"m then p", "into group / after agent"},
		{"m then P", "after group / under agent"},
		{"n  N  M", "new group · notes · mute"},
		{"r / d", "rename / remove group"},
		{"w", "group's dir for new agents"},
	}},
	{"in the draft", [][2]string{
		{"/name  @path", "skill / file (tab, ⏎ picks)"},
		{"ctrl+v", "image, files: [Image #n]"},
		{"ctrl+o", "pick its directory"},
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
			out = append(out, stAccent.Render(fmt.Sprintf("  %-16s", k[0]))+k[1])
		}
	}
	return out
}

// helpModal renders the key reference as a rounded box: two columns when
// the screen is wide enough, one otherwise (cut to the screen height).
func helpModal(w, h int) string {
	var body string
	// input + sessions | navigate + organize: the two columns come out even
	hs := helpSections
	left, right := renderSections([]helpSection{hs[0], hs[2]}), renderSections([]helpSection{hs[1], hs[3]})
	lw, rw := 0, 0
	for _, l := range left {
		lw = max(lw, lipgloss.Width(l))
	}
	for _, l := range right {
		rw = max(rw, lipgloss.Width(l))
	}
	if w >= lw+rw+7 { // border 2 + padding 2 + gap 3
		var ls []string
		for i := 0; i < max(len(left), len(right)); i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			ls = append(ls, pad(l, lw+3)+r)
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
		Padding(0, 1).MaxWidth(w).Render(title + "\n\n" + body)
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
