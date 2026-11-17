package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

const linkReply = "Fixed it in `/abs/x.go:12` and internal/ui/open.go, see https://example.com/a_(b) and ui.go:7.\n\n" +
	"- also ~/notes/todo.md, not and/or, e.g. or 1/2\n\n```\ncat /etc/hosts\n```"

// The links an agent reply gets with links on, as (text, uri) pairs.
func linksIn(lines []string) [][2]string {
	var out [][2]string
	for _, l := range lines {
		for {
			i := strings.Index(l, "\x1b]8;;")
			if i < 0 {
				break
			}
			l = l[i+5:]
			j := strings.Index(l, "\x1b\\")
			uri := l[:j]
			l = l[j+2:]
			if uri == "" {
				continue
			}
			k := strings.Index(l, "\x1b]8;;\x1b\\")
			out = append(out, [2]string{ansi.Strip(l[:k]), uri})
		}
	}
	return out
}

func TestAgentTextLinks(t *testing.T) {
	defer withOpeners(t, "preset = vscode\nosc8 = on")()
	got := linksIn(agentMarkdown(linkReply, 100, "/wt"))
	want := [][2]string{
		{"/abs/x.go:12", "vscode://file/abs/x.go:12"},
		{"internal/ui/open.go", "vscode://file/wt/internal/ui/open.go:1"},
		{"https://example.com/a_(b)", "https://example.com/a_(b)"},
		{"ui.go:7", "vscode://file/wt/ui.go:7"},
		{"~/notes/todo.md", "vscode://file" + absPath("~/notes/todo.md", "") + ":1"},
		{"/etc/hosts", "vscode://file/etc/hosts:1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("links:\n got %q\nwant %q", got, want)
	}
}

// With links off the reply is exactly what it was before links existed.
func TestAgentTextNoLinksWhenOff(t *testing.T) {
	defer withOpeners(t, "preset = vscode\nosc8 = off")()
	msg := &claude.Msg{Role: claude.RoleAgent, Text: linkReply}
	before := renderMarkdown(linkReply, 100)
	for len(before) > 0 && strings.TrimSpace(ansi.Strip(before[len(before)-1])) == "" {
		before = before[:len(before)-1]
	}
	after := agentLines(msg, 100, "/wt")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("links off changed the reply:\n%q\n%q", before, after)
	}
	if strings.Contains(strings.Join(after, ""), "\x1b]8") {
		t.Fatal("links off but OSC 8 in the output")
	}
}

// The panel cuts and wraps lines after they're drawn: a linked line must be
// as wide as the plain one, and cutting it must not leave half a sequence.
func TestLinkedLinesKeepWidthAndCutClean(t *testing.T) {
	plainLines := renderMarkdown(linkReply, 60)
	defer withOpeners(t, "preset = vscode\nosc8 = on")()
	linked := agentMarkdown(linkReply, 60, "/wt")
	if len(linked) != len(plainLines) {
		t.Fatalf("%d lines linked, %d plain", len(linked), len(plainLines))
	}
	n := 0
	for i, l := range linked {
		if l == plainLines[i] {
			continue
		}
		n++
		if ansi.StringWidth(l) != ansi.StringWidth(plainLines[i]) || ansi.Strip(l) != ansi.Strip(plainLines[i]) {
			t.Fatalf("line %d: width %d vs %d\n%q\n%q", i, ansi.StringWidth(l), ansi.StringWidth(plainLines[i]), l, plainLines[i])
		}
		for w := 1; w <= ansi.StringWidth(l); w++ {
			cut := ansi.Truncate(l, w, "…")
			if ansi.Strip(cut) != ansi.Strip(ansi.Truncate(plainLines[i], w, "…")) || !wholeEscapes(cut) {
				t.Fatalf("line %d cut at %d: %q", i, w, cut)
			}
		}
		for _, wl := range wrap(l, 20) {
			if !wholeEscapes(wl) || ansi.StringWidth(wl) > 20 {
				t.Fatalf("line %d wrapped badly: %q", i, wl)
			}
		}
		if got, want := ansi.Strip(strings.Join(wrap(l, 20), "")), ansi.Strip(strings.Join(wrap(plainLines[i], 20), "")); got != want {
			t.Fatalf("line %d wraps differently:\n%q\n%q", i, got, want)
		}
	}
	if n == 0 {
		t.Fatal("no line got a link")
	}
}

// wholeEscapes says every escape sequence in s is complete.
func wholeEscapes(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			continue
		}
		n := escLen(s[i:])
		seq := s[i : i+n]
		if seq[1] == ']' && !strings.HasSuffix(seq, "\x1b\\") && !strings.HasSuffix(seq, "\x07") {
			return false
		}
		i += n - 1
	}
	return true
}

// Links show up in the real conversation panel too, and the cached frame
// keeps them.
func TestConversationPanelLinks(t *testing.T) {
	defer withOpeners(t, "osc8 = on")()
	m := testModel(t)
	m.Update(tkey("esc"))
	goTo(t, m, "aaaa1111")
	giveTranscript(t, m, "aaaa1111", "where is it?", "It's in /srv/app/main.go:40 now.")
	a := m.agents["aaaa1111"]
	lines := m.conversationLines(a, 100)
	host := hostName()
	got := linksIn(lines)
	if len(got) != 1 || got[0] != [2]string{"/srv/app/main.go:40", "file://" + host + "/srv/app/main.go"} {
		t.Fatalf("links %q in:\n%s", got, strings.Join(lines, "\n"))
	}
	if again := m.conversationLines(a, 100); !reflect.DeepEqual(again, lines) {
		t.Fatal("the cached frame differs")
	}
}
