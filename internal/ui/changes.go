package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/gitinfo"
	"github.com/farzeni/regia/internal/layout"
	"github.com/farzeni/regia/internal/plugins"
)

// The agent box's "changes" tab: the files the agent edited or wrote, from
// its conversation's steps, with the lines it added and removed, the most
// recent first. D shows it and gives it the keys: j/k move, enter opens the
// file at the line it last changed, d opens the agent's whole diff (from
// where its branch left the default one) in your diff tool.
//
//	╭─ agent │ changes 2 │ gh-pr ─────────────────╮
//	│ ▸ internal/ui/changes.go:42         +120 −3 │
//	│   README.md:210                       +4 −1 │
//	│ 2 files · +124 −4 · enter opens · d diff    │
//	╰─────────────────────────────────────────────╯

const changesTab = "changes"

// maxChangeRows is how many files the tab lists at once; the cursor scrolls.
const maxChangeRows = 8

// changedFile is one file the agent touched, its edits summed.
type changedFile struct {
	Path           string // absolute
	Rel            string // as shown: relative to the agent's directory when inside it
	Added, Removed int
	Line           int // where its last edit starts (0: unknown)
	last           int // the step that last touched it, for the order
}

// changedFiles sums the successful edits in es by file, newest first.
func changedFiles(es []claude.Entry, cwd string) []changedFile {
	idx := map[string]int{}
	var out []changedFile
	for n, e := range es {
		s := e.Step
		if s == nil || s.Res == nil || s.Res.IsError || !editTools[s.Name] {
			continue
		}
		p := s.Res.FilePath
		if p == "" {
			p = s.In["file_path"]
		}
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		i, ok := idx[p]
		if !ok {
			i = len(out)
			idx[p] = i
			out = append(out, changedFile{Path: p, Rel: relPath(p, cwd)})
		}
		f := &out[i]
		f.Added += s.Res.Added
		f.Removed += s.Res.Removed
		f.Line, f.last = editLine(s.Res), n
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].last > out[j].last })
	return out
}

// relPath is p relative to dir when it's inside it, else with ~ for home.
func relPath(p, dir string) string {
	if r, err := filepath.Rel(dir, p); err == nil && dir != "" && !strings.HasPrefix(r, "..") {
		return r
	}
	return tildify(p)
}

// editLine is the first changed line of an edit's last hunk.
func editLine(r *claude.Result) int {
	if len(r.Patch) == 0 {
		return 0
	}
	h := r.Patch[len(r.Patch)-1]
	n := h.NewStart
	for _, l := range h.Lines {
		if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
			return n
		}
		n++
	}
	return h.NewStart
}

// changesCache keeps the last agent's files: the panel is drawn 8 times a
// second and summing walks the whole conversation. A new line in the
// transcript (len(msgs)) means new steps.
var changesCache struct {
	path, cwd string
	n         int
	files     []changedFile
}

func changesOf(a *claude.Agent) []changedFile {
	path := claude.TranscriptPath(a)
	msgs := claude.Messages(path)
	c := &changesCache
	if c.path != path || c.cwd != a.Cwd || c.n != len(msgs) || c.files == nil {
		c.path, c.cwd, c.n = path, a.Cwd, len(msgs)
		c.files = changedFiles(claude.Conversation(msgs), a.Cwd)
		if c.files == nil {
			c.files = []changedFile{}
		}
	}
	return c.files
}

// changesState is the changes tab's cursor, and what it last drew (for keys
// and clicks).
type changesState struct {
	focus bool   // it has the keys (D, or a click on a file)
	sel   string // the selected file's path ("" = the first)
	id    string // the agent drawn
	files []changedFile
	rows  []int // per body line, the file drawn there or -1
}

func (c *changesState) selIndex() int {
	for i, f := range c.files {
		if f.Path == c.sel {
			return i
		}
	}
	return 0
}

// changesTab is the tab for a: its body, and a badge with the file count.
func (m *model) changesTab(a *claude.Agent, w int) panelTab {
	files := changesOf(a)
	c := &m.chg
	c.id, c.files = a.ID, files
	t := panelTab{name: changesTab, own: true}
	if len(files) > 0 {
		t.badge = &plugins.Badge{Text: fmt.Sprint(len(files))}
	}
	t.body, c.rows = changesBody(files, c.selIndex(), c.focus, boxInner(w))
	return t
}

// changesBody: up to maxChangeRows files around the selected one, then a
// line with the totals and what the keys do.
func changesBody(files []changedFile, sel int, focus bool, w int) (body []string, rows []int) {
	if len(files) == 0 {
		return []string{stDim.Render("no file changes yet")}, []int{-1}
	}
	start := max(0, sel-maxChangeRows+1)
	end := min(len(files), start+maxChangeRows)
	add, del := 0, 0
	for _, f := range files {
		add, del = add+f.Added, del+f.Removed
	}
	for i := start; i < end; i++ {
		body = append(body, changeLine(files[i], focus && i == sel, w))
		rows = append(rows, i)
	}
	foot := fmt.Sprintf("%d file%s · +%d −%d", len(files), plural(len(files)), add, del)
	if end-start < len(files) {
		foot = fmt.Sprintf("%d–%d of ", start+1, end) + foot
	}
	hint := " · D to browse"
	if focus {
		hint = " · enter opens · d full diff · esc back"
	}
	if lipgloss.Width(foot+hint) <= w { // the totals matter more than the hints
		foot += hint
	}
	body = append(body, stDim.Render(trunc(foot, w)))
	return body, append(rows, -1)
}

