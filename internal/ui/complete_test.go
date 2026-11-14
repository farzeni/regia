package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func stubCompletion(t *testing.T) {
	t.Helper()
	os, of := skillsFor, filesFor
	t.Cleanup(func() { skillsFor, filesFor = os, of })
	skillsFor = func(string) []compItem {
		return []compItem{
			{name: "code-review", desc: "review the diff"},
			{name: "swarm", desc: "run a swarm of agents"},
			{name: "frontend-design:frontend-design", desc: "visual design"},
		}
	}
	filesFor = func(string) []compItem {
		return []compItem{
			{name: "internal/", dir: true},
			{name: "internal/ui/", dir: true},
			{name: "internal/ui/input.go"},
			{name: "internal/ui/complete.go"},
			{name: "README.md"},
		}
	}
}

func names(c *completion) []string {
	if c == nil {
		return nil
	}
	var out []string
	for _, it := range c.items {
		out = append(out, it.name)
	}
	return out
}

func TestSkillCompletionMidText(t *testing.T) {
	stubCompletion(t)
	m := testModel(t)
	got := stubSpawn(t)

	typeAndRender(m, "please run ")
	if m.comp != nil {
		t.Fatal("popup open without a trigger")
	}
	typeAndRender(m, "/sw")
	if n := names(m.comp); len(n) == 0 || n[0] != "swarm" {
		t.Fatalf("popup for /sw: %v", n)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "› /swarm") || !strings.Contains(v, "run a swarm of agents") {
		t.Fatalf("popup not drawn:\n%s", v)
	}
	send(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.draft() != "please run /swarm " || m.comp != nil {
		t.Fatalf("after tab: %q open=%v", m.draft(), m.comp != nil)
	}
	typeAndRender(m, "on it")
	send(m, tkey("enter"))
	if *got != "please run /swarm on it" {
		t.Fatalf("sent %q", *got)
	}

	// plugin skills match on the part after "plugin:"; enter picks, doesn't send
	*got = ""
	typeAndRender(m, "/front")
	send(m, tkey("enter"))
	if m.draft() != "/frontend-design:frontend-design " || *got != "" {
		t.Fatalf("enter should pick: %q sent=%q", m.draft(), *got)
	}
}

func TestCompletionNotForPaths(t *testing.T) {
	stubCompletion(t)
	m := testModel(t)
	typeAndRender(m, "look in /home/x")
	if m.comp != nil {
		t.Fatalf("a path opened the popup: %v", names(m.comp))
	}
	m.clearDraft()
	typeAndRender(m, "a/b")
	if m.comp != nil {
		t.Fatal("a slash inside a word opened the popup")
	}
}

func TestCompletionKeysAndEsc(t *testing.T) {
	stubCompletion(t)
	m := testModel(t)
	sel := m.cursorID
	typeAndRender(m, "/")
	if len(names(m.comp)) != 3 {
		t.Fatalf("empty query should list all: %v", names(m.comp))
	}
	send(m, tkey("down"))
	if m.comp.sel != 1 || m.cursorID != sel {
		t.Fatalf("down should move in the popup, not the list: sel=%d", m.comp.sel)
	}
	send(m, tkey("esc"))
	if m.comp != nil || m.draft() != "/" {
		t.Fatalf("esc should close only the popup: %q", m.draft())
	}
	typeAndRender(m, "c") // same word: stays closed
	if m.comp != nil {
		t.Fatal("popup came back on the word esc closed")
	}
	send(m, tkey(" "))
	typeAndRender(m, "/c") // a new word opens again
	if m.comp == nil {
		t.Fatal("popup should open for a new word")
	}
	send(m, tkey("esc"))
	send(m, tkey("esc"))
	if m.draft() != "" {
		t.Fatalf("second esc should clear: %q", m.draft())
	}
}

func TestFileCompletion(t *testing.T) {
	stubCompletion(t)
	m := testModel(t)
	typeAndRender(m, "fix @")
	if n := names(m.comp); !slices.Equal(slices.Sorted(slices.Values(n)), []string{"README.md", "internal/"}) {
		t.Fatalf("empty @ lists the top level: %v", n)
	}
	typeAndRender(m, "int")
	send(m, tea.KeyMsg{Type: tea.KeyTab}) // a directory: popup stays open inside it
	if m.draft() != "fix @internal/" || m.comp == nil {
		t.Fatalf("dir pick: %q open=%v", m.draft(), m.comp != nil)
	}
	typeAndRender(m, "comp")
	if n := names(m.comp); len(n) == 0 || n[0] != "internal/ui/complete.go" {
		t.Fatalf("popup for internal/comp: %v", n)
	}
	send(m, tkey("enter"))
	if m.draft() != "fix @internal/ui/complete.go " {
		t.Fatalf("file pick: %q", m.draft())
	}
}

func TestPopupKeepsFrameHeight(t *testing.T) {
	stubCompletion(t)
	m := testModel(t)
	m.w, m.h = 100, 28
	typeAndRender(m, "/")
	if ls := strings.Split(m.View(), "\n"); len(ls) != 28 {
		t.Fatalf("frame has %d lines with the popup", len(ls))
	}
}

func TestDroppedImagePath(t *testing.T) {
	m := testModel(t)
	got := stubSpawn(t)
	dir := t.TempDir()
	img := filepath.Join(dir, "my shot.png")
	os.WriteFile(img, []byte("\x89PNG"), 0o600)

	typeAndRender(m, "what is this ")
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'" + img + "' "), Paste: true})
	if m.draft() != "what is this [Image #1]" {
		t.Fatalf("dropped image: %q", m.draft())
	}
	send(m, tkey("enter"))
	if *got != "what is this "+img {
		t.Fatalf("sent %q", *got)
	}

	// a dropped file that is not an image goes in as its path
	txt := filepath.Join(dir, "notes.txt")
	os.WriteFile(txt, []byte("x"), 0o600)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("file://" + strings.ReplaceAll(txt, " ", "%20")), Paste: true})
	if m.draft() != txt {
		t.Fatalf("dropped file: %q", m.draft())
	}
}

