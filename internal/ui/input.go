package ui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/layout"
)

// spawnAgent starts `claude --bg <task>` in dir and returns its output (which
// carries the new id). A variable so tests never start a real agent.
var spawnAgent = func(dir, task string) (string, error) {
	cmd := exec.Command("claude", "--bg", task)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// spawnAgentModel is spawnAgent with the model picked for the task (ctrl+g).
var spawnAgentModel = func(dir, model, task string) (string, error) {
	cmd := exec.Command("claude", "--bg", "--model", model, task)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// spawnTarget says where a new agent goes: next to the selected agent, or
// inside it (C, or a group); and the directory it starts in, with why
// (layout.SpawnDir, or the one picked with ctrl+o).
func (m *model) spawnTarget() (parent, after, cwd, where, why string) {
	start := ""
	if r := m.selected(); r != nil {
		if m.draftInside || r.Group {
			parent = r.ID
		} else {
			parent, after = m.layout.Parent[r.ID], r.ID
		}
		start = r.ID
	}
	src := layout.DirSource{Kind: "chosen"}
	if cwd = m.draftDir; cwd == "" {
		cwd, src = m.layout.SpawnDir(start, m.agents)
	}
	why = m.dirWhy(src)
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
	parent, after, cwd, where, _ := m.spawnTarget()
	m.flash("starting %s in %s…", where, tildify(cwd))
	mod := m.draftModel
	return func() tea.Msg {
		if mod != "" {
			out, err := spawnAgentModel(cwd, mod, task)
			return spawnedMsg{out, err, parent, after}
		}
		out, err := spawnAgent(cwd, task)
		return spawnedMsg{out, err, parent, after}
	}
}

// maxPrompt keeps the task under Linux's per-argument limit (128 KiB).
const maxPrompt = 120 << 10

// newDraftInput is the always-on input: a multi-line text area with the
// usual readline keys (alt+backspace/ctrl+w, alt+d, ctrl+a/e, ctrl+k/u,
// alt+←/→, …). enter sends; alt+enter, ctrl+j or "\" + enter add a line.
func newDraftInput() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0 // no cap on lines; the box shows at most inputMaxRows
	ta.Cursor.SetMode(cursor.CursorStatic)
	ta.SetPromptFunc(2, func(i int) string {
		if i == 0 {
			return "❯ "
		}
		return "  "
	})
	st := &ta.FocusedStyle
	st.CursorLine = lipgloss.NewStyle()
	st.Placeholder = stDim
	st.Prompt = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	st.EndOfBuffer = lipgloss.NewStyle()
	ta.EndOfBufferCharacter = ' '
	km := &ta.KeyMap
	km.InsertNewline.SetKeys("alt+enter", "ctrl+j")
	km.CharacterForward.SetKeys("right") // ctrl+f: filter, as in claude agents
	km.WordForward.SetKeys("alt+right", "ctrl+right", "alt+f")
	km.WordBackward.SetKeys("alt+left", "ctrl+left", "alt+b")
	km.Paste.SetEnabled(false) // ctrl+v is handled here, so big pastes fold too
	ta.Focus()
	return ta
}

const inputMaxRows = 8

func (m *model) draft() string { return m.ta.Value() }

// sizeInput fits the text area to the box: its width, and as many rows as the
// draft needs (soft-wrapped), up to inputMaxRows.
func (m *model) sizeInput() {
	tw := max(10, m.w-4-lipgloss.Width("? keys")-1)
	m.ta.SetWidth(tw)
	text := max(1, tw-2)
	rows := 0
	for _, l := range strings.Split(m.draft(), "\n") {
		rows += ansi.StringWidth(l)/text + 1
	}
	m.ta.SetHeight(min(inputMaxRows, max(1, rows)))
}

// inputRows is how many rows the input box's content takes.
func (m *model) inputRows() int {
	if !m.typing {
		return 1
	}
	return m.ta.Height()
}

// paste adds pasted text: short pastes inline, long ones as a token that is
// expanded when the agent starts (like Claude Code's [Pasted text #1]).
func (m *model) paste(s string) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	if ps, ok := droppedPaths(s); ok { // files dropped on the terminal
		m.pasteFiles(ps)
		return
	}
	lines := strings.Count(s, "\n") + 1
	if lines <= 3 && len(s) <= 800 {
		m.ta.InsertString(s)
		return
	}
	m.pasteN++
	tok := fmt.Sprintf("[Pasted text #%d +%d lines]", m.pasteN, lines)
	if lines == 1 {
		tok = fmt.Sprintf("[Pasted text #%d, %d chars]", m.pasteN, len([]rune(s)))
	}
	if m.pastes == nil {
		m.pastes = map[string]string{}
	}
	m.pastes[tok] = s
	m.ta.InsertString(tok)
}

