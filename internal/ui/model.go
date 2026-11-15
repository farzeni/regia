// Package ui is regia's terminal interface (bubbletea): the tree and inbox
// and search views, the detail panel, the always-on input and the key handling.
package ui

import (
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
	"github.com/farzeni/regia/internal/spawns"
)

type viewKind int

const (
	viewTree viewKind = iota
	viewInbox
	viewSearch
	viewCount // how many views tab cycles through
)

type mode int

const (
	modeNormal mode = iota
	modeInput
	modeConfirm
	modeDir // the directory picker (ctrl+o, w)
)

type model struct {
	agents map[string]*claude.Agent
	layout *layout.Layout
	rows   []layout.Row
	visRow map[string]bool

	cursorID string
	cursor   int
	offset   int
	w, h     int

	view viewKind
	opts layout.ViewOpts
	srch searchState // the search view
	help bool

	mode         mode
	input        string
	inputPrompt  string
	inputAction  func(m *model, s string) tea.Cmd
	confirmMsg   string
	confirmKey   string // the action's key: pressed again, it confirms
	confirmDo    func(m *model) tea.Cmd
	filterBefore string

	cut []string // nodes picked up with m, put with p / P

	selectID string // just started: select it as soon as it's listed

	selecting bool            // select mode (v): space marks rows for e / x
	marked    map[string]bool // agent ids marked in select mode

	notesFor string // the blackboard open with N (a group or coordinator id)
	noteSel  int    // the note picked on it, oldest first

	// geometry of the last frame, for the mouse
	lw, vis  int
	dragging bool // dragging the divider to resize the list

	panelScroll int    // lines the panel's history is scrolled back (pgup/pgdn)
	panelFor    string // the row panelScroll belongs to

	// the always-on input at the bottom: typing goes there, command keys
	// (j/k, J/K, s, y…) work after esc
	typing      bool
	ta          textarea.Model    // the draft: readline keys, multi-line
	pastes      map[string]string // long pastes folded into tokens
	pasteN      int
	imageN      int
	draftInside bool
	draftDir    string      // directory picked for this task (ctrl+o); "" = the usual rule
	dirPick     *dirPicker  // the directory picker, when open
	comp        *completion // the "/" skill or "@" file popup, when open
	compShut    string      // the word esc closed the popup for

	status   string
	statusAt time.Time

	dwellID string // agent selected since dwellAt (to notice you looked at it)
	dwellAt time.Time

	updateNote string // set once a background update ran; shown in the header

	hist    claude.History
	histFor string
	logs    string
	logsFor string

	fetching bool
	fetchErr error
	ticks    int
	pulsing  bool // the working-icon animation is ticking
	pulseN   int  // its frame
}

type agentsMsg struct {
	b   []byte
	err error
}
type tickMsg time.Time
type attachDoneMsg struct {
	id  string
	err error
}
type logsMsg struct{ id, out string }
type spawnedMsg struct {
	out           string
	err           error
	parent, after string
}
type stoppedMsg struct {
	id  string
	err error
}

func newModel() *model {
	m := &model{agents: map[string]*claude.Agent{}, layout: layout.Load(), ta: newDraftInput(), opts: layout.ViewOpts{ShowDone: true}} // focus on the list; i types
	if b := claude.CachedAgents(); b != nil {
		m.setAgents(b)
	}
	return m
}

func fetchCmd() tea.Msg {
	b, err := claude.FetchAgents()
	return agentsMsg{b, err}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Init() tea.Cmd {
	m.fetching = true
	return tea.Batch(fetchCmd, tick(), autoUpdateCmd, m.startPulse())
}

func (m *model) setAgents(b []byte) {
	list := claude.ParseAgents(b)
	if list == nil {
		return
	}
	next := make(map[string]*claude.Agent, len(list))
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
		claude.Enrich(a)
	}
	m.classify(time.Now())
	m.layout.Spawns = spawns.Cached()
	placed := m.layout.Place(m.agents)
	if m.layout.Revive(m.agents) || placed { // archived but working again: back in the tree
		_ = m.layout.Save()
	}
	m.rebuild()
}

