package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type viewKind int

const (
	viewTree viewKind = iota
	viewInbox
)

type mode int

const (
	modeNormal mode = iota
	modeInput
	modeConfirm
)

type model struct {
	agents map[string]*Agent
	layout *Layout
	rows   []Row
	visRow map[string]bool

	cursorID string
	cursor   int
	offset   int
	w, h     int

	view viewKind
	opts viewOpts
	help bool

	mode         mode
	input        string
	inputPrompt  string
	inputAction  func(m *model, s string) tea.Cmd
	confirmMsg   string
	confirmDo    func(m *model) tea.Cmd
	filterBefore string

	cut string

	status   string
	statusAt time.Time

	hist        History
	histFor     string
	logs        string
	logsFor     string

	fetching bool
	fetchErr error
	ticks    int
}

type agentsMsg struct {
	b   []byte
	err error
}
type tickMsg time.Time
type attachDoneMsg struct{ err error }
type logsMsg struct{ id, out string }
type spawnedMsg struct {
	out          string
	err          error
	parent, after string
}
type stoppedMsg struct {
	id  string
	err error
}

func newModel() *model {
	m := &model{agents: map[string]*Agent{}, layout: loadLayout()}
	if b := cachedAgents(); b != nil {
		m.setAgents(b)
	}
	return m
}

func fetchCmd() tea.Msg {
	b, err := fetchAgents()
	return agentsMsg{b, err}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Init() tea.Cmd {
	m.fetching = true
	return tea.Batch(fetchCmd, tick())
}

func (m *model) setAgents(b []byte) {
	list := parseAgents(b)
	if list == nil {
		return
	}
	next := make(map[string]*Agent, len(list))
	for _, a := range list {
		next[a.ID] = a
	}
	m.agents = next
	m.refresh()
}

// refresh re-reads job state files and rebuilds rows, keeping the cursor on
// the same node.
func (m *model) refresh() {
	m.syncLayout()
	for _, a := range m.agents {
		enrich(a)
	}
	if m.layout.Place(m.agents) {
		_ = m.layout.Save()
	}
	m.rebuild()
}

func (m *model) rebuild() {
	if m.view == viewInbox {
		var bl []*Agent
		for _, a := range m.agents {
			if a.State == "blocked" && m.opts.visible(a, m.layout.Alias[a.ID]) {
				bl = append(bl, a)
			}
		}
		sort.SliceStable(bl, func(i, j int) bool { return waitedSince(bl[i]).Before(waitedSince(bl[j])) })
		m.rows = m.rows[:0]
		for _, a := range bl {
			t := a.Name
			if al := m.layout.Alias[a.ID]; al != "" {
				t = al
			}
			m.rows = append(m.rows, Row{ID: a.ID, Agent: a, Title: t})
		}
	} else {
		m.rows = m.layout.Flatten(m.agents, m.opts)
	}
	m.visRow = make(map[string]bool, len(m.rows))
	for _, r := range m.rows {
		m.visRow[r.ID] = true
	}
	m.cursor = 0
	for i, r := range m.rows {
		if r.ID == m.cursorID {
			m.cursor = i
			break
		}
	}
	if len(m.rows) > 0 {
		if m.cursor >= len(m.rows) {
			m.cursor = len(m.rows) - 1
		}
		m.cursorID = m.rows[m.cursor].ID
	}
	m.loadDetail()
}

func waitedSince(a *Agent) time.Time {
	if !a.UpdatedAt.IsZero() {
		return a.UpdatedAt
	}
	return a.StartedAt
}

func (m *model) selected() *Row {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return &m.rows[m.cursor]
}

func (m *model) loadDetail() {
	r := m.selected()
	if r == nil || r.Agent == nil || r.Agent.Interactive {
		m.hist, m.histFor = History{}, ""
		return
	}
	m.hist = readHistory(r.Agent)
	m.histFor = r.ID
}

func (m *model) flash(f string, a ...any) {
	m.status = fmt.Sprintf(f, a...)
	m.statusAt = time.Now()
}

func (m *model) moveCursor(d int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = max(0, min(len(m.rows)-1, m.cursor+d))
	m.cursorID = m.rows[m.cursor].ID
	if m.logsFor != m.cursorID {
		m.logs, m.logsFor = "", ""
	}
	m.loadDetail()
}

// syncLayout reloads the layout when another process (a second regia) saved
// it, so this instance never writes back a stale copy.
func (m *model) syncLayout() {
	if fi, err := os.Stat(layoutPath()); err == nil && !fi.ModTime().Equal(m.layout.mtime) {
		m.layout = loadLayout()
	}
}

func (m *model) saveLayout() {
	if err := m.layout.Save(); err != nil {
		m.flash("save failed: %v", err)
	}
	m.rebuild()
}

func (m *model) prompt(p, initial string, do func(m *model, s string) tea.Cmd) {
	m.mode, m.inputPrompt, m.input, m.inputAction = modeInput, p, initial, do
}

func (m *model) confirm(msg string, do func(m *model) tea.Cmd) {
	m.mode, m.confirmMsg, m.confirmDo = modeConfirm, msg, do
}

// ---------------------------------------------------------------- update

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
		return m, nil

	case agentsMsg:
		m.fetching = false
		m.fetchErr = msg.err
		if msg.err == nil {
			m.setAgents(msg.b)
		}
		return m, nil

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
		return m, fetchCmd

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
			m.layout.Parent[id] = msg.parent
			m.layout.Order[msg.parent] = insertAfter(remove(m.layout.Order[msg.parent], id), msg.after, id)
			m.cursorID = id
			_ = m.layout.Save()
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

	case tea.KeyMsg:
		switch m.mode {
		case modeInput:
			return m, m.updateInput(msg)
		case modeConfirm:
			m.mode = modeNormal
			switch msg.String() {
			case "y", "Y", "enter":
				return m, m.confirmDo(m)
			}
			m.flash("cancelled (y confirms)")
			return m, nil
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
		return m, m.updateNormal(msg)
	}
	return m, nil
}

