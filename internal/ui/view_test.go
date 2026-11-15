package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

// TestViewFrames checks the screen layout: the bordered input sits at the
// bottom, every line fits the width, and ? draws the help modal over it.
// REGIA_SHOW=1 prints the frames.
func TestViewFrames(t *testing.T) {
	real := claude.CachedAgents() // the user's agents, read before the test redirects the cache dir
	m := testModel(t)
	if real != nil && os.Getenv("REGIA_SHOW") != "" {
		m.setAgents(real)
	}
	m.w, m.h = 100, 28
	check := func(name string) []string {
		ls := strings.Split(ansi.Strip(m.View()), "\n")
		if len(ls) != m.h {
			t.Fatalf("%s: %d lines, want %d", name, len(ls), m.h)
		}
		for i, l := range ls {
			if w := ansi.StringWidth(l); w > m.w {
				t.Fatalf("%s: line %d is %d wide: %q", name, i, w, l)
			}
		}
		if os.Getenv("REGIA_SHOW") != "" {
			t.Logf("── %s\n%s", name, strings.Join(ls, "\n"))
		}
		return ls
	}
	ls := check("typing")
	n := len(ls)
	if !strings.HasPrefix(ls[n-3], "╭") || !strings.HasPrefix(ls[n-1], "╰") || !strings.Contains(ls[n-2], "❯") || !strings.Contains(ls[n-1], "· ctrl+o ─╯") {
		t.Fatalf("input box not at the bottom:\n%s", strings.Join(ls[n-3:], "\n"))
	}
	typeText(m, "fix the flaky test")
	if ls := check("draft"); !strings.Contains(ls[n-2], "fix the flaky test") {
		t.Fatalf("draft not in box: %q", ls[n-2])
	}
	m.Update(tkey("esc"))
	m.Update(tkey("esc"))
	if ls := check("command"); !strings.Contains(ls[n-2], "i to type a new task") {
		t.Fatalf("command mode hint: %q", ls[n-2])
	}
	m.Update(tkey("?"))
	ls = check("help")
	if all := strings.Join(ls, "\n"); !strings.Contains(all, "esc / ? closes") || !strings.Contains(all, "remove group") || !strings.HasPrefix(ls[0], " regia") {
		t.Fatal("help modal not drawn over the UI")
	}
	m.Update(tkey("j")) // ignored while the modal is open
	m.Update(tkey("esc"))
	if m.help {
		t.Fatal("esc did not close the modal")
	}
}

// TestViewWidths: with the margins, no line may exceed the terminal width.
func TestViewWidths(t *testing.T) {
	m := testModel(t)
	for _, w := range []int{40, 69, 70, 90, 160, 240} {
		m.w, m.h = w, 24
		ls := strings.Split(ansi.Strip(m.View()), "\n")
		if len(ls) != m.h {
			t.Fatalf("w=%d: %d lines", w, len(ls))
		}
		for i, l := range ls {
			if ansi.StringWidth(l) > w {
				t.Fatalf("w=%d: line %d is %d wide: %q", w, i, ansi.StringWidth(l), l)
			}
		}
	}
}