// changeLine is "▸ path:line      +N −M", the counts at the right edge.
func changeLine(f changedFile, sel bool, w int) string {
	var counts []string
	if f.Added > 0 {
		counts = append(counts, stAdd.Render(fmt.Sprintf("+%d", f.Added)))
	}
	if f.Removed > 0 {
		counts = append(counts, stDel.Render(fmt.Sprintf("−%d", f.Removed)))
	}
	right := strings.Join(counts, " ")
	lead := "  "
	if sel {
		lead = stAccent.Render("▸ ")
	}
	at := ""
	if f.Line > 0 {
		at = fmt.Sprintf(":%d", f.Line)
	}
	room := w - 2 - lipgloss.Width(right) - 1
	path := leftTrunc(f.Rel, max(1, room-len(at)))
	st := lipgloss.NewStyle()
	if sel {
		st = stBold
	}
	left := lead + st.Render(path) + stDim.Render(at)
	return pad(left, w-lipgloss.Width(right)) + right
}

// leftTrunc cuts s from the left to w columns ("…/ui/changes.go"): a path's
// end says most.
func leftTrunc(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > w-1 {
		r = r[1:]
	}
	return "…" + string(r)
}

// changesKey is D: the changes tab, with the keys; D again goes back to the
// agent tab.
func (m *model) changesKey(r *layout.Row) {
	if r == nil || r.Agent == nil {
		m.flash("changes: select an agent")
		return
	}
	if m.chg.focus {
		m.chg.focus, m.panelTab, m.status = false, agentTab, ""
		return
	}
	m.focusChanges()
}

func (m *model) focusChanges() {
	m.panelTab, m.chg.focus, m.convFocus = changesTab, true, false
	m.flash("changes: j/k move · enter opens the file · d full diff · esc back")
}

// updateChanges handles keys while the changes tab has them.
func (m *model) updateChanges(k tea.KeyMsg) tea.Cmd {
	r := m.selected()
	if r == nil || r.Agent == nil || m.panelTab != changesTab {
		m.chg.focus = false
		return m.updateNormal(k)
	}
	a := r.Agent
	c := &m.chg
	c.files = changesOf(a)
	i := c.selIndex()
	move := func(j int) {
		if len(c.files) > 0 {
			c.sel = c.files[max(0, min(len(c.files)-1, j))].Path
		}
	}
	switch k.String() {
	case "ctrl+c":
		return tea.Quit
	case "?":
		m.help = !m.help
	case "esc", "q":
		c.focus, m.status = false, ""
	case "D":
		m.changesKey(r)
	case "j", "down":
		move(i + 1)
	case "k", "up":
		move(i - 1)
	case "g", "home":
		move(0)
	case "G", "end":
		move(len(c.files))
	case ",", ".":
		m.cycleTab(map[string]int{",": -1, ".": 1}[k.String()])
	case "o":
		c.focus = false
		m.focusConv()
	case "enter":
		return m.openChanged(i)
	case "d":
		return mergeBaseCmd(a.Cwd)
	}
	return nil
}

// openChanged opens file i of the tab at its line in your editor.
func (m *model) openChanged(i int) tea.Cmd {
	if i < 0 || i >= len(m.chg.files) {
		m.flash("no file changes yet")
		return nil
	}
	f := m.chg.files[i]
	m.chg.sel = f.Path
	return openThing(openTarget{Kind: "file", Path: f.Path, Line: f.Line})
}

// mergeBaseMsg is where the agent's branch left the default one, found off
// the UI's goroutine: the diff opens from there.
type mergeBaseMsg struct{ dir, base, branch, err string }

func mergeBaseCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		c, b, e := gitinfo.MergeBase(dir)
		return mergeBaseMsg{dir, c, b, e}
	}
}

// openDiff opens the agent's whole diff (commits and uncommitted changes) in
// your diff tool.
func (m *model) openDiff(msg mergeBaseMsg) tea.Cmd {
	if msg.err != "" {
		m.flash("diff: %s", msg.err)
		return nil
	}
	if msg.branch == "" {
		m.flash("no main/master branch: the diff shows uncommitted changes only")
	}
	return openThing(openTarget{Kind: "diff", Dir: msg.dir, Base: msg.base})
}

// clickChanges handles a click on body line l of the changes tab: the first
// selects the file (and gives the tab the keys), a click on the selected one
// opens it. False when no file is there.
func (m *model) clickChanges(l int) (tea.Cmd, bool) {
	if m.panelTab != changesTab || m.chg.id != m.cursorID || l < 0 || l >= len(m.chg.rows) || m.chg.rows[l] < 0 {
		return nil, false
	}
	i := m.chg.rows[l]
	if m.chg.focus && i == m.chg.selIndex() {
		return m.openChanged(i), true
	}
	m.chg.sel = m.chg.files[i].Path
	m.focusChanges()
	return nil, true
}
