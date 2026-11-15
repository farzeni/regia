package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func withColors(t *testing.T) {
	p := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(p) })
	mdRenderers, mdCache = map[int]*glamour.TermRenderer{}, map[mdKey][]string{}
}

func TestRenderMarkdown(t *testing.T) {
	withColors(t)
	md := "Card **#61** merged, `make check` passes.\n\n" +
		"- turning errors into lines;\n- making sure Ctrl-C releases the lock;\n\n" +
		"| In flight | Session |\n|---|---|\n| #62 `skl init` | impl-62 |\n| #87 gitx | impl-87 |\n"
	lines := renderMarkdown(md, 60)
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
		if w := ansi.StringWidth(l); w > 60 {
			t.Errorf("line %d is %d wide: %q", i, w, plain[i])
		}
		if strings.HasSuffix(plain[i], " ") {
			t.Errorf("line %d keeps its padding: %q", i, plain[i])
		}
	}
	out := strings.Join(plain, "\n")
	for _, want := range []string{"Card #61 merged", "• turning errors into lines;", "In flight", "impl-62"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered markdown lacks %q:\n%s", want, out)
		}
	}
	for _, raw := range []string{"**", "|---|", "- turning"} {
		if strings.Contains(out, raw) {
			t.Errorf("raw markdown %q left in:\n%s", raw, out)
		}
	}
	if strings.TrimSpace(plain[0]) == "" || strings.TrimSpace(plain[len(plain)-1]) == "" {
		t.Errorf("blank first or last line:\n%s", out)
	}
}

func TestHighlightStyled(t *testing.T) {
	withColors(t)
	bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	line := "see " + bold.Render("the Lock file") + " and lock again"
	got := highlightStyled(line, parseQ("lock"))
	if ansi.Strip(got) != ansi.Strip(line) {
		t.Fatalf("text changed: %q", ansi.Strip(got))
	}
	on := sgrPrefix(stMatch)
	if n := strings.Count(got, on); n < 2 {
		t.Fatalf("expected both matches highlighted, %d in %q", n, got)
	}
	// the match inside the bold part: the highlight comes after bold's own
	// codes, and bold is restored after it
	i := strings.Index(got, "Lock")
	if j := strings.LastIndex(got[:i], on); j < 0 || strings.Contains(got[j+len(on):i], "\x1b[") {
		t.Fatalf("highlight not in effect on Lock: %q", got)
	}
	if after := got[i+len("Lock"):]; !strings.HasPrefix(after, "\x1b[0m"+sgrPrefix(bold)) {
		t.Fatalf("bold not restored after the match: %q", after)
	}
	// nothing to highlight: untouched
	if highlightStyled(line, parseQ("zzz")) != line {
		t.Fatal("line changed without a match")
	}
}

func TestTrimStyledRight(t *testing.T) {
	s := "\x1b[1mhi\x1b[0m   \x1b[0m"
	if got := trimStyledRight(s); got != "\x1b[1mhi\x1b[0m\x1b[0m" {
		t.Fatalf("trimStyledRight = %q", got)
	}
	if got := trimStyledRight("plain"); got != "plain" {
		t.Fatalf("%q", got)
	}
}

func TestMdPlain(t *testing.T) {
	md := "## Done\n**#62**, `skl init`, has started:\n- one;\n> quoted\n\n| In flight | Session |\n|---|:---:|\n| #62 | impl-62 |"
	got := strings.Join(strings.Fields(mdPlain(md)), " ")
	want := "Done #62, skl init, has started: one; quoted In flight · Session #62 · impl-62"
	if got != want {
		t.Fatalf("mdPlain:\n got %q\nwant %q", got, want)
	}
}