func TestCtrlVImage(t *testing.T) {
	m := testModel(t)
	got := stubSpawn(t)
	ot, or := clipboardTypes, readClipboardAs
	t.Cleanup(func() { clipboardTypes, readClipboardAs = ot, or })
	clipboardTypes = func() []string { return []string{"image/png", "text/html"} }
	readClipboardAs = func(mime string) ([]byte, error) {
		if mime != "image/png" {
			t.Fatalf("read as %s", mime)
		}
		return []byte("\x89PNGdata"), nil
	}
	typeAndRender(m, "see ")
	send(m, tea.KeyMsg{Type: tea.KeyCtrlV})
	send(m, tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.draft() != "see [Image #1][Image #2]" {
		t.Fatalf("draft %q", m.draft())
	}
	send(m, tkey("enter"))
	cache, _ := os.UserCacheDir()
	if !strings.HasPrefix(*got, "see "+filepath.Join(cache, "regia", "pastes")) {
		t.Fatalf("sent %q", *got)
	}
	p := strings.TrimPrefix(*got, "see ")
	p = p[:strings.Index(p, ".png")+4]
	if b, err := os.ReadFile(p); err != nil || string(b) != "\x89PNGdata" {
		t.Fatalf("saved image %q: %v %q", p, err, b)
	}
}

func TestCtrlVCopiedFiles(t *testing.T) {
	m := testModel(t)
	dir := t.TempDir()
	img := filepath.Join(dir, "a.jpg")
	os.WriteFile(img, []byte("x"), 0o600)
	ot, or := clipboardTypes, readClipboardAs
	t.Cleanup(func() { clipboardTypes, readClipboardAs = ot, or })
	clipboardTypes = func() []string { return []string{"x-special/gnome-copied-files", "text/uri-list"} }
	readClipboardAs = func(string) ([]byte, error) { return []byte("copy\nfile://" + img + "\nfile://" + dir), nil }
	send(m, tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.draft() != "[Image #1] "+dir {
		t.Fatalf("draft %q", m.draft())
	}
}

func TestLoadSkills(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte(s), 0o600)
	}
	write(filepath.Join(cfg, "skills", "swarm", "SKILL.md"), "---\nname: swarm\ndescription: \"run a swarm\"\n---\nbody")
	write(filepath.Join(cfg, "skills", "hidden", "SKILL.md"), "---\nname: hidden\nuser-invocable: false\n---\n")
	write(filepath.Join(cfg, "commands", "git", "pr.md"), "---\ndescription: open a PR\n---\n")
	write(filepath.Join(cfg, "skills", "folded", "SKILL.md"), "---\nname: folded\ndescription: >\n  first line\n  second line\nlicense: x\n---\n")
	plug := filepath.Join(cfg, "plugins", "cache", "fd")
	write(filepath.Join(plug, "skills", "fd", "SKILL.md"), "---\nname: frontend-design\ndescription: design\n---\n")
	write(filepath.Join(cfg, "plugins", "installed_plugins.json"),
		`{"version":2,"plugins":{"frontend-design@official":[{"installPath":"`+plug+`"}]}}`)
	proj := t.TempDir()
	write(filepath.Join(proj, ".claude", "skills", "swarm", "SKILL.md"), "---\nname: swarm\ndescription: project swarm\n---\n")
	write(filepath.Join(proj, ".claude", "skills", "deploy", "SKILL.md"), "---\nname: deploy\ndescription: ship it\n---\n")

	got := map[string]string{}
	for _, it := range loadSkills(proj) {
		got[it.name] = it.desc
	}
	for name, desc := range map[string]string{
		"swarm":                           "project swarm",
		"deploy":                          "ship it",
		"folded":                          "first line second line",
		"git:pr":                          "open a PR",
		"frontend-design:frontend-design": "design",
		"code-review":                     "built-in: review the current diff for bugs",
	} {
		if got[name] != desc {
			t.Errorf("%s: %q (all: %v)", name, got[name], got)
		}
	}
	if _, ok := got["hidden"]; ok {
		t.Error("user-invocable: false should be hidden")
	}
}

