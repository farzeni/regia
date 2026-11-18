package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Selecting text in the panel: the terminal's own selection runs across the
// whole screen, the list included, so regia selects the panel's history
// itself. A drag highlights from where the button went down to the pointer,
// and the release copies the text. (shift+drag still selects the terminal's
// way.)

// textSel is a drag over the panel's history: lines of m.conv.hist, columns
// of the panel.
type textSel struct {
	on   bool
	id   string // the row whose panel it was dragged on
	from selPos // where the button went down
	to   selPos // the pointer
}

type selPos struct{ line, col int }

// selAt is the history line and column under panel column x, row y, kept
// inside the history shown; ok is false when the row is above it (the card).
func (m *model) selAt(x, y int) (selPos, bool) {
	c := &m.conv
	if c.histEnd <= c.histStart || y < c.histRow {
		return selPos{}, false
	}
	l := min(c.histStart+y-c.histRow, c.histEnd-1)
	return selPos{l, max(0, x)}, true
}

// dragTo starts or extends a drag from the press at (px, py) to (x, y),
// both panel coordinates. It scrolls when the pointer goes past the history.
func (m *model) dragTo(px, py, x, y int) {
	s := &m.conv.drag
	if !s.on {
		p, ok := m.selAt(px, py)
		if !ok {
			return
		}
		*s = textSel{on: true, id: m.panelFor, from: p}
	}
	if y < m.conv.histRow && m.conv.histStart > 0 {
		m.scrollConv(1)
	} else if y >= m.conv.histRow+m.conv.histEnd-m.conv.histStart {
		m.scrollConv(-1)
	}
	y = max(y, m.conv.histRow)
	if p, ok := m.selAt(x, y); ok {
		s.to = p
	}
}

// span is the selection in order, start first.
func (s textSel) span() (selPos, selPos) {
	a, b := s.from, s.to
	if b.line < a.line || b.line == a.line && b.col < a.col {
		a, b = b, a
	}
	return a, b
}

// cols is the column range [from, to) the selection takes on line l, of w.
func (s textSel) cols(l, w int) (int, int, bool) {
	a, b := s.span()
	if l < a.line || l > b.line {
		return 0, 0, false
	}
	from, to := 0, w
	if l == a.line {
		from = a.col
	}
	if l == b.line {
		to = min(w, b.col+1)
	}
	return from, to, from < to
}

// dragText is the selected text: each line's columns, without the block
// gutter and trailing spaces.
func (m *model) dragText() string {
	s := m.conv.drag
	a, b := s.span()
	var out []string
	for l := a.line; l <= b.line && l < len(m.conv.hist); l++ {
		plain := ansi.Strip(m.conv.hist[l])
		from, to, _ := s.cols(l, ansi.StringWidth(plain))
		from = max(from, 1) // column 0 is the block gutter (the cursor's bar)
		out = append(out, strings.TrimRight(ansi.Cut(plain, from, max(from, to)), " "))
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

// endDrag copies what the drag selected.
func (m *model) endDrag() {
	s := &m.conv.drag
	t := m.dragText()
	if t == "" {
		s.on = false
		return
	}
	if err := copyText(t); err != nil {
		m.flash("clipboard: %v", err)
		return
	}
	n := strings.Count(t, "\n") + 1
	if n == 1 {
		m.flash("copied %d characters", len([]rune(t)))
	} else {
		m.flash("copied %d lines", n)
	}
}

// highlightDrag shows the selection on the history lines in view (line start
// of the history first).
func (m *model) highlightDrag(view []string, start int) {
	s := m.conv.drag
	if !s.on || s.id != m.panelFor {
		return
	}
	for i, line := range view {
		w := ansi.StringWidth(line)
		from, to, ok := s.cols(start+i, w)
		if !ok {
			continue
		}
		view[i] = ansi.Cut(line, 0, from) + stDragSel.Render(ansi.Strip(ansi.Cut(line, from, to))) + ansi.Cut(line, to, w)
	}
}
