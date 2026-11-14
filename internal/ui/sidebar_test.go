package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
)

// manyAgents: n waiting agents in one project, agent i started i minutes
// after the first (so the last one is the most recent).
func manyAgents(t *testing.T, n int) *model {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir() + "/Projects/many"
	var js []string
	for i := range n {
		js = append(js, fmt.Sprintf(`{"id":"c%07d","cwd":"%s","kind":"background","name":"agent %02d","status":"idle","state":"blocked","startedAt":%d}`,
			i, dir, i, 1794645285576+int64(i)*60000))
	}
	m := newModel()
	m.w, m.h = 140, 30
	m.setAgents([]byte("[" + strings.Join(js, ",") + "]"))
	m.View()
	return m
}

func TestInboxNewestFirst(t *testing.T) {
	m := manyAgents(t, 5)
	m.Update(tkey("tab"))
	if m.view != viewInbox || len(m.rows) != 5 {
		t.Fatalf("inbox rows %v", rowIDs(m))
	}
	if m.rows[0].Title != "agent 04" || m.rows[4].Title != "agent 00" {
		t.Fatalf("inbox should list the most recent first: %v", rowIDs(m))
	}
}

func TestSidebarWidth(t *testing.T) {
	m := manyAgents(t, 3)
	m.View()
	if m.lw < 28 || m.lw > 48 {
		t.Fatalf("default width %d, want about a third of 138", m.lw)
	}
	def := m.lw
	m.Update(tkey("-"))
	m.View()
	if m.lw != def-4 {
		t.Fatalf("- : width %d, want %d", m.lw, def-4)
	}
	m.Update(tkey("+"))
	m.Update(tkey("+"))
	m.View()
	if m.lw != def+4 {
		t.Fatalf("+ + : width %d, want %d", m.lw, def+4)
	}
	if l := layout.Load(); l.Sidebar != def+4 {
		t.Fatalf("width not saved: %d", l.Sidebar)
	}
	for range 40 {
		m.Update(tkey("-"))
	}
	m.View()
	if m.lw != minSidebar {
		t.Fatalf("width should stop at %d, got %d", minSidebar, m.lw)
	}
	for range 60 {
		m.Update(tkey("+"))
	}
	m.View()
	if dw := m.w - 2*padX - m.lw - gap; dw < 30 {
		t.Fatalf("the panel should keep 30 columns, has %d", dw)
	}
}

func TestMouse(t *testing.T) {
	m := manyAgents(t, 40)
	m.View()
	// click the 5th list line
	m.Update(tea.MouseMsg{X: 5, Y: listTop + 4, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.cursor != m.offset+4 {
		t.Fatalf("click: cursor %d, want %d", m.cursor, m.offset+4)
	}
	at := m.cursor
	m.Update(tea.MouseMsg{X: 5, Y: 10, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.cursor != at+3 {
		t.Fatalf("wheel over the list: cursor %d, want %d", m.cursor, at+3)
	}
	m.Update(tea.MouseMsg{X: m.lw + 20, Y: 10, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.cursor != at+3 || m.panelScroll != 3 {
		t.Fatalf("wheel over the panel: cursor %d, panelScroll %d", m.cursor, m.panelScroll)
	}
	// drag the divider 10 columns right
	div := padX + m.lw + gap/2
	w0 := m.lw
	m.Update(tea.MouseMsg{X: div, Y: 10, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: div + 10, Y: 10, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m.Update(tea.MouseMsg{X: div + 10, Y: 10, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m.View()
	if m.lw != w0+10 || m.dragging {
		t.Fatalf("drag: width %d, want %d", m.lw, w0+10)
	}
	if l := layout.Load(); l.Sidebar != w0+10 {
		t.Fatal("dragged width not saved")
	}
}

func TestScrollKeepsContext(t *testing.T) {
	m := manyAgents(t, 60)
	m.View()
	for range 30 {
		m.Update(tkey("j"))
		m.View()
		if below := m.offset + m.vis - 1 - m.cursor; below < scrollOff && m.offset+m.vis < len(m.rows) {
			t.Fatalf("cursor %d: only %d rows visible below it", m.cursor, below)
		}
	}
	for range 10 {
		m.Update(tkey("k"))
		m.View()
		if above := m.cursor - m.offset; above < scrollOff && m.offset > 0 {
			t.Fatalf("cursor %d: only %d rows visible above it", m.cursor, above)
		}
	}
}

func TestSelectionBarFullRow(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("●") + " title " + stDim.Render("5h")
	bar := selBar(styled, 30)
	r := stSel.Render("x")
	bg := r[:strings.Index(r, "x")]
	body := strings.TrimSuffix(bar, "\x1b[0m")
	if !strings.HasPrefix(bar, bg) {
		t.Fatal("bar should start with the background")
	}
	for i := strings.Index(body, "\x1b[0m"); i >= 0; {
		if !strings.HasPrefix(body[i+4:], bg) {
			t.Fatalf("background not restored after a reset at %d: %q", i, body)
		}
		j := strings.Index(body[i+4:], "\x1b[0m")
		if j < 0 {
			break
		}
		i += 4 + j
	}
	if w := ansi.StringWidth(bar); w != 30 {
		t.Fatalf("bar width %d, want 30", w)
	}
}

func TestPanelDivider(t *testing.T) {
	m := testModel(t)
	m.Update(tkey("esc"))
	m.moveCursor(1)
	r := m.selected()
	at := time.Now().Add(-time.Minute)
	m.hist, m.histFor = claude.History{Task: "the task", Turns: []claude.Turn{{
		User: &claude.HEntry{At: at, Detail: "do it"}, Results: []claude.HEntry{{At: at, State: "done", Detail: "did it"}}, LastAt: at,
	}}}, r.ID
	ls := strings.Split(ansi.Strip(m.View()), "\n")
	for i, l := range ls {
		if strings.Contains(l, "did it") {
			if !strings.Contains(ls[i+1], "─────") || !strings.Contains(ls[i+2], "task") {
				t.Fatalf("want a rule between the history and the card:\n%s", strings.Join(ls[i:i+3], "\n"))
			}
			return
		}
	}
	t.Fatal("history not shown")
}

func TestYourMessagesTinted(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	m := testModel(t)
	m.Update(tkey("esc"))
	m.moveCursor(1)
	r := m.selected()
	at := time.Now().Add(-time.Minute)
	m.hist, m.histFor = claude.History{Turns: []claude.Turn{{
		User: &claude.HEntry{At: at, Detail: "my message"}, Results: []claude.HEntry{{At: at, State: "done", Detail: "agent reply"}}, LastAt: at,
	}}}, r.ID
	hist, _ := m.agentDetail(r, 60)
	fg := "\x1b[38;5;183m"
	for _, l := range hist {
		if strings.Contains(l, "\x1b[48;") {
			t.Fatalf("no background band in the history: %q", l)
		}
		switch {
		case strings.Contains(l, "my message"):
			if !strings.Contains(l, fg+"my message") {
				t.Fatalf("your message should have its own text color: %q", l)
			}
		case strings.Contains(l, "agent reply"):
			if strings.Contains(l, fg) {
				t.Fatalf("the agent's line should not take your color: %q", l)
			}
		}
	}
}
