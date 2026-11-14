package ui

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/attach"
	"github.com/farzeni/regia/internal/claude"
)

// Update wraps update so the selected agent's git info is refreshed in the
// background whenever it's stale (cursor moved, or every tick).
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	mm, cmd := m.update(msg)
	if r := m.selected(); r != nil && r.Agent != nil {
		if g := gitCmd(r.Agent.Cwd); g != nil {
			cmd = tea.Batch(cmd, g)
		}
	}
	return mm, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.sizeInput()
		return m, nil

	case updatedMsg:
		m.noteUpdate(msg)
		return m, nil

	case agentsMsg:
		m.fetching = false
		m.fetchErr = msg.err
		if msg.err == nil {
			m.setAgents(msg.b)
		}
		return m, m.startPulse()

	case pulseMsg:
		return m, m.onPulse()

	case tickMsg:
		m.ticks++
		m.refresh()
		var cmds []tea.Cmd
		cmds = append(cmds, tick())
		if m.ticks%2 == 0 && !m.fetching {
			m.fetching = true
			cmds = append(cmds, fetchCmd)
		}
		return m, tea.Batch(cmds...)

	case attachDoneMsg:
		if msg.err != nil {
			m.flash("attach: %v", msg.err)
		}
		m.fetching = true
		// bubbletea turns the mouse off for the attached session and doesn't turn it back on
		return m, tea.Batch(fetchCmd, tea.EnableMouseCellMotion)

	case tea.MouseMsg:
		return m, m.updateMouse(msg)

	case logsMsg:
		if msg.id == m.cursorID {
			m.logs, m.logsFor = msg.out, msg.id
			if strings.TrimSpace(m.logs) == "" {
				m.logs = "(no recent terminal output)"
			}
		}
		return m, nil

	case spawnedMsg:
		if msg.err != nil {
			m.flash("spawn failed: %v %s", msg.err, strings.TrimSpace(msg.out))
			return m, nil
		}
		id := regexp.MustCompile(`\b[0-9a-f]{8}\b`).FindString(msg.out)
		if id != "" {
			m.layout.PlaceNew(id, msg.parent, msg.after)
			for p := msg.parent; p != ""; p = m.layout.Parent[p] { // so its row is on screen
				delete(m.layout.Collapsed, p)
			}
			// select it once `claude agents` lists it (it may not yet): enter attaches
			m.selectID = id
			if m.view == viewInbox {
				m.view = viewTree
			}
			_ = m.layout.Save()
			m.rebuild()
			m.flash("started %s", id)
		} else {
			m.flash("started: %s", strings.TrimSpace(msg.out))
		}
		m.fetching = true
		return m, fetchCmd

	case stoppedMsg:
		if msg.err != nil {
			m.flash("stop %s: %v", msg.id, msg.err)
		} else {
			m.flash("stopped %s", msg.id)
		}
		m.fetching = true
		return m, fetchCmd

	case removedBatchMsg:
		s := fmt.Sprintf("removed %s (conversations kept)", count(len(msg.removed), "session"))
		if len(msg.refused) > 0 {
			s += fmt.Sprintf(" · %d refused: %s", len(msg.refused), strings.Join(msg.refused, "; "))
		}
		m.flash("%s", s)
		m.fetching = true
		return m, fetchCmd

	case removedMsg:
		if msg.err != nil {
			// claude rm explains why (e.g. unpushed commits in the worktree)
			m.flash("not removed: %s", oneLine(msg.out, msg.err))
			return m, nil
		}
		m.flash("removed %s (conversation kept: claude --resume)", msg.id)
		m.fetching = true
		return m, fetchCmd

	case tea.KeyMsg:
		if m.help { // the help modal is on top: close it, ignore the rest
			switch msg.String() {
			case "esc", "?", "q", "enter", "ctrl+c":
				m.help = false
			}
			return m, nil
		}
		switch m.mode {
		case modeInput:
			return m, m.updateInput(msg)
		case modeConfirm:
			m.mode = modeNormal
			if msg.String() == m.confirmKey {
				return m, m.confirmDo(m)
			}
			m.flash("cancelled (%s again confirms)", m.confirmKey)
			return m, nil
		}
		if m.typing {
			m.syncLayout()
			return m, m.updateTyping(msg)
		}
		// Runes that arrive together (fast typing, key repeat) come as one
		// message: feed them one at a time through Update, so a key that opens
		// a prompt (d, n, /…) passes the following keys to that prompt.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				_, c := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt})
				cmds = append(cmds, c)
			}
			return m, tea.Batch(cmds...)
		}
		m.syncLayout()
		if m.selecting {
			if c, ok := m.updateSelect(msg); ok {
				return m, c
			}
		}
		return m, m.updateNormal(msg)
	}
	return m, nil
}

