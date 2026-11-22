package ui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// In-place editing: renaming a row (r) and naming a new group (n) happen
// where the text is drawn, in the sidebar row, with the usual readline keys
// (←/→, home/end, ctrl+a/e, alt+←/→, backspace, alt+backspace/ctrl+w,
// ctrl+u/ctrl+k, delete). enter saves, esc cancels.

type inlineEdit struct {
	ti     textinput.Model
	row    string // the sidebar row being edited
	save   func(m *model, s string) tea.Cmd
	cancel func(m *model)
}

func newEditInput(initial string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 0
	ti.Cursor.SetMode(cursor.CursorStatic)
	ti.KeyMap.AcceptSuggestion.SetEnabled(false)
	ti.KeyMap.NextSuggestion.SetEnabled(false)
	ti.KeyMap.PrevSuggestion.SetEnabled(false)
	ti.SetValue(initial)
	ti.CursorEnd()
	ti.Focus()
	return ti
}

// editRow starts editing the sidebar row id in place.
func (m *model) editRow(id, initial string, save func(m *model, s string) tea.Cmd, cancel func(m *model)) {
	m.edit = &inlineEdit{ti: newEditInput(initial), row: id, save: save, cancel: cancel}
	m.mode = modeEdit
	m.flash("enter saves · esc cancels")
}

func (m *model) updateEdit(k tea.KeyMsg) tea.Cmd {
	e := m.edit
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mode, m.edit = modeNormal, nil
		if e.cancel != nil {
			e.cancel(m)
		}
		m.flash("")
		return nil
	case tea.KeyEnter:
		m.mode, m.edit = modeNormal, nil
		m.flash("")
		return e.save(m, e.ti.Value())
	}
	if k.Type == tea.KeyRunes && !k.Alt && !k.Paste && len(k.Runes) > 1 {
		// fast typing comes as one message, whose name could be a key's
		// ("end", "home"…): one rune at a time, it's text
		for _, r := range k.Runes {
			e.ti, _ = e.ti.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		return nil
	}
	e.ti, _ = e.ti.Update(k)
	return nil
}

// editView is the text being edited, w columns wide: it scrolls sideways to
// keep the cursor in view.
func (e *inlineEdit) view(w int) string {
	e.ti.Width = max(1, w-1) // the cursor takes a column past the text
	return e.ti.View()
}

// editingRow is the edit on sidebar row id, if any.
func (m *model) editingRow(id string) *inlineEdit {
	if m.mode == modeEdit && m.edit != nil && m.edit.row != "" && m.edit.row == id {
		return m.edit
	}
	return nil
}

// renameKey (r) renames the selected group, or sets the selected agent's
// alias, in its sidebar row. An empty alias goes back to the agent's name.
func (m *model) renameKey(id, title string) {
	m.editRow(id, title, func(m *model, s string) tea.Cmd {
		m.syncLayout()
		s = strings.TrimSpace(s)
		if m.layout.IsGroup(id) {
			if s != "" {
				m.layout.Groups[id] = s
			}
		} else if s == "" || (m.agents[id] != nil && s == m.agents[id].Name) {
			delete(m.layout.Alias, id)
		} else {
			m.layout.Alias[id] = s
		}
		m.saveLayout()
		return nil
	}, nil)
}

// newGroupInPlace makes an unnamed group with create (placing it, and moving
// anything into it) and has you name it in its sidebar row. done runs once
// it's named; esc, or no name, puts the tree back as it was.
func (m *model) newGroupInPlace(create func() (string, error), done func(m *model, gid, title string)) {
	m.syncLayout()
	order := cloneOrder(m.layout.Order)
	parent := make(map[string]string, len(m.layout.Parent))
	for k, v := range m.layout.Parent {
		parent[k] = v
	}
	gid, err := create()
	if err != nil {
		m.flash("%v", err)
		return
	}
	if m.view != viewTree { // the new row is drawn in the tree
		m.setView(viewTree)
	}
	m.opts.Filter = "" // a filter hides groups
	m.cursorID = gid
	m.saveLayout()
	undo := func(m *model) {
		m.syncLayout()
		m.layout.Order, m.layout.Parent = order, parent
		delete(m.layout.Groups, gid)
		delete(m.layout.Collapsed, gid)
		delete(m.layout.Dirs, gid)
		m.saveLayout()
	}
	m.editRow(gid, "", func(m *model, s string) tea.Cmd {
		if s = strings.TrimSpace(s); s == "" {
			undo(m)
			return nil
		}
		m.syncLayout()
		if !m.layout.IsGroup(gid) {
			m.flash("the group is gone")
			return nil
		}
		m.layout.Groups[gid] = s
		m.cursorID = gid
		m.saveLayout()
		if done != nil {
			done(m, gid, s)
		}
		return nil
	}, undo)
}

// cloneOrder copies the tree's order, so a cancelled new group can put it back.
func cloneOrder(o map[string][]string) map[string][]string {
	c := make(map[string][]string, len(o))
	for k, v := range o {
		c[k] = slices.Clone(v)
	}
	return c
}
