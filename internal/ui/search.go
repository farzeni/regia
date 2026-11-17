package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
)

// The search view (third tab): what you type is looked up inside every
// listed session's conversation; the list shows the sessions with a match,
// each followed by its matching messages, and the panel shows the selected
// one in context with the matches highlighted.

// perSession caps the hits listed under one session (the header counts all).
const perSession = 200

// searchDelay: typing waits this long before searching, so a burst of keys
// searches once.
const searchDelay = 120 * time.Millisecond

type searchState struct {
	ti      textinput.Model // the query, edited with the usual readline keys
	tiOn    bool            // ti is set up
	query   string          // ti's value
	tools  bool // also search tool calls and their output
	typed   int  // bumped on every edit; a debounce tick only fires for the latest
	gen     int  // bumped on every search; only the latest result is kept
	running bool
	ranQ    string // the query the results are for
	results []claude.SessionHits
	fold    map[string]bool         // sessions folded to their header
	hits    map[string]searchHitRef // row id → its hit
}

// input is the query's text input, set up the first time it's used.
func (s *searchState) input() *textinput.Model {
	if !s.tiOn {
		s.ti, s.tiOn = newEditInput(s.query), true
		s.ti.KeyMap.DeleteAfterCursor.SetKeys() // ctrl+k moves up the list here
	}
	return &s.ti
}

// setQuery replaces the whole query (esc clears it).
func (m *model) setQuery(q string) tea.Cmd {
	m.srch.input().SetValue(q)
	m.srch.query = q
	return m.queryEdited()
}

var newlines = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

// editQuery passes a key to the query's text input; a change searches again.
func (m *model) editQuery(k tea.KeyMsg) tea.Cmd {
	ti := m.srch.input()
	if k.Type == tea.KeySpace {
		k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}
	}
	if k.Type == tea.KeyRunes {
		k.Runes = []rune(newlines.Replace(string(k.Runes))) // one line
	}
	if k.Type == tea.KeyRunes && !k.Alt && !k.Paste && len(k.Runes) > 1 {
		// fast typing comes as one message, whose name could be a key's
		// ("end", "home"…): one rune at a time, it's text
		for _, r := range k.Runes {
			*ti, _ = ti.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	} else {
		*ti, _ = ti.Update(k)
	}
	if v := ti.Value(); v != m.srch.query {
		m.srch.query = v
		return m.queryEdited()
	}
	return nil
}

type searchHitRef struct {
	s   *claude.SessionHits
	hit int // index in s.Hits
}

type searchTypedMsg struct{ typed int }
type searchDoneMsg struct {
	gen  int
	q    string
	res  []claude.SessionHits
	took time.Duration
}

func hitRowID(id string, index int) string { return fmt.Sprintf("%s#%d", id, index) }

// setView switches between tree, inbox and search, keeping the selection on
// the same session where it can.
func (m *model) setView(v viewKind) {
	if m.view == viewSearch {
		if r := m.selected(); r != nil && r.Agent != nil {
			m.cursorID = r.Agent.ID // from a hit row back to its session
		}
	}
	m.view = v
	m.panelScroll = 0
	if v == viewSearch {
		m.typing = false // keys go to the query; the draft is kept
		m.comp = nil
	}
	m.rebuild()
}

// cycleView is tab (d=1) and shift+tab (d=-1): tree → inbox → search.
func (m *model) cycleView(d int) tea.Cmd {
	m.setView(viewKind((int(m.view) + d + int(viewCount)) % int(viewCount)))
	if m.view == viewSearch && m.srch.query != "" {
		return m.runSearch() // sessions may have moved on since
	}
	return nil
}

// searchSources: every listed session, archived and finished ones too.
func (m *model) searchSources() []claude.Source {
	var srcs []claude.Source
	for _, a := range m.agents {
		if p := claude.TranscriptPath(a); p != "" {
			srcs = append(srcs, claude.Source{ID: a.ID, Path: p})
		}
	}
	return srcs
}

func (m *model) queryEdited() tea.Cmd {
	m.srch.typed++
	n := m.srch.typed
	if claude.ParseQuery(m.srch.query).Empty() {
		if r := m.selected(); r != nil && r.Agent != nil {
			m.cursorID = r.Agent.ID // esc again goes back to the tree on this session
		}
		m.srch.gen++ // drop a search still running
		m.srch.results, m.srch.ranQ, m.srch.running = nil, "", false
		m.rebuild()
		return nil
	}
	return tea.Tick(searchDelay, func(time.Time) tea.Msg { return searchTypedMsg{n} })
}

