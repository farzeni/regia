package ui

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Input history: pgup in the input brings back earlier messages, pgdown goes
// forward again and, past the newest, back to what you were typing.
//
// Typed text is never thrown away: what you send, what esc or ctrl+c clears,
// what is in the input when regia quits, and what was there when it was
// killed (the draft is saved to a file every second while it changes) all go
// into the history, kept in ~/.config/regia/history.jsonl and shared by every
// regia you run.

type histEntry struct {
	At     time.Time         `json:"at"`
	Text   string            `json:"text"`             // the draft, paste tokens folded
	Pastes map[string]string `json:"pastes,omitempty"` // what its tokens stand for
	Unsent bool              `json:"unsent,omitempty"` // cleared or left behind, not sent
}

// inputHist is the history while you move through it.
type inputHist struct {
	list  []histEntry       // oldest first, as loaded when browsing started
	idx   int               // the entry shown: len(list) is the draft you were typing
	draft histEntry         // what you were typing before pgup
	edits map[int]histEntry // entries you changed while browsing, kept until you send
	shown string            // the text put in the input, to tell when you edit it
}

const (
	histKeep    = 1000    // entries loaded
	histMaxSize = 8 << 20 // the file is trimmed to histKeep entries past this
)

func histDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "regia")
}

func histPath() string { return filepath.Join(histDir(), "history.jsonl") }

// draftPath is where this regia keeps the draft it's typing, in case it dies.
func draftPath(pid int) string {
	return filepath.Join(histDir(), "drafts", strconv.Itoa(pid)+".json")
}

// loadHist reads the history, oldest first, the same message twice in a row
// kept once.
func loadHist() []histEntry {
	f, err := os.Open(histPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	var list []histEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4*maxPrompt+(1<<20))
	for sc.Scan() {
		var e histEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil || strings.TrimSpace(e.Text) == "" {
			continue
		}
		if n := len(list); n > 0 && list[n-1].Text == e.Text {
			list[n-1] = e // the latest one, sent wins over unsent
			continue
		}
		list = append(list, e)
	}
	if len(list) > histKeep {
		list = list[len(list)-histKeep:]
	}
	return list
}

