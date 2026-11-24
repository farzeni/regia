// Package ui is regia's terminal interface (bubbletea): the tree and inbox
// and search views, the detail panel, the always-on input and the key handling.
package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/attach"
	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/group"
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
	modeDir  // the directory picker (ctrl+o, w)
	modeMenu // the ! menu: plugin actions and links
	// the model picker (ctrl+g)
	modeModel
	modeEdit    // a row edited in place (m.edit)
	modeScope   // what search looks in (ctrl+g in search)
	modeActions // the agents' actions (U): j/k, enter undoes
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
	// back is the agent you just came back from (attach) while the cursor
	// still sits on it: the inbox doesn't hold it, so once it no longer needs
	// you it drops and the cursor lands on the next item
	back string

	view viewKind
	opts layout.ViewOpts
	srch searchState // the search view
	help bool

	mode          mode
	input         string
	inputPrompt   string
	inputAction   func(m *model, s string) tea.Cmd
	edit          *inlineEdit // what modeEdit edits
	confirmMsg    string
	confirmKey    string // the action's key: pressed again, it confirms
	confirmDo     func(m *model) tea.Cmd
	filterBefore  string
	menu          []menuItem     // the ! menu's items while modeMenu
	actionsSeen   time.Time      // the agents' action log's mtime at the last look (noticeActions)
	actionsLooked bool           // noticeActions ran once: what was there before isn't news
	acts          []group.Action // the U list while modeActions, newest first
	actSel        int            // its selection
	menuTitle     string

	panelTab string   // the agent box's tab (, and .): "" or "agent", or a plugin's name; kept across agents
	tabHits  []tabHit // where the tabs were drawn, for clicks
	tabRow   int      // the panel line they are on

	cut []string // nodes picked up with m, put with p / P

	earlierOpen bool            // the inbox's "earlier days" fold is open (folded each time regia opens)
	earlier     []*claude.Agent // what that fold holds, as last built

	selectID string // just started: select it as soon as it's listed

	selecting bool              // select mode (v): space marks rows for e / x
	marked    map[string]bool   // agent ids marked in select mode
	markCount map[string][2]int // markCounts, built once a frame

	removing map[string]*pendingRemoval // taken off the list while claude rm runs
	retiring map[string]bool            // swapped itself out: its removal is running (retireSwapped)
	pending  *pendingStart              // a session a plugin started, until claude agents lists it

	scanning  bool // a transcript scan for missed spawns is running
	scannedAt int  // the tick the last one started

	// geometry of the last frame, for the mouse
	lw, vis  int
	dragging bool // dragging the divider to resize the list
	// where the left button went down in the panel; the click happens on its
	// release there (see updateMouse)
	pressed        bool
	pressX, pressY int

	// the agent panel's conversation (conv.go): fold state and cursor by
	// agent, whether it has the keys (o), what the last frame drew of it
	convs      map[string]*convState
	convFocus  bool
	convReveal bool // the cursor moved: scroll it into view at the next frame
	conv       convShown
	live       int    // bumped when any agent's state or name changes (liveSig): steps may show them
	liveSig    uint64 // a hash of every agent's id, state and name at the last refresh

	panelScroll int    // lines the panel's conversation is scrolled back (pgup/pgdn)
	panelFor    string // the row panelScroll belongs to
	timeline    bool   // T: a group's or coordinator's panel shows its timeline

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
	inHist      *inputHist  // moving through earlier messages (↑), when not nil
	draftSaved  string      // the draft as last written to its file

	draftModel string       // model picked for this task (ctrl+g); "" = claude's default
	modelPick  *modelPicker // the model picker, when open
	scopeSel   int          // the search scope picker's selection

	status   string
	statusAt time.Time

	dwellID string // agent selected since dwellAt (to notice you looked at it)
	dwellAt time.Time

	updateNote string // set once a background update ran; shown in the header

	hist    claude.History
	histFor string
	chg     changesState // the changes tab (D): its cursor and whether it has the keys

	fetching  bool
	fetchErr  error
	ticks     int
	listStamp uint64 // claude.ListingStamp when the listing was last asked for
	fetchedAt int    // the tick it was asked at
	pulsing   bool   // the working-icon animation is ticking
	pulseN    int    // its frame

	gen        int // counts messages other than pulses: what's drawn from state is good while it stays
	groupPanel struct {
		id      string
		w, gen  int
		plugins int    // plugins.Version when drawn
		tab     string // the plugin tab shown
		lines   []string
	}
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

