package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// clickPanelAt clicks column x, row y of the panel: press, then release.
func clickPanelAt(m *model, x, y int) tea.Cmd {
	e := tea.MouseMsg{X: padX + m.lw + gap + x, Y: listTop + y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	m.Update(e)
	e.Action = tea.MouseActionRelease
	_, cmd := m.Update(e)
	return cmd
}

// rowOfBlock is the panel row a block's first line is drawn at.
func rowOfBlock(t *testing.T, m *model, f func(b convBlock) bool) (int, int) {
	t.Helper()
	m.View()
	for y := range m.h {
		if i := m.convBlockAtRow(y); i >= 0 && f(m.conv.blocks[i]) {
			return y, i
		}
	}
	t.Fatalf("no such block on screen:\n%s", ansi.Strip(m.View()))
	return 0, 0
}

// A run and the agent's reply after it are one item: enter folds its steps,
// c copies the reply; a click on the reply's text only selects, a click on
// the run line folds / unfolds.
func TestRunAndReplyOneItem(t *testing.T) {
	m := convModel(t)
	var got string
	prev := copyText
	copyText = func(s string) error { got = s; return nil }
	t.Cleanup(func() { copyText = prev })
	m.Update(tkey("o"))
	selectWhere(t, m, bigRun)
	b := m.conv.blocks[m.convSel()]
	if b.kind != blockMsg || b.msg.Text != "Done: tests fixed." {
		t.Fatalf("the 7-step run and its reply should be one item: %+v", b)
	}
	m.Update(tkey("enter"))
	if s := convScreen(m); !strings.Contains(s, "▾ ⚙ 7 steps") {
		t.Fatalf("enter on the item unfolds its steps:\n%s", s)
	}
	m.Update(tkey("c"))
	if got != "Done: tests fixed." {
		t.Fatalf("c on the item copies the reply: %q", got)
	}
	m.Update(tkey("enter")) // fold again
	m.Update(tkey("esc"))
	// a click on the reply's text: selected, not unfolded
	y := -1
	for row := range m.h {
		if i := m.convBlockAtRow(row); i >= 0 && bigRun(m.conv.blocks[i]) && strings.Contains(ansi.Strip(m.View()), "Done: tests fixed.") {
			if l := m.conv.histStart + row - m.conv.histRow; l > m.conv.blocks[i].top {
				y = row
				break
			}
		}
	}
	if y < 0 {
		t.Fatal("the reply's text is not on screen")
	}
	clickPanelAt(m, 10, y)
	clickPanelAt(m, 10, y)
	if s := convScreen(m); !m.convFocus || !bigRun(m.conv.blocks[m.convSel()]) || strings.Contains(s, "▾ ⚙ 7 steps") {
		t.Fatalf("clicks on the reply's text select the item and fold nothing:\n%s", s)
	}
}

// A click in the panel acts on its release: a terminal that opens a link on
// cmd/ctrl+click (Ghostty) sends the press and swallows the release, and the
// press alone must not select, unfold or scroll the link away. A drag (the
// release elsewhere) is no click either.
func TestPanelClickOnRelease(t *testing.T) {
	m := convModel(t)
	y, _ := rowOfBlock(t, m, bigRun)
	press := tea.MouseMsg{X: padX + m.lw + gap + 10, Y: listTop + y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	m.Update(press)
	if s := convScreen(m); m.convFocus || strings.Contains(s, "▾ ⚙ 7 steps") {
		t.Fatalf("the press alone does nothing:\n%s", s)
	}
	m.Update(press) // the next press: the swallowed release left nothing pending
	rel := press
	rel.Action, rel.Y = tea.MouseActionRelease, press.Y+1
	m.Update(rel)
	if m.convFocus {
		t.Fatal("a release away from the press (a drag) is no click")
	}
	m.Update(press)
	rel.Y = press.Y
	m.Update(rel)
	if s := convScreen(m); !m.convFocus || !strings.Contains(s, "▾ ⚙ 7 steps") {
		t.Fatalf("press + release on the steps line: selected and unfolded:\n%s", s)
	}
}

// The first click on a run selects and unfolds it; the next folds it.
func TestClickRunSelectsAndUnfolds(t *testing.T) {
	m := convModel(t)
	y, i := rowOfBlock(t, m, bigRun)
	clickPanelAt(m, 10, y)
	if !m.convFocus || m.convSel() != i {
		t.Fatalf("a click selects the run and focuses (focus %v, sel %d want %d)", m.convFocus, m.convSel(), i)
	}
	if s := convScreen(m); !strings.Contains(s, "▾ ⚙ 7 steps") {
		t.Fatalf("the first click unfolds the run too:\n%s", s)
	}
	y, _ = rowOfBlock(t, m, bigRun)
	clickPanelAt(m, 10, y)
	if s := convScreen(m); !strings.Contains(s, "▸ ⚙ 7 steps") {
		t.Fatalf("a click on the selected run folds it:\n%s", s)
	}
	// a message only gets selected
	y, i = rowOfBlock(t, m, func(b convBlock) bool { return b.kind == blockMsg })
	clickPanelAt(m, 10, y)
	if m.convSel() != i {
		t.Fatalf("a click selects the message (sel %d want %d)", m.convSel(), i)
	}
}

func TestClickTabSwitches(t *testing.T) {
	m := editsModel(t)
	m.View()
	var x = -1
	for _, h := range m.tabHits {
		if h.name == changesTab {
			x = h.x0
		}
	}
	if x < 0 {
		t.Fatalf("no changes tab: %+v", m.tabHits)
	}
	clickPanelAt(m, x, m.tabRow)
	if m.panelTab != changesTab {
		t.Fatalf("click on the changes tab: %q", m.panelTab)
	}
	clickPanelAt(m, m.tabHits[0].x0, m.tabRow)
	if m.panelTab != agentTab {
		t.Fatalf("click on the agent tab: %q", m.panelTab)
	}
}

func TestClickChangedFile(t *testing.T) {
	m := editsModel(t)
	got := replaceOpen(t)
	m.panelTab = changesTab
	m.View()
	clickPanelAt(m, 5, m.tabRow+2) // the second file: b.go
	if !m.chg.focus || m.chg.sel != filepath.Join(m.agents["aaaa1111"].Cwd, "b.go") || len(*got) != 0 {
		t.Fatalf("a click selects b.go and gives the tab the keys (focus %v, sel %q, opened %v)", m.chg.focus, m.chg.sel, *got)
	}
	m.View()
	clickPanelAt(m, 5, m.tabRow+2)
	if len(*got) != 1 || (*got)[0].Kind != "file" || filepath.Base((*got)[0].Path) != "b.go" {
		t.Fatalf("a click on the selected file opens it: %+v", *got)
	}
	clickPanelAt(m, 5, m.tabRow+3) // the totals line: nothing
	if len(*got) != 1 {
		t.Fatal("the totals line opens nothing")
	}
}

func TestWheelScrollsKeepsCursor(t *testing.T) {
	m := convModel(t)
	m.h = 22
	m.View()
	wheel := func(b tea.MouseButton) {
		m.Update(tea.MouseMsg{X: padX + m.lw + gap + 10, Y: listTop + 10, Button: b, Action: tea.MouseActionPress})
		m.View()
	}
	wheel(tea.MouseButtonWheelUp)
	if m.panelScroll != 3 {
		t.Fatalf("the wheel scrolls the conversation back 3 lines: %d", m.panelScroll)
	}
	wheel(tea.MouseButtonWheelDown)
	if m.panelScroll != 0 {
		t.Fatalf("and forward: %d", m.panelScroll)
	}
	m.Update(tkey("o")) // focus: the cursor on the last block
	m.View()
	for range 5 {
		wheel(tea.MouseButtonWheelUp)
	}
	b := m.conv.blocks[m.convSel()]
	if b.top >= m.conv.histEnd || b.top+b.n <= m.conv.histStart {
		t.Fatalf("the selected block %d+%d left the view [%d,%d):\n%s", b.top, b.n, m.conv.histStart, m.conv.histEnd, convScreen(m))
	}
	if m.convSel() == len(m.conv.blocks)-1 {
		t.Fatal("the cursor should have moved up with the view")
	}
}

// A drag in the panel selects text there only (not the list's), highlights
// it and copies it on the release.
func TestPanelDragSelectsText(t *testing.T) {
	prevP := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256) // the highlight shows
	t.Cleanup(func() { lipgloss.SetColorProfile(prevP) })
	m := convModel(t)
	var got string
	prev := copyText
	copyText = func(s string) error { got = s; return nil }
	t.Cleanup(func() { copyText = prev })
	y := -1
	for row, l := range strings.Split(convScreen(m), "\n") {
		if strings.Contains(l, "Done: tests fixed.") {
			y = row - listTop
		}
	}
	if y < 0 {
		t.Fatalf("no reply on screen:\n%s", convScreen(m))
	}
	x0 := padX + m.lw + gap
	ev := func(a tea.MouseAction, x, y int) {
		m.Update(tea.MouseMsg{X: x0 + x, Y: listTop + y, Button: tea.MouseButtonLeft, Action: a})
	}
	ev(tea.MouseActionPress, 1, y)
	ev(tea.MouseActionMotion, 3, y)
	ev(tea.MouseActionMotion, 4, y)
	if s := m.View(); !strings.Contains(s, stDragSel.Render("Done")) {
		t.Fatalf("the dragged text is highlighted:\n%s", convScreen(m))
	}
	ev(tea.MouseActionRelease, 4, y)
	if got != "Done" || !m.convFocus {
		t.Fatalf("the release copies the text and focuses the conversation: %q (focus %v)", got, m.convFocus)
	}
	// two lines: from the line above to the reply's end
	ev(tea.MouseActionPress, 0, y-1)
	ev(tea.MouseActionMotion, 60, y)
	ev(tea.MouseActionRelease, 60, y)
	if !strings.HasSuffix(got, "Done: tests fixed.") || !strings.Contains(got, "\n") || strings.Contains(got, "regia") {
		t.Fatalf("the lines dragged over, the panel's only: %q", got)
	}
	ev(tea.MouseActionPress, 1, y) // a new press clears it
	if strings.Contains(m.View(), stDragSel.Render("Done")) {
		t.Fatalf("a press clears the selection: %+v\n%q", m.conv.drag, m.View())
	}
	ev(tea.MouseActionRelease, 1, y)
}

// r after a drag quotes the text dragged over, not the whole block; then r
// quotes blocks again.
func TestQuoteDragSelection(t *testing.T) {
	m := convModel(t)
	prev := copyText
	copyText = func(string) error { return nil }
	t.Cleanup(func() { copyText = prev })
	y := -1
	for row, l := range strings.Split(convScreen(m), "\n") {
		if strings.Contains(l, "Done: tests fixed.") {
			y = row - listTop
		}
	}
	x0 := padX + m.lw + gap
	ev := func(a tea.MouseAction, x int) {
		m.Update(tea.MouseMsg{X: x0 + x, Y: listTop + y, Button: tea.MouseButtonLeft, Action: a})
	}
	ev(tea.MouseActionPress, 7)
	ev(tea.MouseActionMotion, 17)
	ev(tea.MouseActionRelease, 17)
	m.Update(tkey("r"))
	if got := m.ta.Value(); got != "> tests fixed\n\n" || !m.typing || m.convFocus {
		t.Fatalf("r quotes the selection in the input: %q (typing %v)", got, m.typing)
	}
	if m.conv.drag.on {
		t.Fatal("quoted: the selection is gone")
	}
}
