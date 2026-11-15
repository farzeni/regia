package ui

import (
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/gitinfo"
	"github.com/farzeni/regia/internal/prinfo"
)

// PR status on the card and in the inbox. The PR is the last "pr" link in the
// agent's state.json, or else whatever PR its branch has (`gh pr view
// <branch>` in its directory). gh runs in tea.Cmds, cached by prinfo (60 s
// for open PRs, never again once merged or closed); without gh, or not
// logged in, nothing is shown.

type prMsg struct{}

// prRefsPerTick caps how many gh calls one tick may start.
const prRefsPerTick = 4

// prRef is where and what to ask gh about for a, or "" when there's nothing.
func prRef(a *claude.Agent) (dir, ref string) {
	if a == nil || a.Interactive {
		return "", ""
	}
	dir = a.Cwd
	if dir != "" {
		if _, err := os.Stat(dir); err != nil {
			dir = "" // worktree removed: a URL still works from anywhere
		}
	}
	for i := len(a.PRs) - 1; i >= 0; i-- {
		if p := a.PRs[i]; p.Kind == "pr" && (p.Href != "" || p.ID != "") {
			ref = p.Href
			if ref == "" {
				ref = p.ID
				if dir == "" {
					return "", ""
				}
			}
			if dir == "" {
				dir, _ = os.UserHomeDir()
			}
			return dir, ref
		}
	}
	if dir == "" {
		return "", ""
	}
	b := a.Branch
	if gi, ok := gitinfo.Cached(a.Cwd); ok && gi.OK && gi.Branch != "" {
		b = gi.Branch
	}
	switch b {
	case "", "main", "master", "develop", "trunk":
		return "", "" // the default branch has no PR of its own
	}
	return dir, b
}

// prFor is a's cached PR status (zero when unknown or none).
func prFor(a *claude.Agent) prinfo.Info {
	dir, ref := prRef(a)
	if ref == "" {
		return prinfo.Info{}
	}
	i, _ := prinfo.Cached(dir, ref)
	return i
}

// prNeedsLook: a finished agent whose PR has failing CI or changes requested.
func prNeedsLook(a *claude.Agent) bool {
	if a == nil || !(a.State == "ready" || a.State == "done") {
		return false
	}
	return prFor(a).NeedsLook()
}

// needsLook: listed in the inbox although not waiting on you: its PR needs a
// look, or a plugin says so (attention).
func needsLook(a *claude.Agent) bool {
	if prNeedsLook(a) {
		return true
	}
	why, _ := pluginAttention(a)
	return why != ""
}

// prCmd refreshes a's PR in the background when it's due.
func prCmd(a *claude.Agent) tea.Cmd {
	dir, ref := prRef(a)
	f := prinfo.Refresher(dir, ref)
	if f == nil {
		return nil
	}
	return func() tea.Msg { f(); return prMsg{} }
}

// prCmds refreshes the PRs of finished, listed agents (a few per tick), so
// the inbox knows about failing CI without the agent being selected.
func (m *model) prCmds() tea.Cmd {
	var cmds []tea.Cmd
	now := time.Now()
	for _, a := range m.agents {
		if len(cmds) >= prRefsPerTick {
			break
		}
		if !(a.State == "ready" || a.State == "done") || m.layout.IsArchived(a.ID) {
			continue
		}
		if faded(a, now) && len(a.PRs) == 0 {
			continue // finished before today, no PR link: don't go asking about its branch
		}
		if c := prCmd(a); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

// prLine renders the card's PR field, e.g. "#12 open · CI ✗ failing · changes requested".
func prLine(i prinfo.Info) (string, lipgloss.Style) {
	if !i.OK {
		return "", stDim
	}
	parts := []string{"#" + strconv.Itoa(i.Number)}
	st := lipgloss.NewStyle()
	switch i.State {
	case "merged":
		parts = append(parts, "✓ merged")
		st = lipgloss.NewStyle().Foreground(cReady).Bold(true)
	case "closed":
		parts = append(parts, "closed, not merged")
		st = stDim
	default:
		parts = append(parts, i.State)
		switch i.Checks {
		case "passing":
			parts = append(parts, "CI ✓ passing")
		case "failing":
			parts = append(parts, "CI ✗ failing")
		case "pending":
			parts = append(parts, "CI … pending")
		}
		switch i.Review {
		case "approved":
			parts = append(parts, "approved")
		case "changes":
			parts = append(parts, "changes requested")
		case "requested":
			parts = append(parts, "review requested")
		}
		if i.NeedsLook() {
			st = lipgloss.NewStyle().Foreground(cFailed)
		}
	}
	if i.URL != "" {
		parts = append(parts, i.URL)
	}
	return strings.Join(parts, " · "), st
}
