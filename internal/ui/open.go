package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// openTarget is what enter (or a click) opens from the conversation or the
// changes tab: a file at a line, an image, a URL, text in the pager, or a
// worktree's diff.
type openTarget struct {
	Kind string // "file", "image", "url", "pager", "diff"
	Path string // file or image path
	Line int    // file line, 0 when unknown
	URL  string
	Text string // what the pager shows
	Dir  string // the agent's worktree, for "diff"
}

// openDoneMsg reports an opener that failed, to flash it.
type openDoneMsg struct{ err error }

// openThing opens t with the user's tools. A replaceable var: tests swap it,
// and the openers config decides what really runs.
var openThing = func(t openTarget) tea.Cmd {
	switch t.Kind {
	case "pager", "diff":
		text := t.Text
		if t.Kind == "diff" && text == "" {
			out, _ := exec.Command("git", "-C", t.Dir, "diff").Output()
			text = string(out)
		}
		pager := os.Getenv("PAGER")
		if pager == "" {
			pager = "less -R"
		}
		f := strings.Fields(pager)
		c := exec.Command(f[0], f[1:]...)
		c.Stdin = strings.NewReader(text)
		return tea.ExecProcess(c, func(err error) tea.Msg { return openDoneMsg{err} })
	}
	target := t.Path
	if t.Kind == "url" {
		target = t.URL
	}
	open := "xdg-open"
	if runtime.GOOS == "darwin" {
		open = "open"
	}
	return func() tea.Msg { return openDoneMsg{exec.Command(open, target).Start()} }
}

// linkify makes text a clickable link to t where the terminal supports it;
// until then it returns text unchanged.
var linkify = func(text string, t openTarget) string { return text }