func TestLoadFiles(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src", "deep"), 0o700)
	os.WriteFile(filepath.Join(dir, "src", "deep", "a.go"), nil, 0o600)
	os.WriteFile(filepath.Join(dir, "ignored.log"), nil, 0o600)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o600)
	os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o700)
	os.WriteFile(filepath.Join(dir, "node_modules", "x", "i.js"), nil, 0o600)

	var walked []string
	for _, it := range loadFiles(dir) {
		walked = append(walked, it.name)
	}
	slices.Sort(walked)
	// no repository: the walk skips dot dirs and node_modules, not ignored files
	if want := []string{".gitignore", "ignored.log", "src/", "src/deep/", "src/deep/a.go"}; !slices.Equal(walked, want) {
		t.Fatalf("walk: %v", walked)
	}

	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\nnode_modules/\n"), 0o600)
	if exec.Command("git", "-C", dir, "init", "-q").Run() != nil {
		t.Skip("no git")
	}
	var listed []string
	for _, it := range loadFiles(dir) {
		listed = append(listed, it.name)
	}
	slices.Sort(listed)
	if want := []string{".gitignore", "src/", "src/deep/", "src/deep/a.go"}; !slices.Equal(listed, want) {
		t.Fatalf("git: %v", listed)
	}
}

func TestSplitShellWords(t *testing.T) {
	for in, want := range map[string][]string{
		`/a/b.png`:            {"/a/b.png"},
		`'/a b/c.png' /d.png`: {"/a b/c.png", "/d.png"},
		`/a\ b/c.png`:         {"/a b/c.png"},
		`"/x y.png"`:          {"/x y.png"},
	} {
		if got := splitShellWords(in); !slices.Equal(got, want) {
			t.Errorf("%q: %q", in, got)
		}
	}
}