func (m *model) updateInput(k tea.KeyMsg) tea.Cmd {
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mode = modeNormal
		if m.inputPrompt == "/" {
			m.opts.Filter = m.filterBefore
			m.rebuild()
		}
		return nil
	case tea.KeyEnter:
		m.mode = modeNormal
		return m.inputAction(m, m.input)
	case tea.KeyBackspace:
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		m.input = ""
	case tea.KeySpace:
		m.input += " "
	case tea.KeyRunes:
		m.input += string(k.Runes)
	}
	if m.inputPrompt == "/" { // live filter
		m.opts.Filter = m.input
		m.rebuild()
	}
	return nil
}

func (m *model) updateNormal(k tea.KeyMsg) tea.Cmd {
	r := m.selected()
	page := max(1, m.bodyHeight()/2)
	switch k.String() {
	case "ctrl+c":
		return tea.Quit
	case "?":
		m.help = !m.help
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "ctrl+d":
		m.moveCursor(page)
	case "ctrl+u":
		m.moveCursor(-page)
	case "pgup": // the panel's history: back in time
		m.panelScroll += page
	case "pgdown":
		m.panelScroll = max(0, m.panelScroll-page)
	case "alt+up", "alt+down", "ctrl+up", "ctrl+down", "[", "]": // group to group (Tilix takes alt+↑/↓)
		d := 1
		if s := k.String(); s == "alt+up" || s == "ctrl+up" || s == "[" {
			d = -1
		}
		for i := m.cursor + d; i >= 0 && i < len(m.rows); i += d {
			if m.rows[i].Group {
				m.moveCursor(i - m.cursor)
				break
			}
		}
	case "z":
		m.foldAll()
	case "-", "+", "=": // narrower / wider list
		d := 4
		if k.String() == "-" {
			d = -4
		}
		m.resizeSidebar(m.sidebarWidth(m.w-2*padX) + d)
	case "e":
		return m.archiveSelection()
	case "A":
		m.opts.ShowArchived = !m.opts.ShowArchived
		if m.opts.ShowArchived {
			m.flash("showing archived (dim) · e brings one back · A hides them")
		}
		m.rebuild()
	case "v":
		m.selecting, m.marked = true, map[string]bool{}
		m.flash("select: space marks (a group: all of it), * all · e archive · x remove · esc done")
	case "g", "home":
		m.moveCursor(-len(m.rows))
	case "G", "end":
		m.moveCursor(len(m.rows))
	case "tab":
		if m.view == viewTree {
			m.view = viewInbox
		} else {
			m.view = viewTree
		}
		m.rebuild()
	case "b": // jump to next blocked agent
		for i := 1; i <= len(m.rows); i++ {
			j := (m.cursor + i) % len(m.rows)
			if a := m.rows[j].Agent; a != nil && a.State == "blocked" {
				m.moveCursor(j - m.cursor)
				break
			}
		}
	case "a":
		m.opts.ShowDone = !m.opts.ShowDone
		m.flash("finished sessions %s", map[bool]string{true: "shown", false: "hidden"}[m.opts.ShowDone])
		m.rebuild()
	case "/":
		m.filterBefore = m.opts.Filter
		m.prompt("/", m.opts.Filter, func(m *model, s string) tea.Cmd { m.opts.Filter = s; m.rebuild(); return nil })
	case "esc": // clear what's active (filter, a move); with nothing to clear, quit
		if m.opts.Filter == "" && m.cut == "" {
			return tea.Quit
		}
		m.opts.Filter, m.cut = "", ""
		m.rebuild()
	case "R":
		m.fetching = true
		return fetchCmd

	case "right", "l":
		if r != nil && (r.HasKids || r.Group) && r.Collapsed {
			delete(m.layout.Collapsed, r.ID)
			m.saveLayout()
		}
	case "enter":
		if r == nil {
			return nil
		}
		if r.Agent == nil {
			m.layout.Collapsed[r.ID] = !m.layout.Collapsed[r.ID]
			m.saveLayout()
			return nil
		}
		if r.Agent.Interactive {
			m.flash("interactive session: open it in its own terminal (pid shown in `claude agents`)")
			return nil
		}
		return tea.Exec(attach.Session(r.ID), func(err error) tea.Msg { return attachDoneMsg{err} })
	case " ":
		if r != nil && (r.HasKids || r.Group) {
			m.layout.Collapsed[r.ID] = !m.layout.Collapsed[r.ID]
			m.saveLayout()
		}
	case "left", "h":
		if r == nil {
			return nil
		}
		if (r.HasKids || r.Group) && !r.Collapsed {
			m.layout.Collapsed[r.ID] = true
			m.saveLayout()
		} else if p := m.layout.Parent[r.ID]; p != "" && m.view == viewTree {
			m.cursorID = p
			m.rebuild()
		}

	case "y":
		if r != nil && r.Agent != nil && r.Agent.Suggested != "" {
			if err := copyClipboard(r.Agent.Suggested); err != nil {
				m.flash("clipboard: %v", err)
			} else {
				m.flash("copied suggested reply — enter to attach, then paste")
			}
		} else {
			m.flash("no suggested reply")
		}
	case "Y":
		if r != nil && r.Agent != nil {
			_ = copyClipboard(r.Agent.SessionID)
			m.flash("copied session id %s", r.Agent.SessionID)
		}
	case "o":
		if r != nil && r.Agent != nil && !r.Agent.Interactive {
			id := r.ID
			m.logs, m.logsFor = "loading…", id
			return func() tea.Msg {
				out, _ := exec.Command("claude", "logs", id).CombinedOutput()
				return logsMsg{id, string(out)}
			}
		}
	case "x":
		if r != nil && r.Agent != nil && !r.Agent.Interactive {
			id, name := r.ID, r.Title
			m.confirm("x", fmt.Sprintf("remove %q (%s) from the list? its worktree is deleted if safe; conversation kept", name, id), func(m *model) tea.Cmd {
				return func() tea.Msg {
					out, err := removeAgent(id)
					return removedMsg{id, out, err}
				}
			})
		}
	case "s":
		if r != nil && r.Agent != nil && !r.Agent.Interactive {
			id, name := r.ID, r.Title
			m.confirm("s", fmt.Sprintf("stop %q (%s)? conversation is kept", name, id), func(m *model) tea.Cmd {
				return func() tea.Msg {
					out, err := exec.Command("claude", "stop", id).CombinedOutput()
					if err != nil {
						err = fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
					}
					return stoppedMsg{id, err}
				}
			})
		}

	// ---- organization (tree view only)
	case "K", "shift+up":
		if r != nil && m.view == viewTree && m.layout.Move(r.ID, -1, m.agents, m.visRow) {
			m.saveLayout()
		}
	case "J", "shift+down":
		if r != nil && m.view == viewTree && m.layout.Move(r.ID, 1, m.agents, m.visRow) {
			m.saveLayout()
		}
	case ">":
		if r != nil && m.view == viewTree {
			if err := m.layout.Indent(r.ID, m.agents, m.visRow); err != nil {
				m.flash("%v", err)
			} else {
				m.saveLayout()
			}
		}
	case "<":
		if r != nil && m.view == viewTree {
			if err := m.layout.Outdent(r.ID, m.agents); err != nil {
				m.flash("%v", err)
			} else {
				m.saveLayout()
			}
		}
	case "m":
		if r != nil && m.view == viewTree {
			m.cut = r.ID
			m.flash("moving %q — p: put below cursor, P: put inside cursor, esc: cancel", r.Title)
		}
	case "p", "P":
		if r != nil && m.cut != "" && m.view == viewTree {
			var err error
			if k.String() == "P" {
				err = m.layout.Reparent(m.cut, r.ID, "", m.agents)
			} else {
				err = m.layout.Reparent(m.cut, m.layout.Parent[r.ID], r.ID, m.agents)
			}
			if err != nil {
				m.flash("%v", err)
			} else {
				m.cursorID, m.cut = m.cut, ""
				m.saveLayout()
			}
		}
	case "n":
		near := ""
		if r != nil && m.view == viewTree {
			near = r.ID
		}
		m.prompt("new group: ", "", func(m *model, s string) tea.Cmd {
			if s = strings.TrimSpace(s); s != "" {
				m.cursorID = m.layout.NewGroup(s, near, m.agents)
				m.saveLayout()
			}
			return nil
		})
	case "r":
		if r == nil {
			return nil
		}
		id := r.ID
		m.prompt("rename: ", r.Title, func(m *model, s string) tea.Cmd {
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
		})
	case "d":
		if r != nil && r.Group {
			id, title := r.ID, r.Title
			m.confirm("d", fmt.Sprintf("remove group %q? its agents move up a level", title), func(m *model) tea.Cmd {
				m.syncLayout()
				m.layout.DeleteGroup(id, m.agents)
				m.saveLayout()
				m.flash("removed group %q", title)
				return nil
			})
		}
	case "i":
		m.typing = true
	case "c", "C":
		m.typing, m.draftInside = true, k.String() == "C" && r != nil
	}
	return nil
}

// cwdFor finds a working directory for a node: its own, or the first agent below it.
func (m *model) cwdFor(id string) string {
	if a := m.agents[id]; a != nil && a.Cwd != "" {
		return a.Cwd
	}
	for _, k := range m.layout.Children(id, m.agents) {
		if c := m.cwdFor(k); c != claude.Home() {
			return c
		}
	}
	return claude.Home()
}

func (m *model) titleOf(id string) string {
	if t, ok := m.layout.Groups[id]; ok {
		return t
	}
	if al := m.layout.Alias[id]; al != "" {
		return al
	}
	if a := m.agents[id]; a != nil {
		return a.Name
	}
	return id
}
