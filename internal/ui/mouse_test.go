package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// clickPanelAt clicks column x, row y of the panel.
func clickPanelAt(m *model, x, y int) tea.Cmd {
	_, cmd := m.Update(tea.MouseMsg{X: padX + m.lw + gap + x, Y: listTop + y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
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

func TestClickRunSelectsThenUnfolds(t *testing.T) {
	m := convModel(t)
	y, i := rowOfBlock(t, m, bigRun)
	clickPanelAt(m, 10, y)
	if !m.convFocus || m.convSel() != i {
		t.Fatalf("a click selects the run and focuses (focus %v, sel %d want %d)", m.convFocus, m.convSel(), i)
	}
	if s := convScreen(m); strings.Contains(s, "go test") {
		t.Fatalf("the first click doesn't unfold:\n%s", s)
	}
	y, _ = rowOfBlock(t, m, bigRun)
	clickPanelAt(m, 10, y)
	if s := convScreen(m); !strings.Contains(s, "▾ ⚙ 7 steps") {
		t.Fatalf("a click on the selected run unfolds it:\n%s", s)
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
