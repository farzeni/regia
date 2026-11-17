package ui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/claude"
)

// What search looks in (ctrl+g in the search view, like the model picker in
// the input): the sessions regia lists, or every conversation on disk,
// removed and old sessions too. The choice lasts until regia quits.

type searchScope int

const (
	scopeSessions searchScope = iota // what `claude agents --all` lists
	scopeHistory                     // every transcript in ~/.claude/projects
)

var searchScopes = []struct{ name, desc string }{
	{"sessions", "the sessions in the list: running, finished, archived"},
	{"history", "every conversation on disk, removed and old sessions too"},
}

// pastState is a session only the history knows: not listed any more.
const pastState = "removed"

func (m *model) openScopePick() {
	m.scopeSel, m.mode = int(m.srch.scope), modeScope
}

func (m *model) updateScopePick(k tea.KeyMsg) tea.Cmd {
	n := len(searchScopes)
	switch k.String() {
	case "esc", "ctrl+c", "ctrl+g":
		m.mode = modeNormal
	case "up", "ctrl+p", "k", "shift+tab":
		m.scopeSel = (m.scopeSel + n - 1) % n
	case "down", "ctrl+n", "j", "tab":
		m.scopeSel = (m.scopeSel + 1) % n
	case "enter":
		m.mode = modeNormal
		if sc := searchScope(m.scopeSel); sc != m.srch.scope {
			m.srch.scope = sc
			m.flash("searching %s", searchScopes[sc].desc)
			return m.runSearch()
		}
	}
	return nil
}

func (m *model) scopePickRows() int {
	if m.mode != modeScope {
		return 0
	}
	return len(searchScopes)
}

func (m *model) scopePickPrompt() string {
	return stAccent.Render("search in: ") + stDim.Render("↑↓ enter · esc")
}

// scopePickView lists the scopes where the completion popup goes, like the
// model picker.
func (m *model) scopePickView() string {
	sel := lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	var out []string
	for i, s := range searchScopes {
		name := pad(s.name, 10)
		desc := trunc(s.desc, max(0, m.w-18))
		l := "  " + name + "  " + stDim.Render(desc)
		if i == m.scopeSel {
			l = sel.Render("› "+name) + "  " + desc
		}
		out = append(out, pad(l, m.w))
	}
	return strings.Join(out, "\n")
}

// searchAgent is a result's session: a listed agent, or one only the history
// has (named after its title, or what you first wrote).
func (m *model) searchAgent(id string) *claude.Agent {
	if a := m.agents[id]; a != nil {
		return a
	}
	return m.srch.past[id]
}

// historySearch adds every unlisted transcript to srcs; it runs in the search's
// tea.Cmd (it reads the disk), and names the sessions that matched.
func historySearch(srcs []claude.Source, skip map[string]bool, q claude.Query, tools bool) ([]claude.SessionHits, map[string]*claude.Agent) {
	listed := make(map[string]bool, len(srcs))
	for _, s := range srcs {
		listed[s.ID] = true
	}
	res := claude.Search(append(srcs, claude.HistorySources(skip)...), q, tools, perSession)
	past := map[string]*claude.Agent{}
	for _, r := range res {
		if listed[r.ID] {
			continue
		}
		meta := claude.Meta(r.Path)
		name := meta.Title
		if name == "" {
			name = claude.FirstPrompt(claude.Messages(r.Path))
		}
		if name == "" {
			name = r.ID[:min(8, len(r.ID))]
		}
		past[r.ID] = &claude.Agent{ID: r.ID, SessionID: r.ID, Name: name, Cwd: meta.Cwd, State: pastState, Transcript: r.Path}
	}
	return res, past
}

type resumeDoneMsg struct{ err error }

// resumeSession continues a removed session in this terminal (claude
// --resume, in the directory it ran in) until you quit it; a var so tests can
// see it without running claude.
var resumeSession = func(sid, dir string) tea.Cmd {
	cmd := exec.Command("claude", "--resume", sid)
	cmd.Dir = dir
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return resumeDoneMsg{err} })
}

// resumePast is enter on a session only the history has.
func (m *model) resumePast(a *claude.Agent) tea.Cmd {
	if a.Cwd == "" {
		m.flash("can't resume: the transcript doesn't say which directory it ran in")
		return nil
	}
	return resumeSession(a.SessionID, a.Cwd)
}
