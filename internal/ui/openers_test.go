package ui

import (
	"io/fs"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/farzeni/regia/presets"
)

// Tests run with the default openers and links off, whatever your terminal
// and openers.conf say.
func init() { openers, _ = buildOpeners("openers.conf", "", "linux", noEnv) }

func noEnv(string) string { return "" }

func envOf(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestOpenersConfig(t *testing.T) {
	file := openTarget{Kind: "file", Path: "x.go", Line: 12, Dir: "/w"}
	cases := []struct {
		name, conf, goos string
		env              func(string) string
		t                openTarget
		argv             []string
		term             bool
		err              string
	}{
		{name: "no file: default", t: file, argv: []string{"xdg-open", "/w/x.go"}},
		{name: "macos default", goos: "darwin", t: file, argv: []string{"open", "/w/x.go"}},
		{name: "preset", conf: "preset = vscode", t: file, argv: []string{"code", "/w", "-g", "/w/x.go:12"}},
		{name: "preset without a folder", conf: "preset = vscode", t: openTarget{Kind: "file", Path: "/w/x.go", Line: 12},
			argv: []string{"code", "-g", "/w/x.go:12"}},
		{name: "override after preset", conf: "file = zed {path}:{line}\npreset = vscode # comment", t: file,
			argv: []string{"zed", "/w/x.go:12"}},
		{name: "no line is 1", conf: "preset = nvim-tmux", t: openTarget{Kind: "file", Path: "/a/b.go", Dir: "/w"},
			argv: []string{"tmux", "new-window", "-c", "/w", "nvim", "+1", "/a/b.go"}},
		{name: "quoting", conf: `url = firefox --new-tab "{url}" 'a b' c\ d`, t: openTarget{Kind: "url", URL: "https://x.y/?a=1&b"},
			argv: []string{"firefox", "--new-tab", "https://x.y/?a=1&b", "a b", "c d"}},
		{name: "term prefix", conf: "image = term: chafa {path}", t: openTarget{Kind: "image", Path: "/i.png"},
			argv: []string{"chafa", "/i.png"}, term: true},
		{name: "pager always terminal", conf: "pager = bat -p", t: openTarget{Kind: "pager", Text: "x"},
			argv: []string{"bat", "-p"}, term: true},
		{name: "$PAGER", env: envOf("PAGER", "most -s"), t: openTarget{Kind: "pager"}, argv: []string{"most", "-s"}, term: true},
		{name: "your pager beats $PAGER", conf: "pager = less", env: envOf("PAGER", "most"), t: openTarget{Kind: "pager"},
			argv: []string{"less"}, term: true},
		{name: "diff with base", t: openTarget{Kind: "diff", Dir: "/w", Base: "abc123"},
			argv: []string{"git", "-C", "/w", "diff", "abc123"}, term: true},
		{name: "diff without base drops the arg", t: openTarget{Kind: "diff", Dir: "/w"},
			argv: []string{"git", "-C", "/w", "diff"}, term: true},
		{name: "diff with text pages it", t: openTarget{Kind: "diff", Text: "+x"}, argv: []string{"less", "-R"}, term: true},
		{name: "unknown key", conf: "preset = vscode\nfiel = vim", t: file, argv: []string{"xdg-open", "/w/x.go"},
			err: `openers.conf:2: unknown key "fiel"`},
		{name: "unknown preset", conf: "preset = emacs", t: file, argv: []string{"xdg-open", "/w/x.go"}, err: `no preset "emacs"`},
		{name: "unknown placeholder", conf: "file = vim {file}", t: file, argv: []string{"xdg-open", "/w/x.go"},
			err: "unknown placeholder {file}"},
		{name: "unclosed quote", conf: `file = vim "{path}`, t: file, argv: []string{"xdg-open", "/w/x.go"}, err: "unclosed"},
		{name: "bad osc8", conf: "osc8 = yes", t: file, argv: []string{"xdg-open", "/w/x.go"}, err: "osc8 is on, off or auto"},
		{name: "no equals", conf: "vim", t: file, argv: []string{"xdg-open", "/w/x.go"}, err: "want key = value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := tc.env
			if env == nil {
				env = noEnv
			}
			goos := tc.goos
			if goos == "" {
				goos = "linux"
			}
			c, err := buildOpeners("openers.conf", tc.conf, goos, env)
			if tc.err == "" && err != nil || tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
				t.Fatalf("error %v, want %q", err, tc.err)
			}
			argv, term, err := c.command(tc.t)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(argv, tc.argv) || term != tc.term {
				t.Fatalf("got %q term=%v, want %q term=%v", argv, term, tc.argv, tc.term)
			}
		})
	}
}