// attachSession hands the terminal to an agent's session until you detach
// (a var so tests can see the attach without running it).
var attachSession = func(id string) tea.Cmd {
	return tea.Exec(attach.Session(id), func(err error) tea.Msg { return attachDoneMsg{id, err} })
}

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

// Listing poll: `claude agents` runs when something it reports may have
// changed (see claude.ListingStamp), at most every listMin ticks, and at
// least every listMax (a session that dies writes nothing; `regia context`
// trusts the cached listing for 20 s).
const (
	listMin = 2
	listMax = 10
)

func (m *model) pollListing() tea.Cmd {
	if m.fetching {
		return nil
	}
	since := m.ticks - m.fetchedAt
	if since < listMin {
		return nil
	}
	st := claude.ListingStamp(m.agents)
	if st == m.listStamp && since < listMax {
		return nil
	}
	m.fetching, m.listStamp, m.fetchedAt = true, st, m.ticks
	return fetchCmd
}

// Spawn scan: the hook links a spawn when it sees it, but sessions started
// before the plugin was installed never run it. Their transcripts are read
// for spawns on a goroutine (a tea.Cmd), only what was appended since the
// last scan; the scanner only appends to spawns.jsonl, and the next refresh
// picks the links up (spawns.Cached, Place). One scan at a time.
const scanEvery = 15 // ticks

var spawnScanner *spawns.Scanner // made on the first scan; one scan at a time

type spawnScanMsg struct{ added int }

func spawnScanCmd() tea.Msg {
	if spawnScanner == nil {
		spawnScanner = spawns.NewScanner()
	}
	n, _ := spawnScanner.Scan()
	return spawnScanMsg{n}
}

func (m *model) pollScan() tea.Cmd {
	if m.scanning || m.ticks-m.scannedAt < scanEvery {
		return nil
	}
	m.scanning, m.scannedAt = true, m.ticks
	return spawnScanCmd
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Init() tea.Cmd {
	m.fetching, m.scanning = true, true
	return tea.Batch(fetchCmd, tick(), autoUpdateCmd, m.startPulse(), spawnScanCmd)
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
	m.dropRemoving(next)
	m.agents = next
	m.refresh()
}

// prune lets the layout forget agents gone from `claude agents` for a week
// (layout.Prune), after each fresh listing.
func (m *model) prune() {
	m.syncLayout()
	if m.layout.Prune(m.agents, time.Now()) {
		m.saveLayoutQuiet()
	}
}

// refresh re-reads job state files and rebuilds rows, keeping the cursor on
// the same node.
func (m *model) refresh() {
	m.syncLayout()
	for _, a := range m.agents {
		claude.Enrich(a)
	}
	m.classify(time.Now())
	m.checkHealth(time.Now())
	m.noteLive()
	m.noticeActions()
	m.releaseDone()
	m.layout.Spawns = spawns.Cached()
	placed := m.layout.Place(m.agents)
	if m.layout.Revive(m.agents) || placed { // archived but working again: back in the tree
		_ = m.layout.Save()
	}
	m.rebuild()
}

func (m *model) rebuild() {
	m.opts.Keep = m.cursorID
	if m.view == viewInbox && m.back == m.cursorID {
		m.opts.Keep = "" // answered (now or at a later refresh): move on
	}
	if m.view == viewInbox {
		m.rows = m.inboxRows(time.Now())
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
	m.back = ""
	m.panelScroll = 0
	m.convFocus, m.chg.focus = false, false
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

// saveLayoutQuiet saves without rebuilding the rows (for use while they're
// being rebuilt).
func (m *model) saveLayoutQuiet() {
	if err := m.layout.Save(); err != nil {
		m.flash("save failed: %v", err)
	}
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
