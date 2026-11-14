package main

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// spawnAgent starts `claude --bg <task>` in dir and returns its output (which
// carries the new id). A variable so tests never start a real agent.
var spawnAgent = func(dir, task string) (string, error) {
	cmd := exec.Command("claude", "--bg", task)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// spawnTarget says where a new agent goes: next to the selected agent, or
// inside it (C, or a group), in the working directory of that part of the tree.
func (m *model) spawnTarget() (parent, after, cwd, where string) {
	cwd = home()
	if r := m.selected(); r != nil {
		if m.draftInside || r.Group {
			parent = r.ID
		} else {
			parent, after = m.layout.Parent[r.ID], r.ID
		}
		cwd = m.cwdFor(r.ID)
	}
	switch {
	case parent == "":
		where = "top level"
	case after == "":
		where = "inside " + m.titleOf(parent)
	default:
		where = "next to " + m.titleOf(after)
	}
	return
}

func (m *model) spawn(task string) tea.Cmd {
	parent, after, cwd, where := m.spawnTarget()
	m.flash("starting %s in %s…", where, tildify(cwd))
	return func() tea.Msg {
		out, err := spawnAgent(cwd, task)
		return spawnedMsg{out, err, parent, after}
	}
}

// updateTyping handles keys while the bottom input has focus. Text goes into
// the draft; list movement and enter-to-attach still work.
func (m *model) updateTyping(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "enter":
		if task := strings.TrimSpace(m.draft); task != "" {
			m.draft = ""
			cmd := m.spawn(task)
			m.draftInside = false
			return cmd
		}
		return m.updateNormal(k) // empty: attach / fold, as in command mode
	case "esc":
		if m.draft != "" {
			m.draft, m.draftInside = "", false
			return nil
		}
		m.typing = false
		m.updateNormal(k) // also clears filter, help
		m.flash("command keys (j/k, J/K, s, y, …) — i to type again")
		return nil
	case "ctrl+c":
		if m.draft != "" {
			m.draft = ""
			return nil
		}
		return tea.Quit
	case "ctrl+f":
		return m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	case "ctrl+u":
		m.draft = ""
		return nil
	case "ctrl+w":
		t := strings.TrimRight(m.draft, " ")
		if i := strings.LastIndexAny(t, " \n"); i >= 0 {
			m.draft = t[:i+1]
		} else {
			m.draft = ""
		}
		return nil
	case "backspace":
		if r := []rune(m.draft); len(r) > 0 {
			m.draft = string(r[:len(r)-1])
		}
		return nil
	case "up", "down", "pgup", "pgdown", "home", "end", "tab", "shift+up", "shift+down", "ctrl+d":
		return m.updateNormal(k)
	case "left", "right":
		if m.draft == "" {
			return m.updateNormal(k)
		}
		return nil
	}
	switch k.Type {
	case tea.KeySpace:
		m.draft += " "
	case tea.KeyRunes:
		s := string(k.Runes)
		if m.draft == "" && s == "?" && !k.Paste {
			m.help = !m.help
			return nil
		}
		if k.Paste {
			s = strings.ReplaceAll(s, "\r\n", "\n")
		}
		m.draft += s
	}
	return nil
}

// inputLine renders the always-on input: the draft with a cursor, or a hint
// saying where a new agent would go.
func (m *model) inputLine() string {
	caret := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render("❯ ")
	if !m.typing {
		return stDim.Render("❯ i to type a new task")
	}
	if m.draft == "" {
		_, _, cwd, where := m.spawnTarget()
		return caret + "█" + stDim.Render(trunc(" new task — starts "+where+" in "+tildify(cwd), m.w-3))
	}
	d := strings.ReplaceAll(m.draft, "\n", "↵")
	room := m.w - 4
	if r := []rune(d); len(r) > room && room > 1 {
		d = "…" + string(r[len(r)-room+1:])
	}
	return caret + d + "█"
}
