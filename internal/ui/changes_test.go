package ui

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// editsModel: alpha edited a.go twice (+2 −1 at line 4, then +1 at line 11),
// wrote b.go (3 new lines), and an edit of c.go failed.
func editsModel(t *testing.T) *model {
	t.Helper()
	m := testModel(t)
	m.Update(tkey("esc"))
	goTo(t, m, "aaaa1111")
	cwd := m.agents["aaaa1111"].Cwd
	j := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	n := 0
	step := func(name, file string, use map[string]any, failed bool) []string {
		n++
		id := "toolu_" + string(rune('a'+n))
		ts := "2026-11-14T09:0" + string(rune('0'+n)) + ":00Z"
		res := map[string]any{"type": "tool_result", "tool_use_id": id, "content": "ok"}
		if failed {
			res["is_error"] = true
		}
		line := map[string]any{"type": "user", "timestamp": ts, "message": map[string]any{"content": []any{res}}}
		if use != nil {
			line["toolUseResult"] = use
		}
		return []string{
			j(map[string]any{"type": "assistant", "timestamp": ts, "message": map[string]any{"content": []any{
				map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{"file_path": file}},
			}}}),
			j(line),
		}
	}
	a, b, c := filepath.Join(cwd, "a.go"), filepath.Join(cwd, "b.go"), filepath.Join(cwd, "c.go")
	patch := func(start int, lines ...string) map[string]any {
		return map[string]any{"filePath": "", "structuredPatch": []any{map[string]any{
			"oldStart": start, "oldLines": 1, "newStart": start, "newLines": 1, "lines": lines}}}
	}
	p1 := patch(3, " ctx", "-old", "+new1", "+new2")
	p1["filePath"] = a
	p2 := patch(11, "+x")
	p2["filePath"] = a
	p3 := patch(1, "-gone", "+bad")
	p3["filePath"] = c
	var ls []string
	ls = append(ls, `{"type":"user","timestamp":"2026-11-14T09:00:00Z","message":{"content":"change things"}}`)
	ls = append(ls, step("Edit", a, p1, false)...)
	ls = append(ls, step("Write", b, map[string]any{"type": "create", "filePath": b, "content": "l1\nl2\nl3\n"}, false)...)
	ls = append(ls, step("Edit", a, p2, false)...)
	ls = append(ls, step("Edit", c, p3, true)...)
	ls = append(ls, `{"type":"assistant","timestamp":"2026-11-14T09:09:00Z","message":{"content":[{"type":"text","text":"Done."}]}}`)
	writeConv(t, m, ls...)
	return m
}

func TestChangesTabSumsEdits(t *testing.T) {
	m := editsModel(t)
	m.Update(tkey("D"))
	s := ansi.Strip(m.View())
	if !regexp.MustCompile(`a\.go:11 +\+3 −1 │`).MatchString(s) {
		t.Errorf("a.go: two edits summed, its last line, counts at the right:\n%s", s)
	}
	if !regexp.MustCompile(`b\.go:1 +\+3 │`).MatchString(s) {
		t.Errorf("b.go: a new file of 3 lines:\n%s", s)
	}
	if strings.Contains(s, "c.go") {
		t.Errorf("the failed edit isn't a change:\n%s", s)
	}
	if !strings.Contains(s, "2 files · +6 −1") || !strings.Contains(s, "changes 2") {
		t.Errorf("totals and the tab's badge:\n%s", s)
	}
	if strings.Index(s, "a.go") > strings.Index(s, "b.go") {
		t.Errorf("the last edited file comes first:\n%s", s)
	}
}

