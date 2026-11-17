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

// helpSections, in two even columns: sessions + organize + changes | navigate +
// conversation + new agent.
// Text editing in the input box (the usual readline keys) is left out.
var helpSections = []helpSection{
	{"sessions", [][2]string{
		{"enter", "attach (detach to come back)"},
		{"D", "changes tab: files it touched"},
		{"!", "plugin actions and links"},
		{"y / Y", "copy reply / session id"},
		{"s", "stop (conversation kept)"},
		{"x", "remove (claude rm)"},
		{"e / A", "archive / show archived"},
		{"a", "show / hide finished"},
		{"f", "pin (stays when done) / unpin"},
		{"v", "select several (space, *)"},
		{"⚠ (dim)", "looks stuck: why is on the card"},
	}},
	{"organize", [][2]string{
		{"J / K", "move down / up"},
		{"> / <", "nest under above / un-nest"},
		{"m, then p", "into group / after agent"},
		{"m, then P", "the other way round"},
		{"n  r  d", "new / rename / remove group"},
		{"N  M", "group notes / mute"},
		{"T", "group timeline / blackboard"},
		{"w", "group's dir for new agents"},
	}},
	{"D: the changes tab", [][2]string{
		{"j/k  enter", "move / open the file at its line"},
		{"d / esc", "full diff in your tool / back"},
	}},
	{"navigate", [][2]string{
		{"j/k  ↑/↓", "move"},
		{"[ ]  ctrl+↑/↓", "previous / next group"},
		{"g / G", "top / bottom"},
		{"ctrl+d / u", "half page down / up"},
		{"pgup / pgdn", "scroll the conversation"},
		{", / .", "agent box: previous / next tab"},
		{"b", "next agent waiting for you"},
		{"tab / shift+tab", "tree → inbox → search"},
		{"Z", "snooze a question"},
		{"/", "filter: ↑↓ pick, enter opens, esc clears"},
		{"h/l  space  z", "fold / unfold / all"},
		{"- / +", "list narrower / wider"},
		{"R", "refresh"},
	}},
	{"o / →: focus the conversation", [][2]string{
		{"↑↓  ctrl+↑↓", "message to message / your messages"},
		{"shift+↑↓ j/k  ctrl+d/u", "every block, steps too / half page"},
		{"enter / space", "fold, unfold, open a step; or click"},
		{"c r i  ← esc", "copy / quote in the input / input / back"},
	}},
	{"new agent", [][2]string{
		{"type + enter", "start it at the selection"},
		{"/name  @path", "skill / file (tab completes)"},
		{"ctrl+v", "paste image or files"},
		{"ctrl+o", "pick its directory"},
		{"ctrl+g", "pick its model"},
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
	hs := helpSections
	left, right := renderSections(hs[:3]), renderSections(hs[3:])
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
	title := stBold.Render("keys") + stDim.Render("   esc / ? closes · ctrl+c quits")
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