// appendHist adds an entry to the history file. Errors are returned so the
// caller can say the text could not be kept.
func appendHist(e histEntry) error {
	if strings.TrimSpace(e.Text) == "" {
		return nil
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(histDir(), 0o700); err != nil {
		return err
	}
	p := histPath()
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n')) // one write: lines from two regias don't mix
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if st, serr := os.Stat(p); err == nil && serr == nil && st.Size() > histMaxSize {
		trimHist()
	}
	return err
}

// trimHist rewrites the file with its last histKeep entries.
func trimHist() {
	list := loadHist()
	var sb strings.Builder
	for _, e := range list {
		b, _ := json.Marshal(e)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	tmp := histPath() + ".tmp"
	if os.WriteFile(tmp, []byte(sb.String()), 0o600) == nil {
		os.Rename(tmp, histPath())
	}
}

// entry is the draft as it is now.
func (m *model) draftEntry() histEntry {
	return histEntry{Text: m.draft(), Pastes: m.usedPastes(m.draft())}
}

// usedPastes is the pastes whose tokens are still in s.
func (m *model) usedPastes(s string) map[string]string {
	var ps map[string]string
	for tok, v := range m.pastes {
		if strings.Contains(s, tok) {
			if ps == nil {
				ps = map[string]string{}
			}
			ps[tok] = v
		}
	}
	return ps
}

// keepDraft saves the draft in the history as unsent, before it is cleared.
func (m *model) keepDraft() bool {
	e := m.draftEntry()
	if strings.TrimSpace(e.Text) == "" {
		return false
	}
	e.Unsent = true
	if err := appendHist(e); err != nil {
		m.flash("could not keep the draft in the history: %v", err)
		return false
	}
	return true
}

// histStep moves through the history: d=-1 older, d=1 newer. It says whether
// it did anything (false: nothing older / not browsing).
func (m *model) histStep(d int) bool {
	h := m.inHist
	if h == nil {
		if d > 0 {
			return false
		}
		list := loadHist()
		if len(list) == 0 {
			m.flash("no earlier messages")
			return true
		}
		h = &inputHist{list: list, idx: len(list), draft: m.draftEntry(), edits: map[int]histEntry{}}
		m.inHist = h
	}
	to := h.idx + d
	if to < 0 {
		m.flash("oldest message (%d)", len(h.list))
		return true
	}
	if to > len(h.list) {
		return false
	}
	// keep what you changed in the one you leave
	if cur := m.draftEntry(); cur.Text != h.shown {
		if h.idx == len(h.list) {
			h.draft = cur
		} else {
			h.edits[h.idx] = cur
		}
	}
	h.idx = to
	var e histEntry
	switch e2, ok := h.edits[to]; {
	case to == len(h.list):
		e = h.draft
	case ok:
		e = e2
	default:
		e = h.list[to]
	}
	m.showEntry(e)
	h.shown = e.Text
	if to == len(h.list) {
		m.inHist = nil // back on your draft
		m.flash("")
		return true
	}
	note := ""
	if e.Unsent {
		note = " · not sent"
	}
	if _, ok := h.edits[to]; ok {
		note += " · edited"
	}
	m.flash("message %d/%d · %s%s · pgdown newer", to+1, len(h.list), histAgo(e.At), note)
	return true
}

// showEntry puts e in the input, its paste tokens working again.
func (m *model) showEntry(e histEntry) {
	if len(e.Pastes) > 0 {
		if m.pastes == nil {
			m.pastes = map[string]string{}
		}
		for k, v := range e.Pastes {
			m.pastes[k] = v
		}
		m.bumpPasteN()
	}
	m.ta.SetValue(e.Text) // the cursor ends up at the end
	m.comp, m.compShut = nil, e.Text
}

var pasteNum = regexp.MustCompile(`^\[(Pasted text|Image) #(\d+)`)

// bumpPasteN keeps new paste tokens from reusing the number of one brought back.
func (m *model) bumpPasteN() {
	for tok := range m.pastes {
		if g := pasteNum.FindStringSubmatch(tok); g != nil {
			n, _ := strconv.Atoi(g[2])
			if g[1] == "Image" {
				m.imageN = max(m.imageN, n)
			} else {
				m.pasteN = max(m.pasteN, n)
			}
		}
	}
}

func histAgo(t time.Time) string {
	if s := ago(t); s != "" {
		return s + " ago"
	}
	return "earlier"
}

// ---- the draft on disk, in case regia is killed

// saveDraftFile writes the draft to this regia's draft file (or removes it
// when the draft is empty). Called on ticks when the draft changed.
func (m *model) saveDraftFile() {
	e := m.draftEntry()
	if e.Text == m.draftSaved {
		return
	}
	p := draftPath(os.Getpid())
	if strings.TrimSpace(e.Text) == "" {
		if err := os.Remove(p); err == nil || os.IsNotExist(err) {
			m.draftSaved = e.Text
		}
		return
	}
	e.At, e.Unsent = time.Now(), true
	b, _ := json.Marshal(e)
	os.MkdirAll(filepath.Dir(p), 0o700)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, p) == nil {
		m.draftSaved = e.Text
	}
}

// recoverDrafts moves the drafts of regias that are no longer running into
// the history, and returns the newest, to put back in the input.
func recoverDrafts() (newest *histEntry) {
	dir := filepath.Join(histDir(), "drafts")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, de := range ents {
		name := de.Name()
		pid, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
		if err != nil || !strings.HasSuffix(name, ".json") || (pid != os.Getpid() && alive(pid)) {
			continue
		}
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var e histEntry
		if json.Unmarshal(b, &e) != nil || strings.TrimSpace(e.Text) == "" {
			os.Remove(p)
			continue
		}
		if appendHist(e) != nil {
			continue // keep the file: next time
		}
		os.Remove(p)
		if newest == nil || e.At.After(newest.At) {
			newest = &e
		}
	}
	return newest
}

// alive says whether process pid is running.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}

// finishDraft runs when regia quits: the draft goes into the history and its
// file away.
func (m *model) finishDraft() {
	if strings.TrimSpace(m.draft()) == "" || m.keepDraft() {
		os.Remove(draftPath(os.Getpid()))
	} // else the draft file stays, and the next regia brings it back
}