// Every line of the tab fits the box, on a narrow panel too; the hints go
// before the totals.
func TestChangesFitsNarrowBox(t *testing.T) {
	m := editsModel(t)
	m.Update(tkey("D"))
	for _, w := range []int{40, 60, 100} {
		_, card, _ := m.agentDetail(m.selected(), w)
		box := 0
		for _, l := range card {
			s := ansi.Strip(l)
			if !strings.HasPrefix(s, "╭") && !strings.HasPrefix(s, "│") && !strings.HasPrefix(s, "╰") {
				continue
			}
			box++
			if ansi.StringWidth(l) != w {
				t.Fatalf("at %d cols a box line is %d wide: %q", w, ansi.StringWidth(l), s)
			}
		}
		all := ansi.Strip(strings.Join(card, "\n"))
		if box < 5 || !strings.Contains(all, "2 files · +6 −1") || !strings.Contains(all, "+3 −1") {
			t.Fatalf("at %d cols the files and totals show:\n%s", w, all)
		}
		if w == 40 && strings.Contains(all, "esc back") {
			t.Fatalf("at 40 cols the hints give way to the totals:\n%s", all)
		}
	}
}

func TestChangesEmpty(t *testing.T) {
	m := testModel(t)
	m.Update(tkey("esc"))
	goTo(t, m, "aaaa1111")
	m.Update(tkey("D"))
	if s := ansi.Strip(m.View()); !strings.Contains(s, "no file changes yet") {
		t.Fatalf("empty state:\n%s", s)
	}
}

// replaceOpen records what openThing is asked to open.
func replaceOpen(t *testing.T) *[]openTarget {
	var got []openTarget
	orig := openThing
	t.Cleanup(func() { openThing = orig })
	openThing = func(o openTarget) tea.Cmd { got = append(got, o); return nil }
	return &got
}

func TestChangesKeys(t *testing.T) {
	m := editsModel(t)
	got := replaceOpen(t)
	m.Update(tkey("D"))
	if m.panelTab != changesTab || !m.chg.focus {
		t.Fatalf("D shows the changes tab with the keys (tab %q, focus %v)", m.panelTab, m.chg.focus)
	}
	m.View()
	m.Update(tkey("j"))
	if m.cursorID != "aaaa1111" {
		t.Fatal("j moves in the tab, not the list")
	}
	m.Update(tkey("enter"))
	cwd := m.agents["aaaa1111"].Cwd
	if len(*got) != 1 || (*got)[0] != (openTarget{Kind: "file", Path: filepath.Join(cwd, "b.go"), Line: 1}) {
		t.Fatalf("enter on b.go opens it at line 1, got %+v", *got)
	}
	if s := ansi.Strip(m.View()); !strings.Contains(s, "▸ b.go") {
		t.Fatalf("the cursor is on b.go:\n%s", s)
	}
	m.Update(tkey("k"))
	m.Update(tkey("enter"))
	if (*got)[1].Path != filepath.Join(cwd, "a.go") || (*got)[1].Line != 11 {
		t.Fatalf("k, enter opens a.go at 11: %+v", (*got)[1])
	}
	if _, cmd := m.Update(tkey("esc")); quits(cmd) || m.chg.focus || m.panelTab != changesTab {
		t.Fatal("esc gives the keys back, the tab stays")
	}
	m.Update(tkey("D"))
	m.Update(tkey("D"))
	if m.chg.focus || m.panelTab != agentTab {
		t.Fatalf("D again goes back to the agent tab (%q)", m.panelTab)
	}
	m.Update(tkey("D"))
	m.Update(tkey("j"))
	m.Update(tkey("esc"))
	m.Update(tkey("j"))
	if m.cursorID == "aaaa1111" || m.chg.focus {
		t.Fatal("after esc, j moves in the list again")
	}
}

func TestChangesFullDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	m := editsModel(t)
	got := replaceOpen(t)
	dir := m.agents["aaaa1111"].Cwd
	git := func(args ...string) string {
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "init")
	fork := git("rev-parse", "HEAD")
	git("checkout", "-qb", "agent")
	git("commit", "-q", "--allow-empty", "-m", "work")

	m.Update(tkey("D"))
	_, cmd := m.Update(tkey("d"))
	deliver(m, cmd)
	if len(*got) != 1 || (*got)[0] != (openTarget{Kind: "diff", Dir: dir, Base: fork}) {
		t.Fatalf("d opens the diff from the merge-base %s, got %+v", fork, *got)
	}
}
