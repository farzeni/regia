package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

func TestRegexHelpOpensInSearch(t *testing.T) {
	m := searchModel(t)
	m.setView(viewSearch)
	typeQuery(m, "pay")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	all := ansi.Strip(m.View())
	if !strings.Contains(all, "regex in search") || !strings.Contains(all, `\bword\b`) {
		t.Fatalf("ctrl+r should show the regex cheat sheet:\n%s", all)
	}
	m.Update(tkey("x")) // typing while it's open doesn't reach the query
	if m.srch.query != "pay" {
		t.Fatalf("query changed under the cheat sheet: %q", m.srch.query)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	all = ansi.Strip(m.View())
	if strings.Contains(all, "regex in search") || m.srch.query != "pay" || m.view != viewSearch {
		t.Fatalf("esc should close the cheat sheet and keep the query %q:\n%s", m.srch.query, all)
	}
}

func TestRegexHelpFitsTheScreen(t *testing.T) {
	for _, w := range []int{40, 70, 140} {
		for _, l := range strings.Split(ansi.Strip(regexHelpModal(w, 40)), "\n") {
			if ansi.StringWidth(l) > w {
				t.Fatalf("width %d: line wider than the screen: %q", w, l)
			}
		}
	}
}

// TestRegexHelpExamplesParse: every full query the sheet shows (the ones
// you'd type as is) is a valid query.
func TestRegexHelpExamplesParse(t *testing.T) {
	for _, s := range regexHelpSections {
		if s.title != "the query" && s.title != "handy" {
			continue
		}
		for _, k := range s.keys {
			if q := claude.ParseQuery(k[0]); q.Err != nil || q.Empty() {
				t.Errorf("%s: %v", k[0], q.Err)
			}
		}
	}
}