func (m *model) rebuild() {
	m.opts.Keep = m.cursorID
	if m.view == viewInbox {
		var bl []*claude.Agent
		for _, a := range m.agents {
			if (a.State == "blocked" || a.State == "ready" || a.ID == m.opts.Keep) && m.shown(a.ID) {
				bl = append(bl, a)
			}
		}
		sort.SliceStable(bl, func(i, j int) bool { // waiting, ready, stale; newest first in each
			if ri, rj := inboxRank(bl[i]), inboxRank(bl[j]); ri != rj {
				return ri < rj
			}
			return waitedSince(bl[i]).After(waitedSince(bl[j]))
		})
		m.rows = m.rows[:0]
		for _, a := range bl {
			t := a.Name
			if al := m.layout.Alias[a.ID]; al != "" {
				t = al
			}
			m.rows = append(m.rows, layout.Row{ID: a.ID, Agent: a, Title: t, Where: m.layout.Trail(a.ID, m.agents)})
		}
	} else if m.view == viewSearch {
		m.searchRows()
	} else {
		m.rows = m.layout.Flatten(m.agents, m.opts)
	}
	m.visRow = make(map[string]bool, len(m.rows))
	for _, r := range m.rows {
		m.visRow[r.ID] = true
	}
	if m.selectID != "" && m.visRow[m.selectID] { // a just-started agent appeared
		m.cursorID, m.selectID = m.selectID, ""
	}
	prev := m.cursor // the selected row may have gone (archived, removed): stay at its place
	m.cursor = -1
	for i, r := range m.rows {
		if r.ID == m.cursorID {
			m.cursor = i
			break
		}
	}
	if m.cursor < 0 {
		m.cursor = max(0, prev)
	}
	if len(m.rows) > 0 {
		if m.cursor >= len(m.rows) {
			m.cursor = len(m.rows) - 1
		}
		m.cursorID = m.rows[m.cursor].ID
	}
	m.loadDetail()
}

func waitedSince(a *claude.Agent) time.Time {
	if !a.UpdatedAt.IsZero() {
		return a.UpdatedAt
	}
	return a.StartedAt
}

func (m *model) selected() *layout.Row {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return &m.rows[m.cursor]
}

func (m *model) loadDetail() {
	r := m.selected()
	if r == nil || r.Agent == nil || r.Agent.Interactive || m.view == viewSearch { // search draws its own panel
		m.hist, m.histFor = claude.History{}, ""
		return
	}
	m.hist = claude.ReadHistory(r.Agent)
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
	m.draftInside = false
	m.selectID = "" // you moved: don't jump to a just-started agent later
	m.panelScroll = 0
	if m.logsFor != m.cursorID {
		m.logs, m.logsFor = "", ""
	}
	m.loadDetail()
}

// syncLayout reloads the layout when another process (a second regia) saved
// it, so this instance never writes back a stale copy.
func (m *model) syncLayout() {
	if m.layout.ChangedOnDisk() {
		m.layout = layout.Load()
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

// confirm asks before do runs: pressing the action's key again confirms
// (x then x removes), any other key cancels.
func (m *model) confirm(key, msg string, do func(m *model) tea.Cmd) {
	m.mode, m.confirmKey, m.confirmDo = modeConfirm, key, do
	// the hint first: a long question is cut at the screen's edge
	m.confirmMsg = "[" + key + " again to confirm]  " + msg
}

// foldAll folds every group and coordinator, keeping the selection on its
// top-level ancestor; when everything on screen is already folded, it
// unfolds them all instead.
func (m *model) foldAll() {
	open := false
	for _, r := range m.rows {
		if (r.HasKids || r.Group) && !r.Collapsed {
			open = true
			break
		}
	}
	if !open {
		m.layout.Collapsed = map[string]bool{}
		m.flash("unfolded all")
	} else {
		for p, kids := range m.layout.Order {
			if p != "" && len(kids) > 0 {
				m.layout.Collapsed[p] = true
			}
		}
		for id := range m.layout.Groups {
			m.layout.Collapsed[id] = true
		}
		for m.layout.Parent[m.cursorID] != "" {
			m.cursorID = m.layout.Parent[m.cursorID]
		}
		m.flash("folded all · z unfolds")
	}
	m.saveLayout()
}
