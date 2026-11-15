package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/gitinfo"
)

func TestDiffKeyTogglesPanel(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	m := testModel(t)
	m.Update(tkey("esc")) // command keys
	m.cursorID = "aaaa1111"
	m.rebuild()
	dir := m.agents["aaaa1111"].Cwd
	git := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "f.go"), []byte("package f\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "init")
	os.WriteFile(filepath.Join(dir, "f.go"), []byte("package f\n\nvar AddedLine = 1\n"), 0o644)

	_, cmd := m.Update(tkey("D"))
	if !m.dv.on || cmd == nil {
		t.Fatal("D should turn the diff view on and start loading")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "computing the diff") {
		t.Fatalf("no loading line:\n%s", v)
	}
	deliver(m, cmd)
	v := ansi.Strip(m.View())
	for _, want := range []string{"changes vs main", "f.go", "+var AddedLine = 1", "1 file changed"} {
		if !strings.Contains(v, want) {
			t.Errorf("diff view lacks %q:\n%s", want, v)
		}
	}
	if _, cmd := m.Update(tkey("esc")); quits(cmd) || m.dv.on {
		t.Fatal("esc should close the diff view, not quit")
	}
	m.Update(tkey("D"))
	m.Update(tkey("D"))
	if m.dv.on {
		t.Fatal("D again should close it")
	}
	m.Update(tkey("D"))
	m.Update(tkey("j"))
	if m.dv.on {
		t.Fatal("moving to another row should close it")
	}
}

func TestDiffLinesStates(t *testing.T) {
	s := func(d gitinfo.Diff) string { return ansi.Strip(strings.Join(diffLines(d, 60), "\n")) }
	if v := s(gitinfo.Diff{Err: "no git repo"}); !strings.Contains(v, "no diff: no git repo") {
		t.Error(v)
	}
	if v := s(gitinfo.Diff{Empty: true, Base: "main"}); !strings.Contains(v, "no changes against main") {
		t.Error(v)
	}
	if v := s(gitinfo.Diff{Stat: " a | 1 +", Body: "+x", Cut: 1234}); !strings.Contains(v, "1234 more lines not shown") || !strings.Contains(v, "uncommitted changes only") {
		t.Error(v)
	}
}

func TestStatLineKeepsCounts(t *testing.T) {
	long := " " + strings.Repeat("dir/", 20) + "file.go | 12 ++++++----"
	got := ansi.Strip(statLine(long, 40))
	if !strings.HasSuffix(got, "| 12 ++++++----") || !strings.Contains(got, "…") || ansi.StringWidth(got) > 40 {
		t.Fatalf("%q (%d)", got, ansi.StringWidth(got))
	}
	if got := ansi.Strip(statLine(" 2 files changed, 3 insertions(+)", 60)); got != " 2 files changed, 3 insertions(+)" {
		t.Fatalf("%q", got)
	}
}

func TestCleanDropsControls(t *testing.T) {
	if got := clean("+\tx\x1b[31mred\r"); got != "+    x[31mred" {
		t.Fatalf("%q", got)
	}
}

// deliver runs cmd (a batch too) and feeds its messages back to m.
func deliver(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			deliver(m, c)
		}
	case nil:
	default:
		m.Update(msg)
	}
}