// pasteFiles adds files: images as [Image #n] tokens, others as their path.
func (m *model) pasteFiles(ps []string) {
	for i, p := range ps {
		if i > 0 {
			m.ta.InsertString(" ")
		}
		if isImage(p) {
			m.attachImage(p)
		} else {
			m.ta.InsertString(p)
		}
	}
}

// attachImage adds an [Image #n] token that becomes the image's path when
// the agent starts: the agent reads it with its Read tool, which sees images.
func (m *model) attachImage(path string) {
	m.imageN++
	tok := fmt.Sprintf("[Image #%d]", m.imageN)
	if m.pastes == nil {
		m.pastes = map[string]string{}
	}
	m.pastes[tok] = path
	m.ta.InsertString(tok)
}

// pasteClipboard is ctrl+v: an image on the clipboard is saved and attached,
// files copied in a file manager are added, anything else is pasted as text.
func (m *model) pasteClipboard() {
	types := clipboardTypes()
	// files first: Finder puts a copied file's icon on the clipboard as an image
	if ps := clipboardFiles(types); len(ps) > 0 {
		m.pasteFiles(ps)
		return
	}
	if data, ext, err := clipboardImage(types); err != nil {
		m.flash("clipboard image: %v", err)
		return
	} else if len(data) > 0 {
		p, err := savePastedImage(data, ext)
		if err != nil {
			m.flash("saving pasted image: %v", err)
			return
		}
		m.attachImage(p)
		return
	}
	s, err := readClipboard()
	if err != nil {
		m.flash("clipboard: %v", err)
		return
	}
	m.paste(s)
}

// expandPastes swaps the paste tokens still in the text for what was pasted.
func (m *model) expandPastes(s string) string {
	for tok, text := range m.pastes {
		s = strings.ReplaceAll(s, tok, text)
	}
	return s
}

func (m *model) clearDraft() {
	m.inHist = nil
	m.ta.Reset()
	m.pastes, m.draftInside, m.draftDir, m.draftModel = nil, false, "", ""
	m.comp, m.compShut = nil, ""
}

// atEnd says whether the cursor is at the very end of the draft.
func (m *model) atEnd() bool {
	if m.ta.Line() != m.ta.LineCount()-1 {
		return false
	}
	v := m.draft()
	last := []rune(v[strings.LastIndex(v, "\n")+1:])
	li := m.ta.LineInfo()
	return li.StartColumn+li.ColumnOffset >= len(last)
}