func (m *model) updateInput(k tea.KeyMsg) tea.Cmd {
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mode = modeNormal
		if m.inputPrompt == "/" {
			m.opts.filter = m.filterBefore
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
		m.opts.filter = m.input
		m.rebuild()
	}
	return nil
}

func (m *model) updateNormal(k tea.KeyMsg) tea.Cmd {
	r := m.selected()
	page := max(1, m.bodyHeight()/2)
	switch k.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "?":
		m.help = !m.help
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "ctrl+d", "pgdown":
		m.moveCursor(page)
	case "ctrl+u", "pgup":
		m.moveCursor(-page)
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
		m.opts.showDone = !m.opts.showDone
		m.flash("finished sessions %s", map[bool]string{true: "shown", false: "hidden"}[m.opts.showDone])
		m.rebuild()
	case "/":
		m.filterBefore = m.opts.filter
		m.prompt("/", m.opts.filter, func(m *model, s string) tea.Cmd { m.opts.filter = s; m.rebuild(); return nil })
	case "esc":
		m.opts.filter, m.cut, m.help = "", "", false
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
		return tea.Exec(&attachCmd{id: r.ID}, func(err error) tea.Msg { return attachDoneMsg{err} })
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
	case "s":
		if r != nil && r.Agent != nil && !r.Agent.Interactive {
			id, name := r.ID, r.Title
			m.confirm(fmt.Sprintf("stop %q (%s)? conversation is kept  [y/N]", name, id), func(m *model) tea.Cmd {
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
			if m.layout.isGroup(id) {
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
			m.confirm(fmt.Sprintf("remove group %q? its agents move up a level  [y/N]", title), func(m *model) tea.Cmd {
				m.syncLayout()
				m.layout.DeleteGroup(id, m.agents)
				m.saveLayout()
				m.flash("removed group %q", title)
				return nil
			})
		}
	case "c", "C":
		if r == nil && k.String() == "C" {
			return nil
		}
		parent, after, cwd := "", "", home()
		if r != nil {
			if k.String() == "C" || r.Group {
				parent = r.ID
			} else {
				parent, after = m.layout.Parent[r.ID], r.ID
			}
			cwd = m.cwdFor(r.ID)
		}
		where := "top level"
		if parent != "" {
			where = "under " + m.titleOf(parent)
		}
		m.prompt(fmt.Sprintf("new agent %s in %s: ", where, tildify(cwd)), "", func(m *model, s string) tea.Cmd {
			if s = strings.TrimSpace(s); s == "" {
				return nil
			}
			m.flash("starting…")
			return func() tea.Msg {
				cmd := exec.Command("claude", "--bg", s)
				cmd.Dir = cwd
				out, err := cmd.CombinedOutput()
				return spawnedMsg{string(out), err, parent, after}
			}
		})
	}
	return nil
}

// cwdFor finds a working directory for a node: its own, or the first agent below it.
func (m *model) cwdFor(id string) string {
	if a := m.agents[id]; a != nil && a.Cwd != "" {
		return a.Cwd
	}
	for _, k := range m.layout.children(id, m.agents) {
		if c := m.cwdFor(k); c != home() {
			return c
		}
	}
	return home()
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

// ---------------------------------------------------------------- view

var (
	cBlocked = lipgloss.Color("214")
	cBusy    = lipgloss.Color("42")
	cIdle    = lipgloss.Color("245")
	cDone    = lipgloss.Color("65")
	cFailed  = lipgloss.Color("196")
	cInter   = lipgloss.Color("75")
	cDim     = lipgloss.Color("241")
	cAccent  = lipgloss.Color("141")

	stDim    = lipgloss.NewStyle().Foreground(cDim)
	stBold   = lipgloss.NewStyle().Bold(true)
	stAccent = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stSel    = lipgloss.NewStyle().Background(lipgloss.Color("237"))
	stHead   = lipgloss.NewStyle().Foreground(cDim).Bold(true)
)

func glyph(a *Agent) (string, lipgloss.Color) {
	if a.Interactive {
		return "⌨", cInter
	}
	switch a.State {
	case "blocked":
		return "●", cBlocked
	case "busy":
		return "◆", cBusy
	case "done":
		return "✓", cDone
	case "failed":
		return "✗", cFailed
	}
	return "○", cIdle
}

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func tildify(p string) string {
	if h := home(); strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

func trunc(s string, w int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if w <= 0 {
		return ""
	}
	return runewidth.Truncate(s, w, "…")
}

func pad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func (m *model) bodyHeight() int { return max(1, m.h-3) }

func (m *model) View() string {
	if m.w == 0 {
		return ""
	}
	bh := m.bodyHeight()
	lw := max(30, min(64, m.w*42/100))
	if m.w < 70 {
		lw = m.w
	}
	dw := m.w - lw - 3

	// scroll
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+bh {
		m.offset = m.cursor - bh + 1
	}
	m.offset = max(0, min(m.offset, max(0, len(m.rows)-bh)))

	left := make([]string, 0, bh)
	for i := m.offset; i < len(m.rows) && len(left) < bh; i++ {
		left = append(left, m.renderRow(m.rows[i], lw, i == m.cursor))
	}
	if len(m.rows) == 0 {
		msg := "no agents"
		if m.view == viewInbox {
			msg = "nobody is waiting for you"
		}
		if m.opts.filter != "" {
			msg = "nothing matches /" + m.opts.filter
		}
		left = append(left, stDim.Render("  "+msg))
	}
	for len(left) < bh {
		left = append(left, "")
	}

	var right []string
	if m.help {
		right = helpLines()
	} else if dw > 20 {
		right = m.detailLines(dw)
	}
	sep := stDim.Render("│")
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteByte('\n')
	for i := 0; i < bh; i++ {
		b.WriteString(pad(left[i], lw))
		if dw > 20 {
			b.WriteString(" " + sep + " ")
			if i < len(right) {
				b.WriteString(right[i])
			}
		}
		b.WriteByte('\n')
	}
	b.WriteString(m.footer())
	return b.String()
}

func (m *model) header() string {
	var nb, nbusy, n int
	for _, a := range m.agents {
		if a.State == "done" {
			continue
		}
		n++
		switch a.State {
		case "blocked":
			nb++
		case "busy":
			nbusy++
		}
	}
	tabs := []string{"tree", "inbox"}
	for i, t := range tabs {
		if viewKind(i) == m.view {
			tabs[i] = stAccent.Render("[" + t + "]")
		} else {
			tabs[i] = stDim.Render(" " + t + " ")
		}
	}
	s := stBold.Render("regia") + "  " + strings.Join(tabs, "") + "   " +
		lipgloss.NewStyle().Foreground(cBlocked).Render(fmt.Sprintf("● %d need you", nb)) + "  " +
		lipgloss.NewStyle().Foreground(cBusy).Render(fmt.Sprintf("◆ %d busy", nbusy)) + "  " +
		stDim.Render(fmt.Sprintf("%d live", n))
	if m.opts.filter != "" {
		s += "  " + stAccent.Render("/"+m.opts.filter)
	}
	if m.fetchErr != nil {
		s += "  " + lipgloss.NewStyle().Foreground(cFailed).Render("listing failed: "+m.fetchErr.Error())
	}
	return s
}

func (m *model) footer() string {
	var line1 string
	switch m.mode {
	case modeInput:
		line1 = stAccent.Render(m.inputPrompt) + m.input + "█"
	case modeConfirm:
		line1 = lipgloss.NewStyle().Foreground(cBlocked).Render(m.confirmMsg)
	default:
		if time.Since(m.statusAt) < 6*time.Second {
			line1 = m.status
		}
	}
	keys := "enter attach  ←/→ fold  J/K reorder  >/< nest  m·p move  n group  r rename  c new agent  y copy reply  tab inbox  b next blocked  / filter  ? help"
	return pad(trunc(line1, m.w), m.w) + "\n" + stDim.Render(trunc(keys, m.w))
}

func (m *model) renderRow(r Row, w int, sel bool) string {
	indent := strings.Repeat("  ", r.Depth)
	fold := "  "
	if r.HasKids || r.Group {
		fold = "▾ "
		if r.Collapsed {
			fold = "▸ "
		}
	}
	var line string
	if r.Group {
		meta := fmt.Sprintf("%d", r.Count)
		if r.Busy > 0 {
			meta = lipgloss.NewStyle().Foreground(cBusy).Render(fmt.Sprintf("◆%d ", r.Busy)) + meta
		}
		if r.Blocked > 0 {
			meta = lipgloss.NewStyle().Foreground(cBlocked).Render(fmt.Sprintf("●%d ", r.Blocked)) + meta
		}
		title := trunc(r.Title, w-len(indent)-lipgloss.Width(meta)-4)
		line = indent + stDim.Render(fold) + stHead.Render(title)
		line = pad(line, w-lipgloss.Width(meta)-1) + meta
	} else {
		a := r.Agent
		g, c := glyph(a)
		age := ago(waitedSince(a))
		cutMark := ""
		if r.ID == m.cut {
			cutMark = "✂ "
		}
		extra := ""
		if m.view == viewInbox {
			extra = "  " + a.Needs
		} else if r.Collapsed && r.Blocked > 0 {
			extra = fmt.Sprintf("  (+%d waiting)", r.Blocked)
		}
		titleW := w - len(indent) - 6 - len(age) - len(cutMark)
		title := trunc(r.Title+stripNL(extra), titleW)
		ts := lipgloss.NewStyle()
		if a.State == "blocked" {
			ts = ts.Bold(true)
		} else if a.State == "done" {
			ts = ts.Foreground(cDim)
		}
		line = indent + stDim.Render(fold) + lipgloss.NewStyle().Foreground(c).Render(g) + " " + cutMark + ts.Render(title)
		line = pad(line, w-len(age)-1) + stDim.Render(age)
	}
	if sel {
		return stSel.Render(pad(line, w))
	}
	return line
}

func stripNL(s string) string { return strings.ReplaceAll(s, "\n", " ") }

func wrap(s string, w int) []string {
	if s == "" {
		return nil
	}
	return strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
}

func (m *model) detailLines(w int) []string {
	r := m.selected()
	if r == nil {
		return nil
	}
	var out []string
	sec := func(t string) { out = append(out, "", stHead.Render(t)) }
	if r.Group {
		out = append(out, stBold.Render(r.Title), stDim.Render(fmt.Sprintf("%d agents · %d need you · %d busy", r.Count, r.Blocked, r.Busy)))
		sec("waiting for you")
		n := 0
		for _, rr := range m.layout.Flatten(m.agents, viewOpts{showDone: false}) {
			if rr.Agent != nil && rr.Agent.State == "blocked" && m.layout.isDescendant(rr.ID, r.ID) {
				out = append(out, lipgloss.NewStyle().Foreground(cBlocked).Render("● ")+trunc(rr.Title+" — "+stripNL(rr.Agent.Needs), w-2))
				n++
			}
		}
		if n == 0 {
			out = append(out, stDim.Render("nothing"))
		}
		return out
	}
	return m.agentDetail(r, w)
}

func helpLines() []string {
	return strings.Split(`navigate
  j/k ↑/↓       move            g/G        top / bottom
  ctrl+d/u      half page       b          next agent waiting for you
  tab           tree ⇄ inbox    / filter   esc clear
  ←/→ h/l       fold / unfold   space      toggle fold

sessions
  enter         attach (← in the session comes back here)
  o             show terminal output (claude logs)
  y / Y         copy suggested reply / session id
  c / C         new agent next to / inside the selection
  s             stop (conversation is kept)
  a             show / hide finished sessions
  R             refresh now

organize  (saved in ~/.config/regia/layout.json)
  J/K           move up / down among siblings
  > / <         nest under the agent above (coordinator) / un-nest
  m then p / P  move: put below / inside the target
  n             new group      r  rename (alias)    d  remove group

? closes this help`, "\n")
}
