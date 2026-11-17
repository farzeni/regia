package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The model a new agent runs: claude's default, unless one was picked for
// this task (ctrl+g). Like the directory, the pick lasts one task.

type modelItem struct{ name, desc string } // name "" = claude's default

var modelAliases = []modelItem{
	{"", "claude's default"},
	{"opus", "latest Opus"},
	{"sonnet", "latest Sonnet"},
	{"haiku", "latest Haiku"},
	{"fable", "latest Fable"},
}

type modelPicker struct {
	query string
	items []modelItem
	sel   int
}

func (m *model) openModelPick() {
	m.comp = nil
	m.modelPick, m.mode = &modelPicker{}, modeModel
	m.refreshModelPick()
}

func (m *model) closeModelPick() { m.modelPick, m.mode = nil, modeNormal }

// refreshModelPick filters the aliases by what was typed; anything else
// typed is offered as a full model id.
func (m *model) refreshModelPick() {
	p := m.modelPick
	q := strings.ToLower(strings.TrimSpace(p.query))
	p.items = nil
	for _, it := range modelAliases {
		if q == "" || (it.name != "" && strings.Contains(it.name, q)) {
			p.items = append(p.items, it)
		}
	}
	if q != "" && (len(p.items) == 0 || p.items[0].name != q) {
		p.items = append(p.items, modelItem{q, "use this model id"})
	}
	p.sel = min(p.sel, max(0, len(p.items)-1))
}

func (m *model) updateModelPick(k tea.KeyMsg) tea.Cmd {
	p := m.modelPick
	n := len(p.items)
	switch k.String() {
	case "esc", "ctrl+c", "ctrl+g":
		m.closeModelPick()
		return nil
	case "up", "ctrl+p":
		if n > 0 {
			p.sel = (p.sel + n - 1) % n
		}
		return nil
	case "down", "ctrl+n", "tab":
		if n > 0 {
			p.sel = (p.sel + 1) % n
		}
		return nil
	case "enter":
		if n > 0 {
			m.draftModel = p.items[p.sel].name
			m.flash("this task runs %s", modelName(m.draftModel))
		}
		m.closeModelPick()
		return nil
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
		}
	case "ctrl+u":
		p.query = ""
	default:
		if k.Type != tea.KeyRunes { // model ids have no spaces
			return nil
		}
		p.query += string(k.Runes)
	}
	p.sel = 0
	m.refreshModelPick()
	return nil
}

func modelName(s string) string {
	if s == "" {
		return "the default model"
	}
	return s
}

func (m *model) modelPickRows() int {
	if m.mode != modeModel || m.modelPick == nil {
		return 0
	}
	return max(1, len(m.modelPick.items))
}

func (m *model) modelPickPrompt() string {
	return stAccent.Render("model for this task: ") + m.modelPick.query + "█" +
		stDim.Render("  ↑↓ enter · type a model id · esc")
}

// modelPickView renders the picker's list where the completion popup goes.
func (m *model) modelPickView() string {
	p := m.modelPick
	sel := lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	nw := 0
	for _, it := range p.items {
		nw = max(nw, len([]rune(modelItemName(it)))+1)
	}
	nw = min(nw, max(20, m.w/2))
	var out []string
	for i, it := range p.items {
		name := pad(trunc(modelItemName(it), nw), nw)
		desc := trunc(it.desc, max(0, m.w-nw-6))
		l := "  " + name + "  " + stDim.Render(desc)
		if i == p.sel {
			l = sel.Render("› "+name) + "  " + desc
		}
		out = append(out, pad(l, m.w))
	}
	return strings.Join(out, "\n")
}

func modelItemName(it modelItem) string {
	if it.name == "" {
		return "default"
	}
	return it.name
}

// modelLabel is the input's model, for the box's border.
func (m *model) modelLabel() string {
	tail := stDim.Render(" · ctrl+g")
	if m.draftModel == "" {
		return stDim.Render("default") + tail
	}
	return stAccent.Render(trunc(m.draftModel, 24)) + tail
}
