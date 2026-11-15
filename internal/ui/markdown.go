package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	gansi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

// Agent replies are markdown: the search panel renders them with glamour
// (what glow uses), in its dark style without the page margins, so
// headings, lists, tables and code read as they were meant to.

var (
	mdRenderers = map[int]*glamour.TermRenderer{} // by width
	mdCache     = map[mdKey][]string{}
)

type mdKey struct {
	text string
	w    int
}

const mdCacheMax = 512

func mdStyle() gansi.StyleConfig {
	st := styles.DarkStyleConfig
	zero := uint(0)
	st.Document.Margin = &zero
	st.Document.BlockPrefix, st.Document.BlockSuffix = "", ""
	st.Document.Color = nil // the terminal's own foreground, like the rest of regia
	st.CodeBlock.Margin = &zero
	return st
}

// renderMarkdown renders md to lines at most w wide (styled; trailing padding
// trimmed, leading and trailing blank lines dropped). Results are cached:
// the panel is redrawn on every key and tick.
func renderMarkdown(md string, w int) []string {
	k := mdKey{md, w}
	if ls, ok := mdCache[k]; ok {
		return ls
	}
	r := mdRenderers[w]
	if r == nil {
		var err error
		r, err = glamour.NewTermRenderer(
			glamour.WithStyles(mdStyle()),
			glamour.WithWordWrap(w),
			glamour.WithColorProfile(lipgloss.ColorProfile()),
		)
		if err != nil {
			return wrap(md, w)
		}
		mdRenderers[w] = r
	}
	out, err := r.Render(md)
	if err != nil {
		return wrap(md, w)
	}
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = trimStyledRight(l)
	}
	for len(lines) > 0 && ansi.Strip(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && ansi.Strip(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(mdCache) >= mdCacheMax {
		mdCache = map[mdKey][]string{}
	}
	mdCache[k] = lines
	return lines
}

var (
	mdMarks = strings.NewReplacer("**", "", "__", "", "`", "")
	mdBlock = regexp.MustCompile(`(?m)^\s*(#{1,6}\s+|[-*+]\s+|>\s?|\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$)`)
	mdPipes = regexp.MustCompile(`\s*\|\s*`)
)

// mdPlain strips the markdown syntax a one-line snippet would show raw:
// emphasis marks, backticks, heading, list and quote markers, table rules
// and pipes.
func mdPlain(s string) string {
	lines := strings.Split(mdBlock.ReplaceAllString(s, ""), "\n")
	for i, l := range lines {
		lines[i] = strings.Trim(mdPipes.ReplaceAllString(l, " · "), " ·")
	}
	return mdMarks.Replace(strings.Join(lines, "\n"))
}

// trimStyledRight drops the spaces glamour pads each line with, keeping the
// escape sequences (so styles still end where they should).
func trimStyledRight(s string) string {
	if strings.TrimRight(ansi.Strip(s), " ") == ansi.Strip(s) {
		return s
	}
	keep := len(strings.TrimRight(ansi.Strip(s), " ")) // visible bytes to keep
	var b strings.Builder
	vis := 0
	for i := 0; i < len(s); {
		if n := escLen(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		if vis < keep {
			b.WriteByte(s[i])
		}
		vis++
		i++
	}
	return b.String()
}

// escLen is the length of the escape sequence s starts with (CSI, or OSC as
// in hyperlinks), 0 if it doesn't start with one.
func escLen(s string) int {
	if len(s) < 2 || s[0] != 0x1b {
		return 0
	}
	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	case ']':
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	}
	return 2
}

// highlightStyled marks the matches of q in an already styled line: the
// matches are found in its visible text, and the highlight is drawn over
// whatever styles the line has there (restored after each match).
func highlightStyled(line string, q claude.Query) string {
	plain := ansi.Strip(line)
	spans := q.Spans(plain)
	if len(spans) == 0 {
		return line
	}
	on := sgrPrefix(stMatch)
	if on == "" { // no colors
		return line
	}
	var b strings.Builder
	var active []string // SGR sequences in effect, to restore after a match
	vis, si, inside := 0, 0, false
	for i := 0; i < len(line); {
		if !inside && si < len(spans) && vis == spans[si][0] {
			b.WriteString(on)
			inside = true
		}
		if inside && vis == spans[si][1] {
			b.WriteString("\x1b[0m" + strings.Join(active, ""))
			inside = false
			si++
			continue
		}
		if n := escLen(line[i:]); n > 0 {
			seq := line[i : i+n]
			b.WriteString(seq)
			if strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
				if seq == "\x1b[0m" || seq == "\x1b[m" {
					active = active[:0]
				} else {
					active = append(active, seq)
				}
				if inside { // the line's own style must not cover the highlight
					b.WriteString(on)
				}
			}
			i += n
			continue
		}
		b.WriteByte(line[i])
		vis++
		i++
	}
	if inside {
		b.WriteString("\x1b[0m" + strings.Join(active, ""))
	}
	return b.String()
}

// sgrPrefix is the escape sequence a style opens with ("" without colors).
func sgrPrefix(st lipgloss.Style) string {
	r := st.Render("x")
	return r[:strings.Index(r, "x")]
}