func (m *model) runSearch() tea.Cmd {
	q := claude.ParseQuery(m.srch.query)
	if q.Empty() {
		return nil
	}
	m.srch.gen++
	m.srch.running = true
	gen, qs, tools, srcs := m.srch.gen, m.srch.query, m.srch.tools, m.searchSources()
	return func() tea.Msg {
		t0 := time.Now()
		res := claude.Search(srcs, q, tools, perSession)
		return searchDoneMsg{gen, qs, res, time.Since(t0)}
	}
}

func (m *model) searchDone(msg searchDoneMsg) {
	if msg.gen != m.srch.gen {
		return // a newer search is on its way
	}
	m.srch.running = false
	jump := msg.q != m.srch.ranQ // a new query: back to the top
	m.srch.results, m.srch.ranQ = msg.res, msg.q
	if jump {
		m.cursorID, m.cursor, m.offset = "", 0, 0
	}
	m.rebuild()
}

// searchRows builds the list: each session with a match, then its hits
// (newest first) unless it's folded.
func (m *model) searchRows() {
	m.srch.hits = map[string]searchHitRef{}
	m.rows = m.rows[:0]
	for i := range m.srch.results {
		s := &m.srch.results[i]
		a := m.agents[s.ID]
		if a == nil {
			continue
		}
		title := a.Name
		if al := m.layout.Alias[a.ID]; al != "" {
			title = al
		}
		folded := m.srch.fold[s.ID]
		m.rows = append(m.rows, layout.Row{ID: s.ID, Agent: a, Title: title, Count: s.Total, HasKids: true, Collapsed: folded,
			Where: m.layout.Trail(a.ID, m.agents), Archived: m.layout.IsArchived(a.ID)})
		if folded {
			continue
		}
		for j, h := range s.Hits {
			id := hitRowID(s.ID, h.Index)
			m.srch.hits[id] = searchHitRef{s, j}
			m.rows = append(m.rows, layout.Row{ID: id, Agent: a, Title: title, Depth: 1})
		}
	}
}

// selectedHit is the hit under the cursor; on a session's header, its newest.
func (m *model) selectedHit() (*claude.SessionHits, *claude.Hit) {
	r := m.selected()
	if r == nil {
		return nil, nil
	}
	if ref, ok := m.srch.hits[r.ID]; ok {
		return ref.s, &ref.s.Hits[ref.hit]
	}
	for i := range m.srch.results {
		if s := &m.srch.results[i]; s.ID == r.ID && len(s.Hits) > 0 {
			return s, &s.Hits[0]
		}
	}
	return nil, nil
}

// updateSearch handles keys in the search view: typing edits the query,
// arrows move through the hits, enter attaches to the session.
func (m *model) updateSearch(k tea.KeyMsg) tea.Cmd {
	page := max(1, m.bodyHeight()/2)
	switch k.String() {
	case "ctrl+c":
		return tea.Quit
	case "tab":
		return m.cycleView(1)
	case "shift+tab":
		return m.cycleView(-1)
	case "esc":
		if m.srch.query != "" {
			return m.setQuery("")
		}
		m.setView(viewTree)
		return nil
	case "up", "ctrl+p", "ctrl+k":
		m.moveCursor(-1)
	case "down", "ctrl+n", "ctrl+j":
		m.moveCursor(1)
	case "pgup":
		m.panelScroll += page
	case "pgdown":
		m.panelScroll = max(0, m.panelScroll-page)
	case "ctrl+home": // home/end move in the query
		m.moveCursor(-len(m.rows))
	case "ctrl+end":
		m.moveCursor(len(m.rows))
	case "ctrl+up", "alt+up", "ctrl+down", "alt+down": // session to session
		d := 1
		if s := k.String(); s == "ctrl+up" || s == "alt+up" {
			d = -1
		}
		for i := m.cursor + d; i >= 0 && i < len(m.rows); i += d {
			if m.rows[i].HasKids {
				m.moveCursor(i - m.cursor)
				break
			}
		}
	case "shift+left", "shift+right": // fold / unfold the session (←/→ move in the query)
		r := m.selected()
		if r == nil || r.Agent == nil {
			return nil
		}
		if m.srch.fold == nil {
			m.srch.fold = map[string]bool{}
		}
		fold := k.String() == "shift+left"
		if m.srch.fold[r.Agent.ID] == fold {
			return nil
		}
		m.srch.fold[r.Agent.ID] = fold
		m.cursorID = r.Agent.ID
		m.rebuild()
	case "ctrl+t":
		m.srch.tools = !m.srch.tools
		m.flash("tool calls and output %s", map[bool]string{true: "searched too", false: "left out"}[m.srch.tools])
		return m.runSearch()
	case "enter":
		r := m.selected()
		if r == nil || r.Agent == nil {
			return nil
		}
		if r.Agent.Interactive {
			m.flash("interactive session: open it in its own terminal (pid shown in `claude agents`)")
			return nil
		}
		id := r.Agent.ID
		return attachSession(id)
	default: // everything else edits the query: ←/→, home/end, alt+←/→, ctrl+w…
		return m.editQuery(k)
	}
	return nil
}

