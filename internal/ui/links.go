package ui

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// In the agent's replies, URLs and file paths become OSC 8 links when the
// terminal shows them (openers.conf osc8): ctrl+click (or a plain click,
// depending on the terminal) opens the URL in the browser and the file in
// the editor the preset's `link` names. With links off nothing changes.

var (
	urlRe = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)
	// pathRe finds, after a space, a bracket or a quote: absolute paths
	// (/x/y, ~/x), relative ones with a folder and an extension
	// (internal/ui/x.go), and bare files with a line (x.go:12). Each may
	// end in :line or :line:col. A word with a slash ("and/or") isn't one.
	pathRe = regexp.MustCompile(`(?:^|[\s(\[{"'` + "`" + `=,])(` +
		`(?:~?/[\w.\-@+%]+(?:/[\w.\-@+%]+)*/?` +
		`|[\w.\-@+]+(?:/[\w.\-@+]+)*/[\w\-@+]*(?:\.[\w\-@+]+)*\.[A-Za-z]\w*` +
		`|[\w\-@+]+(?:\.[\w\-]+)*\.[A-Za-z]\w*` +
		`)(?::(\d+))?(?::\d+)?)`)
)

type linkSpan struct {
	from, to int // byte offsets in the line's visible text
	uri      string
}

type linkKey struct {
	text, dir string
	w         int
}

// linkCache keeps the linked replies: the conversation is laid out again on
// every new step, and the markdown cache alone would leave the link pass
// running over every reply each time.
var linkCache = map[linkKey][]string{}

// agentMarkdown is an agent reply rendered w wide, with links to its URLs
// and paths (relative ones are in dir, the agent's worktree) when links are
// on; renderMarkdown's own lines when they're off.
func agentMarkdown(text string, w int, dir string) []string {
	lines := renderMarkdown(text, w)
	c := theOpeners()
	if !c.links {
		return lines
	}
	k := linkKey{text, dir, w}
	if ls, ok := linkCache[k]; ok {
		return ls
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = linkLine(c, l, dir)
	}
	if len(linkCache) >= mdCacheMax {
		clear(linkCache)
	}
	linkCache[k] = out
	return out
}

// linkLine adds links to one styled line, keeping its styles and width.
func linkLine(c *openerConf, line, dir string) string {
	// most lines have no path or URL: skip them before stripping and the regexps
	if !strings.ContainsAny(line, "/:") || strings.Contains(line, "\x1b]8;") { // or are already linked
		return line
	}
	spans := findLinks(c, ansi.Strip(line), dir)
	if len(spans) == 0 {
		return line
	}
	var b strings.Builder
	vis, si, in := 0, 0, false
	for i := 0; i <= len(line); {
		if in && vis == spans[si].to {
			b.WriteString("\x1b]8;;\x1b\\")
			si, in = si+1, false
			continue
		}
		if !in && si < len(spans) && vis == spans[si].from {
			b.WriteString("\x1b]8;;" + spans[si].uri + "\x1b\\")
			in = true
		}
		if i == len(line) {
			break
		}
		if n := escLen(line[i:]); n > 0 {
			b.WriteString(line[i : i+n])
			i += n
			continue
		}
		b.WriteByte(line[i])
		vis++
		i++
	}
	return b.String()
}

// findLinks finds the URLs and paths in plain text, in order, not
// overlapping.
func findLinks(c *openerConf, plain, dir string) []linkSpan {
	if !strings.ContainsAny(plain, "/:") {
		return nil
	}
	var spans []linkSpan
	for _, m := range urlRe.FindAllStringIndex(plain, -1) {
		u := trimURL(plain[m[0]:m[1]])
		spans = append(spans, linkSpan{m[0], m[0] + len(u), u})
	}
	for _, m := range pathRe.FindAllStringSubmatchIndex(plain, -1) {
		from, to := m[2], m[3]
		if overlaps(spans, from, to) {
			continue
		}
		p := plain[from:to]
		line := 0
		if m[4] >= 0 {
			line, _ = strconv.Atoi(plain[m[4]:m[5]])
			p = plain[from : m[4]-1]
		} else if t := strings.TrimRight(p, ".,;:"); t != p { // the end of a sentence
			p, to = t, from+len(t)
		}
		if p == "/" || p == "~/" {
			continue
		}
		if line == 0 && !strings.Contains(p, "/") { // a bare file only with a line: x.go:12
			continue
		}
		uri := c.linkURI(openTarget{Kind: "file", Path: p, Line: line, Dir: dir})
		if uri != "" {
			spans = append(spans, linkSpan{from, to, uri})
		}
	}
	slices.SortFunc(spans, func(a, b linkSpan) int { return a.from - b.from })
	return spans
}

// trimURL drops what ends a sentence after a URL: punctuation, and a
// closing bracket the URL didn't open.
func trimURL(u string) string {
	for {
		t := strings.TrimRight(u, ".,;:!?'\"")
		if strings.HasSuffix(t, ")") && strings.Count(t, "(") < strings.Count(t, ")") ||
			strings.HasSuffix(t, "]") && strings.Count(t, "[") < strings.Count(t, "]") {
			t = t[:len(t)-1]
		}
		if t == u {
			return u
		}
		u = t
	}
}

func overlaps(spans []linkSpan, from, to int) bool {
	for _, s := range spans {
		if from < s.to && s.from < to {
			return true
		}
	}
	return false
}