// A path from a transcript can hold anything: it must reach the program as
// one argument, never through a shell.
func TestOpenerNeverUsesAShell(t *testing.T) {
	var got []string
	orig := newCmd
	defer func() { newCmd = orig }()
	newCmd = func(name string, args ...string) *exec.Cmd {
		got = append([]string{name}, args...)
		return exec.Command("true")
	}
	c, err := buildOpeners("openers.conf", "preset = vscode", "linux", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	evil := "/tmp/my dir/a; rm -rf ~ $(touch x) `id`.go"
	msg := c.open(openTarget{Kind: "file", Path: evil, Line: 3})()
	if d, ok := msg.(openDoneMsg); !ok || d.err != nil {
		t.Fatalf("open: %#v", msg)
	}
	want := []string{"code", "-g", evil + ":3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exec got %q, want %q", got, want)
	}
}

func TestOpenerMissingProgramFlashes(t *testing.T) {
	c, _ := buildOpeners("openers.conf", "file = no-such-editor-xyz {path}", "linux", noEnv)
	msg := c.open(openTarget{Kind: "file", Path: "/a.go"})()
	d, ok := msg.(openDoneMsg)
	if !ok || d.err == nil || !strings.Contains(d.err.Error(), "no-such-editor-xyz not found: set file = …") {
		t.Fatalf("want a not-found error, got %#v", msg)
	}
	m := testModel(t)
	m.Update(d)
	if !strings.Contains(m.status, "no-such-editor-xyz not found") {
		t.Fatalf("status %q", m.status)
	}
	c, _ = buildOpeners("openers.conf", "pager = term: no-such-pager-xyz", "linux", noEnv)
	if d, ok := c.open(openTarget{Kind: "pager", Text: "x"})().(openDoneMsg); !ok || d.err == nil {
		t.Fatal("a missing terminal program should flash, not take over the screen")
	}
}

func TestEveryPresetParses(t *testing.T) {
	ents, err := fs.ReadDir(presets.Openers, "openers")
	if err != nil || len(ents) < 6 {
		t.Fatalf("presets: %v %d", err, len(ents))
	}
	for _, e := range ents {
		name := strings.TrimSuffix(e.Name(), ".conf")
		text, _ := presetText(name)
		if !strings.HasPrefix(text, "# ") {
			t.Errorf("%s: start with a one-line # comment saying what it is", e.Name())
		}
		lines, err := parseConf(e.Name(), text)
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
		}
		for _, l := range lines {
			if l.key == "preset" {
				t.Errorf("%s: a preset can't name another preset", e.Name())
			}
		}
		if _, err := buildOpeners("openers.conf", "preset = "+name, "linux", noEnv); err != nil {
			t.Errorf("preset = %s: %v", name, err)
		}
	}
}

func TestOSC8Auto(t *testing.T) {
	cases := []struct {
		name string
		env  func(string) string
		want bool
	}{
		{"nothing", noEnv, false},
		{"plain xterm", envOf("TERM", "xterm-256color"), false},
		{"linux console", envOf("TERM", "linux"), false},
		{"tilix / gnome", envOf("VTE_VERSION", "7600"), true},
		{"old vte", envOf("VTE_VERSION", "4200"), false},
		{"iterm", envOf("TERM_PROGRAM", "iTerm.app"), true},
		{"wezterm", envOf("TERM_PROGRAM", "WezTerm"), true},
		{"ghostty", envOf("TERM", "xterm-ghostty"), true},
		{"kitty", envOf("KITTY_WINDOW_ID", "1"), true},
		{"windows terminal", envOf("WT_SESSION", "x"), true},
		{"tmux in tilix", envOf("VTE_VERSION", "7600", "TMUX", "/tmp/tmux-1/default,1,0"), false},
	}
	for _, tc := range cases {
		if got := osc8Auto(tc.env); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	on, _ := buildOpeners("openers.conf", "osc8 = on", "linux", envOf("TMUX", "x"))
	off, _ := buildOpeners("openers.conf", "osc8 = off", "linux", envOf("KITTY_WINDOW_ID", "1"))
	if !on.links || off.links {
		t.Fatalf("osc8 on/off should beat the terminal: on=%v off=%v", on.links, off.links)
	}
}

func TestLinkify(t *testing.T) {
	defer withOpeners(t, "preset = vscode\nosc8 = on")()
	got := linkify("x.go:3", openTarget{Kind: "file", Path: "a b/x.go", Line: 3, Dir: "/w"})
	if want := "\x1b]8;;vscode://file/w/a%20b/x.go:3\x1b\\x.go:3\x1b]8;;\x1b\\"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := linkify("log", openTarget{Kind: "pager", Text: "x"}); got != "log" {
		t.Fatalf("pager text has nothing to link to: %q", got)
	}
	defer withOpeners(t, "osc8 = off")()
	if got := linkify("u", openTarget{Kind: "url", URL: "https://x"}); got != "u" {
		t.Fatalf("links off: %q", got)
	}
}

// withOpeners makes conf the openers in effect until the returned func.
func withOpeners(t *testing.T, conf string) func() {
	t.Helper()
	c, err := buildOpeners("openers.conf", conf, "linux", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	orig := openers
	openers = c
	clear(linkCache)
	return func() { openers = orig; clear(linkCache) }
}