// --- drawing ---------------------------------------------------------------

var (
	stMatch = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("221"))
	roleTag = map[claude.Role]string{claude.RoleYou: "you", claude.RoleAgent: "agent", claude.RoleTool: "tool"}
)

func roleStyle(r claude.Role) lipgloss.Style {
	switch r {
	case claude.RoleYou:
		return lipgloss.NewStyle().Foreground(cAccent)
	case claude.RoleTool:
		return stDim
	}
	return lipgloss.NewStyle()
}

// highlight styles s with base, and the matches of q in it with stMatch.
func highlight(s string, q claude.Query, base lipgloss.Style) string {
	var b strings.Builder
	last := 0
	for _, sp := range q.Spans(s) {
		b.WriteString(base.Render(s[last:sp[0]]))
		b.WriteString(stMatch.Render(s[sp[0]:sp[1]]))
		last = sp[1]
	}
	b.WriteString(base.Render(s[last:]))
	return b.String()
}

var spaceRun = strings.NewReplacer("\n", " ", "\t", " ", "\r", " ")

// snippet is one line of text, w columns wide, around the first match: it
// starts a little before it, so the match shows whatever the message's length.
func snippet(text string, q claude.Query, w int) string {
	s := strings.Join(strings.Fields(spaceRun.Replace(text)), " ")
	if w <= 1 {
		return ""
	}
	if sp := q.Spans(s); len(sp) > 0 {
		lead := runewidth.StringWidth(s[:sp[0][0]])
		end := runewidth.StringWidth(s[:sp[0][1]])
		if back := w / 4; end > w-1 && lead > back { // the match would be cut off
			cut := cutWidth(s, lead-back)
			s = "…" + s[cut:]
		}
	}
	return runewidth.Truncate(s, w, "…")
}

// cutWidth is the byte offset in s where the first w columns end.
func cutWidth(s string, w int) int {
	n := 0
	for i, r := range s {
		if n >= w {
			return i
		}
		n += runewidth.RuneWidth(r)
	}
	return len(s)
}

// searchRowLine draws a row of the search list: a session (state, name,
// how many matches) or one of its hits (who, when, the line around the match).
func (m *model) searchRowLine(r layout.Row, w int) string {
	q := claude.ParseQuery(m.srch.ranQ)
	if ref, ok := m.srch.hits[r.ID]; ok {
		h := ref.s.Hits[ref.hit]
		tag := roleStyle(h.Msg.Role).Render(fmt.Sprintf("%-5s", roleTag[h.Msg.Role]))
		age := fmt.Sprintf("%3s", ago(h.Msg.At))
		pre := "    " + tag + " " + stDim.Render(age) + "  "
		room := w - lipgloss.Width(pre) - 1
		base := lipgloss.NewStyle()
		if h.Msg.Role == claude.RoleTool {
			base = stDim
		}
		text := h.Msg.Text
		if h.Msg.Role == claude.RoleAgent {
			text = mdPlain(text)
		}
		return pre + highlight(snippet(text, q, room), q, base)
	}
	a := r.Agent
	g, c := m.agentGlyph(a)
	fold := "▾ "
	if r.Collapsed {
		fold = "▸ "
	}
	meta := fmt.Sprintf("%d", r.Count)
	ts := stBold
	if r.Archived {
		ts = lipgloss.NewStyle().Foreground(cDim).Italic(true)
	}
	title := trunc(r.Title, w-lipgloss.Width(meta)-6)
	line := stDim.Render(fold) + lipgloss.NewStyle().Foreground(c).Render(g) + " " + ts.Render(highlight(title, q, lipgloss.NewStyle()))
	return pad(line, w-lipgloss.Width(meta)-1) + stDim.Render(meta)
}

func (m *model) searchEmpty() string {
	switch {
	case strings.TrimSpace(m.srch.query) == "":
		return "type to search inside every session"
	case m.srch.running || m.srch.ranQ != m.srch.query:
		return "searching…"
	case m.srch.tools:
		return "nothing found"
	}
	return "nothing found · ctrl+t searches tool output too"
}

