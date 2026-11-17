package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// regexHelpSections is the search view's cheat sheet (ctrl+r): how a query is
// read, then the Go regexp tricks worth knowing, each with an example you can
// type as is.
var regexHelpSections = []helpSection{
	{"the query", [][2]string{
		{"deploy failed", "both words, same message, any case"},
		{`"deploy failed"`, "the exact phrase"},
		{"/retr(y|ies)/", "a pattern (case already ignored)"},
		{"pr /#\\d+/", "words and patterns mix freely"},
		{"/a b/", "a space inside a pattern is fine"},
	}},
	{"match", [][2]string{
		{".", "any character"},
		{"\\d  \\w  \\s", "digit / word char / space"},
		{"[abc]  [^abc]", "one of / none of"},
		{"[0-9a-f]", "a range"},
		{"\\.  \\(  \\/", "a literal . ( /"},
	}},
	{"repeat", [][2]string{
		{"*  +  ?", "0+ / 1+ / 0 or 1 times"},
		{"{8}  {2,4}", "exactly 8 / 2 to 4 times"},
		{".*?", "as few as possible"},
	}},
	{"where", [][2]string{
		{"\\bword\\b", "a whole word, not inside one"},
		{"^  $", "start / end of the message"},
		{"(?m)^…$", "start / end of each line"},
		{"a|b  (a|b)c", "either / grouped"},
	}},
	{"handy", [][2]string{
		{"/\\b[0-9a-f]{8}\\b/", "an agent or job id"},
		{"/https?:\\/\\/\\S+/", "a URL"},
		{"/\\w+\\.go:\\d+/", "a file:line"},
		{"/(?-i)TODO/", "case-sensitive"},
		{"/fail(ed|ure)?/", "fail, failed, failure"},
	}},
}

// regexHelpModal renders the cheat sheet as a rounded box, like the key
// help: two columns when there is room, one otherwise (cut to the height).
func regexHelpModal(w, h int) string {
	ss := regexHelpSections
	left, right := renderSectionsW(ss[:2], 18), renderSectionsW(ss[2:], 21)
	lw, rw := maxWidth(left), maxWidth(right)
	var ls []string
	if w >= lw+rw+7 { // border 2 + padding 2 + gap 3
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
	} else {
		ls = renderSectionsW(ss, 21)
	}
	if len(ls) > h-6 && h > 8 {
		ls = append(ls[:h-8], stDim.Render("…  (taller window shows the rest)"))
	}
	title := stBold.Render("regex in search") + stDim.Render("   esc / ctrl+r closes")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(0, 1).MaxWidth(w).Render(title + "\n\n" + strings.Join(ls, "\n"))
}

func maxWidth(ls []string) int {
	n := 0
	for _, l := range ls {
		n = max(n, lipgloss.Width(l))
	}
	return n
}
