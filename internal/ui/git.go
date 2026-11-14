package ui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/gitinfo"
)

type gitMsg struct{ dir string }

// gitCmd refreshes dir's git info in the background when it's stale.
func gitCmd(dir string) tea.Cmd {
	f := gitinfo.Refresher(dir)
	if f == nil {
		return nil
	}
	return func() tea.Msg {
		f()
		return gitMsg{dir}
	}
}

// gitLines renders the card's GIT field: branch line + last commit line.
func gitLines(g gitinfo.Info) (string, string) {
	if !g.OK {
		return "", ""
	}
	b := g.Branch
	if b == "" {
		b = "(detached " + g.Head + ")"
	}
	parts := []string{"⎇ " + b}
	if g.Worktree {
		parts = append(parts, "worktree")
	}
	switch {
	case g.Upstream == "":
		parts = append(parts, "no upstream")
	case g.Ahead == 0 && g.Behind == 0:
		parts = append(parts, "in sync")
	default:
		ab := ""
		if g.Ahead > 0 {
			ab += "↑" + strconv.Itoa(g.Ahead)
		}
		if g.Behind > 0 {
			ab += "↓" + strconv.Itoa(g.Behind)
		}
		parts = append(parts, ab)
	}
	switch {
	case g.Changed == 0 && g.Untracked == 0:
		parts = append(parts, "clean")
	default:
		var d []string
		if g.Changed > 0 {
			d = append(d, strconv.Itoa(g.Changed)+" changed")
		}
		if g.Untracked > 0 {
			d = append(d, strconv.Itoa(g.Untracked)+" untracked")
		}
		parts = append(parts, strings.Join(d, ", "))
	}
	last := ""
	if g.Head != "" {
		last = g.Head + " " + g.Subject + " · " + ago(g.CommitAt) + " ago"
	}
	return strings.Join(parts, " · "), last
}