// updateTyping handles keys while the bottom input has focus. Text editing
// goes to the text area; list movement and enter-to-attach still work.
func (m *model) updateTyping(k tea.KeyMsg) tea.Cmd {
	// Let the text area scroll as if at full height, then fit it: resizing
	// after a 1-row update would leave its view scrolled past the first line.
	m.sizeInput()
	m.ta.SetHeight(inputMaxRows)
	defer m.sizeInput()
	defer m.updateCompletion() // runs before sizeInput: the popup follows the text
	empty := m.draft() == ""
	multi := strings.Contains(m.draft(), "\n")
	if k.Paste {
		m.paste(string(k.Runes))
		return nil
	}
	if m.updateCompletionKeys(k) {
		return nil
	}
	switch k.String() {
	case "enter":
		v := m.draft()
		if strings.HasSuffix(v, `\`) && m.atEnd() {
			// "\" at the end + enter: a new line instead of sending
			m.ta.SetValue(strings.TrimSuffix(v, `\`) + "\n")
			return nil
		}
		if task := strings.TrimSpace(m.expandPastes(v)); task != "" {
			if len(task) > maxPrompt {
				m.flash("task too long (%d KiB, max %d KiB): put it in a file and point the agent to it", len(task)>>10, maxPrompt>>10)
				return nil
			}
			if _, _, cwd, _, _ := m.spawnTarget(); !isDir(cwd) {
				m.flash("%s no longer exists · ctrl+o picks another directory", tildify(cwd))
				return nil
			}
			e := m.draftEntry()
			if err := appendHist(e); err != nil {
				m.flash("could not save the message in the history: %v", err)
			}
			cmd := m.spawn(task)
			m.clearDraft()
			m.typing = false // sent: keys go back to the list
			return cmd
		}
		return m.updateNormal(k) // empty: attach / fold, as in command mode
	case "esc":
		if !empty {
			if m.keepDraft() {
				m.clearDraft()
				m.flash("cleared · pgup brings it back")
			} else {
				m.clearDraft()
			}
			return nil
		}
		m.typing = false
		m.flash("esc again quits")
		return nil
	case "ctrl+c":
		if !empty {
			if m.keepDraft() {
				m.clearDraft()
				m.flash("cleared · pgup brings it back")
			} else {
				m.clearDraft()
			}
			return nil
		}
		return tea.Quit
	case "ctrl+f":
		return m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	case "ctrl+v":
		m.pasteClipboard()
		return nil
	case "ctrl+o":
		m.comp = nil
		m.openDirPick("")
		return nil
	case "ctrl+g":
		m.openModelPick()
		return nil
	case "up", "down":
		if !multi { // one line: arrows move the list; several: move in the text
			return m.updateNormal(k)
		}
	case "pgup": // earlier messages
		m.histStep(-1)
		return nil
	case "pgdown":
		if m.histStep(1) {
			return nil
		}
		return m.updateNormal(k) // not in the history: scroll the panel
	case "tab", "shift+tab", "shift+up", "shift+down", "alt+up", "alt+down", "ctrl+up", "ctrl+down":
		return m.updateNormal(k)
	case "left", "right", "home", "end", "ctrl+d":
		if empty { // nothing to edit: fold / top / bottom / half page
			return m.updateNormal(k)
		}
	case "?":
		if empty {
			m.help = !m.help
			return nil
		}
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(k)
	return cmd
}

// inputBox renders the always-on input in a rounded box: the draft with its
// cursor (growing to inputMaxRows lines), or a hint saying where a new agent
// would go. The border is lit while the input has focus.
func (m *model) inputBox() string {
	iw := max(10, m.w-4) // border + padding
	keys := stDim.Render("? keys")
	border := lipgloss.Color("240")
	var lines []string
	label := "" // shown in the bottom border, on the right
	if m.selecting {
		border = cBlocked
		lines = []string{lipgloss.NewStyle().Foreground(cBlocked).Render(fmt.Sprintf("◉ select · %d marked", len(m.markedIDs()))) +
			stDim.Render(" — space mark (group: all) · * all · e archive · x remove · f pin · esc done")}
	} else if !m.typing {
		lines = []string{stDim.Render("❯ i to type a new task")}
		if d := strings.TrimSpace(m.draft()); d != "" {
			lines = []string{stDim.Render("❯ i to go on with: ") + strings.ReplaceAll(d, "\n", " ⏎ ")}
		}
	} else {
		border = cAccent
		_, _, cwd, where, _ := m.spawnTarget()
		m.ta.Placeholder = "new task — starts " + where
		m.sizeInput()
		lines = strings.Split(m.ta.View(), "\n")
		label = m.dirLabel(cwd, m.w/2)
		// the model sits left of the directory, when both fit
		if both := m.modelLabel() + lipgloss.NewStyle().Foreground(border).Render(" ── ") + label; label != "" && lipgloss.Width(both)+8 <= m.w {
			label = both
		}
	}
	for i, l := range lines {
		l = ansi.Truncate(l, iw-lipgloss.Width(keys)-1, "…")
		if i == 0 {
			l = pad(l, iw-lipgloss.Width(keys)) + keys
		}
		lines[i] = l
	}
	return inputFrame(strings.Join(lines, "\n"), border, label, m.w)
}

type frameKey struct {
	text, label string
	border      lipgloss.Color
	w           int
}

var frameCache struct {
	key frameKey
	box string
}

// inputFrame draws the input's rounded box, w wide, around text, with label
// in its bottom border. The last one is kept: it's the same on most frames.
func inputFrame(text string, border lipgloss.Color, label string, w int) (box string) {
	k := frameKey{text, label, border, w}
	if frameCache.box != "" && frameCache.key == k {
		return frameCache.box
	}
	defer func() { frameCache.key, frameCache.box = k, box }()
	box = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(0, 1).Width(w - 2).Render(text)
	if label == "" {
		return box
	}
	// the directory sits in the bottom border: ╰──── ~/x · ctrl+o ─╯
	rows := strings.Split(box, "\n")
	bw := ansi.StringWidth(rows[len(rows)-1])
	lw := lipgloss.Width(label)
	if bw < lw+6 {
		return box
	}
	bs := lipgloss.NewStyle().Foreground(border)
	rows[len(rows)-1] = bs.Render("╰"+strings.Repeat("─", bw-lw-5)) + " " + label + " " + bs.Render("─╯")
	return strings.Join(rows, "\n")
}

// dirLabel is the input's directory, for the box's border: where the task
// will run (its tail when long), flagged when picked or missing.
func (m *model) dirLabel(cwd string, max int) string {
	dir := tildify(cwd)
	note := ""
	switch {
	case !isDir(cwd):
		note = lipgloss.NewStyle().Foreground(cFailed).Render(" missing")
	case m.draftDir != "":
		note = stAccent.Render(" picked")
	}
	tail := stDim.Render(" · ctrl+o")
	if r := []rune(dir); len(r) > 0 && lipgloss.Width(note+tail)+len(r) > max {
		keep := max - lipgloss.Width(note+tail) - 1
		if keep < 4 {
			return ""
		}
		dir = "…" + string(r[len(r)-keep:])
	}
	if !isDir(cwd) {
		dir = lipgloss.NewStyle().Foreground(cFailed).Render(dir)
	}
	return dir + note + tail
}