// searchLine is the status line while searching: the query and the counts.
func (m *model) searchLine(w int) string {
	n := 0
	for _, s := range m.srch.results {
		n += s.Total
	}
	info := ""
	switch {
	case m.srch.running:
		info = "searching…"
	case m.srch.ranQ != "":
		matches := fmt.Sprintf("%d matches", n)
		if n == 1 {
			matches = "1 match"
		}
		info = matches + " in " + count(len(m.srch.results), "session")
	}
	tools := "ctrl+t: tool output too"
	if m.srch.tools {
		tools = "ctrl+t: without tool output"
	}
	if time.Since(m.statusAt) < 6*time.Second && m.status != "" {
		tools = m.status
	}
	hint := stDim.Render("  " + strings.Join(nonEmpty(info, "↑↓ enter attaches", tools, "esc clears"), " · "))
	prompt := stAccent.Render("search › ")
	ti := m.srch.input()
	ti.Width = max(1, w-lipgloss.Width(prompt)-1) // past that it scrolls sideways; -1: the cursor
	q := strings.TrimRight(ti.View(), " ")        // the input pads to its width: the hint follows the text
	return prompt + q + trunc(hint, max(0, w-lipgloss.Width(prompt)-lipgloss.Width(q)))
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// searchPanel shows the selected hit in context: a few messages before and
// after it, matches highlighted, scrolled so the first match is in view.
func (m *model) searchPanel(w, h int) panel {
	s, hit := m.selectedHit()
	if s == nil {
		return panel{}
	}
	a := m.agents[s.ID]
	if a == nil {
		return panel{}
	}
	q := claude.ParseQuery(m.srch.ranQ)
	title := a.Name
	if al := m.layout.Alias[a.ID]; al != "" {
		title = al
	}
	g, c := m.agentGlyph(a)
	var top []string
	top = append(top, lipgloss.NewStyle().Foreground(c).Render(g)+" "+stBold.Render(trunc(title, w-2)))
	where := strings.Join(m.layout.Trail(a.ID, m.agents), " › ")
	meta := strings.Join(nonEmpty(where, tildify(a.Cwd), a.ID), " · ")
	top = append(top, stDim.Render(trunc(meta, w)))
	top = append(top, stDim.Render(trunc(fmt.Sprintf("match %d of %d, newest first · %s %s · pgup/pgdn", indexOf(s, hit)+1, s.Total,
		dayLabel(hit.Msg.At), clock(hit.Msg.At)), w)), stDim.Render(strings.Repeat("─", w)))

	msgs := claude.Messages(s.Path)
	if hit.Index >= len(msgs) {
		return panel{card: top}
	}
	// the neighbours: tool messages only when they are searched
	const around = 3
	var idx []int
	for i, n := hit.Index-1, 0; i >= 0 && n < around; i-- {
		if (msgs[i].Role != claude.RoleTool || m.srch.tools) && msgs[i].Text != "" { // a silent step says nothing
			idx = append([]int{i}, idx...)
			n++
		}
	}
	idx = append(idx, hit.Index)
	for i, n := hit.Index+1, 0; i < len(msgs) && n < around; i++ {
		if (msgs[i].Role != claude.RoleTool || m.srch.tools) && msgs[i].Text != "" { // a silent step says nothing
			idx = append(idx, i)
			n++
		}
	}
	var body []string
	focus := 0 // line of the first match in the hit
	for _, i := range idx {
		msg := msgs[i]
		mine := i == hit.Index
		head := messageHead(msg, mine)
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, head)
		lines, md := messageLines(msg, max(10, w-2))
		if !mine {
			if len(lines) > 4 { // neighbours: a glimpse
				lines = append(lines[:4:4], "…")
			}
			for _, l := range lines { // dim, in their shape but without colors
				body = append(body, "  "+stDim.Render(strings.TrimRight(ansi.Strip(l), " ")))
			}
			continue
		}
		for _, l := range lines {
			if focus == 0 && len(q.Spans(ansi.Strip(l))) > 0 {
				focus = len(body)
			}
			if md {
				body = append(body, "  "+highlightStyled(l, q))
			} else {
				body = append(body, "  "+highlight(strings.TrimRight(l, " "), q, lipgloss.NewStyle()))
			}
		}
	}
	// scroll: the first match a third down the room, then pgup/pgdn
	room := max(1, h-len(top))
	anchor := max(0, min(focus-room/3, len(body)-room))
	start := max(0, anchor-m.panelScroll)
	m.panelScroll = anchor - start
	end := min(len(body), start+room)
	return panel{card: append(top, body[start:end]...)}
}

func indexOf(s *claude.SessionHits, h *claude.Hit) int {
	for i := range s.Hits {
		if s.Hits[i].Index == h.Index {
			return i
		}
	}
	return 0
}
