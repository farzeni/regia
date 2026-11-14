package main

import (
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// newDraftInput is the always-on input: a standard text field (readline keys:
// alt+backspace/ctrl+w, alt+d, ctrl+a/e, ctrl+k/u, alt+←/→, …) with a steady cursor.
func newDraftInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Cursor.SetMode(cursor.CursorStatic)
	ti.PlaceholderStyle = stDim
	ti.KeyMap.AcceptSuggestion.SetEnabled(false) // tab: tree ⇄ inbox
	ti.KeyMap.NextSuggestion.SetEnabled(false)   // ↑/↓ move the list
	ti.KeyMap.PrevSuggestion.SetEnabled(false)
	ti.KeyMap.CharacterForward.SetKeys("right") // ctrl+f: filter, as in claude agents
	ti.Focus()
	return ti
}

func (m *model) draft() string { return m.ti.Value() }

// updateTyping handles keys while the bottom input has focus. Text editing
// goes to the text field; list movement and enter-to-attach still work.
func (m *model) updateTyping(k tea.KeyMsg) tea.Cmd {
	empty := m.draft() == ""
	switch k.String() {
	case "enter":
		if task := strings.TrimSpace(m.draft()); task != "" {
			m.ti.Reset()
			cmd := m.spawn(task)
			m.draftInside = false
			return cmd
		}
		return m.updateNormal(k) // empty: attach / fold, as in command mode
	case "esc":
		if !empty {
			m.ti.Reset()
			m.draftInside = false
			return nil
		}
		m.typing = false
		m.updateNormal(k) // also clears filter, help
		m.flash("command keys (j/k, J/K, s, y, …) — i to type again")
		return nil
	case "ctrl+c":
		if !empty {
			m.ti.Reset()
			return nil
		}
		return tea.Quit
	case "ctrl+f":
		return m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	case "up", "down", "pgup", "pgdown", "tab", "shift+up", "shift+down":
		return m.updateNormal(k)
	case "left", "right", "home", "end", "ctrl+d":
		if empty { // nothing to edit: fold / top / bottom / half page
			return m.updateNormal(k)
		}
	case "?":
		if empty && !k.Paste {
			m.help = !m.help
			return nil
		}
	}
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(k)
	return cmd
}

// inputBox renders the always-on input in a rounded box: the draft with its
// cursor, or a hint saying where a new agent would go. The border is lit while
// the input has focus.
func (m *model) inputBox() string {
	iw := max(10, m.w-4) // border + padding
	caret := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render("❯ ")
	keys := stDim.Render("? keys")
	border := lipgloss.Color("240")
	var line string
	if !m.typing {
		line = stDim.Render("❯ command keys — i to type a new task")
	} else {
		border = cAccent
		_, _, cwd, where := m.spawnTarget()
		m.ti.Placeholder = "new task — starts " + where + " in " + tildify(cwd)
		m.ti.Width = max(5, iw-2-lipgloss.Width(keys)-3)
		line = caret + m.ti.View()
	}
	if lipgloss.Width(line)+lipgloss.Width(keys)+2 <= iw {
		line = pad(line, iw-lipgloss.Width(keys)) + keys
	} else {
		line = ansi.Truncate(line, iw, "…")
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(0, 1).Width(m.w - 2).Render(line)
}
