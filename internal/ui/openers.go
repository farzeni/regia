package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/farzeni/regia/presets"
)

// Openers are the programs enter opens things with: a file at a line, an
// image, a URL, long output in a pager, a worktree's diff. They come from
// ~/.config/regia/openers.conf, `key = value` lines:
//
//	preset = vscode              # a file in presets/openers, laid over default
//	image  = term: chafa {path}  # then your own lines over it
//	osc8   = auto                # clickable links: on, off, auto
//
// A value is a command template: split like a shell would split it (quotes,
// backslashes), then {path} {line} {url} {dir} {base} are filled in inside
// each argument. It never goes through a shell, so a path from a transcript
// can't run anything. A `term:` prefix runs the program in this terminal
// (regia steps aside until it quits); without it the program starts on its
// own, detached, like a GUI editor.

// openerKeys are the keys openers.conf knows, in the order `regia openers
// show` prints them.
var openerKeys = []string{"preset", "file", "image", "url", "pager", "diff", "link", "osc8"}

// openerVars are the placeholders a template may use.
var openerVars = map[string]bool{"path": true, "line": true, "url": true, "dir": true, "base": true, "host": true}

var placeholderRe = regexp.MustCompile(`\{[^{}]*\}`)

// openerConf is the openers in effect: each key's value and where it was set
// (a preset, a line of openers.conf, $PAGER), for `regia openers show`.
type openerConf struct {
	preset string
	vals   map[string]string
	from   map[string]string
	links  bool // OSC 8 links are on (osc8 resolved against the terminal)
}

// confLine is one `key = value` line of openers.conf or a preset.
type confLine struct {
	key, val string
	n        int
}

// parseConf reads `key = value` lines; name is how errors call the file.
func parseConf(name, text string) ([]confLine, error) {
	var out []confLine
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: want key = value, got %q", name, n, l)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(stripComment(v))
		if err := checkOpener(k, v); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, n, err)
		}
		out = append(out, confLine{k, v, n})
	}
	return out, nil
}

// stripComment drops a ` # comment` after a value (a # inside quotes or
// without a space before it, as in a URL, stays).
func stripComment(v string) string {
	q := byte(0)
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '\'' || c == '"':
			q = c
		case c == '#' && i > 0 && (v[i-1] == ' ' || v[i-1] == '\t'):
			return v[:i]
		}
	}
	return v
}

// checkOpener says what is wrong with one line, nil when nothing.
func checkOpener(k, v string) error {
	switch k {
	case "preset":
		if _, err := presetText(v); err != nil {
			return err
		}
		return nil
	case "osc8":
		if v != "on" && v != "off" && v != "auto" {
			return fmt.Errorf("osc8 is on, off or auto, not %q", v)
		}
		return nil
	case "link":
		return checkPlaceholders(v)
	case "file", "image", "url", "pager", "diff":
		args, err := splitArgs(strings.TrimPrefix(v, "term:"))
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		if len(args) == 0 {
			return fmt.Errorf("%s: no program", k)
		}
		return checkPlaceholders(v)
	}
	return fmt.Errorf("unknown key %q (known: %s)", k, strings.Join(openerKeys, ", "))
}

func checkPlaceholders(v string) error {
	for _, p := range placeholderRe.FindAllString(v, -1) {
		if !openerVars[p[1:len(p)-1]] {
			return fmt.Errorf("unknown placeholder %s (known: {path} {line} {url} {dir} {base} {host})", p)
		}
	}
	return nil
}

// presetText is a built-in preset's file.
func presetText(name string) (string, error) {
	b, err := presets.Openers.ReadFile("openers/" + name + ".conf")
	if err != nil {
		return "", fmt.Errorf("no preset %q (regia openers list shows them)", name)
	}
	return string(b), nil
}

// presetNames lists the built-in presets with their first comment line.
func presetNames() [][2]string {
	ents, _ := fs.ReadDir(presets.Openers, "openers")
	var out [][2]string
	for _, e := range ents {
		name := strings.TrimSuffix(e.Name(), ".conf")
		text, _ := presetText(name)
		about, _, _ := strings.Cut(text, "\n")
		out = append(out, [2]string{name, strings.TrimSpace(strings.TrimPrefix(about, "#"))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// buildOpeners lays the user's openers.conf (text; name for errors) over
// its preset over the platform's default. On a mistake it returns the
// default openers and the error, so regia still opens things.
func buildOpeners(name, text, goos string, getenv func(string) string) (*openerConf, error) {
	c := baseOpeners(goos)
	lines, err := parseConf(name, text)
	if err == nil {
		err = c.apply(name, lines)
	}
	if err != nil {
		c = baseOpeners(goos)
	}
	if v := getenv("PAGER"); v != "" && !strings.Contains(c.from["pager"], ".conf:") {
		if _, perr := splitArgs(v); perr == nil {
			c.vals["pager"], c.from["pager"] = "term: "+v, "$PAGER"
		}
	}
	c.links = c.vals["osc8"] == "on" || c.vals["osc8"] == "auto" && osc8Auto(getenv)
	return c, err
}

func baseOpeners(goos string) *openerConf {
	base := "default"
	if goos == "darwin" {
		base = "default-macos"
	}
	c := &openerConf{preset: "default", vals: map[string]string{}, from: map[string]string{}}
	text, _ := presetText(base)
	lines, _ := parseConf(base, text)
	for _, l := range lines {
		c.vals[l.key], c.from[l.key] = l.val, "preset "+base
	}
	return c
}

// apply sets the preset first (wherever its line is), then every other line.
func (c *openerConf) apply(name string, lines []confLine) error {
	for _, l := range lines {
		if l.key != "preset" {
			continue
		}
		text, _ := presetText(l.val)
		pl, err := parseConf(l.val+".conf", text)
		if err != nil {
			return err
		}
		for _, p := range pl {
			if p.key == "preset" {
				return fmt.Errorf("%s.conf:%d: a preset can't name another preset", l.val, p.n)
			}
			c.vals[p.key], c.from[p.key] = p.val, "preset "+l.val
		}
		c.preset = l.val
	}
	for _, l := range lines {
		if l.key != "preset" {
			c.vals[l.key], c.from[l.key] = l.val, name+":"+strconv.Itoa(l.n)
		}
	}
	return nil
}

// osc8Auto says whether the terminal regia runs in shows OSC 8 links, from
// what it puts in the environment. Inside tmux links need a tmux setting, so
// there you turn them on yourself (osc8 = on).
func osc8Auto(getenv func(string) string) bool {
	if getenv("TMUX") != "" {
		return false
	}
	switch getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "ghostty", "vscode":
		return true
	}
	switch getenv("TERM") {
	case "xterm-kitty", "xterm-ghostty", "wezterm":
		return true
	}
	if getenv("KITTY_WINDOW_ID") != "" || getenv("WT_SESSION") != "" {
		return true
	}
	v, _ := strconv.Atoi(getenv("VTE_VERSION")) // Tilix, GNOME Terminal, Ptyxis…
	return v >= 5000
}

// openersPath is where your openers live.
func openersPath() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "regia", "openers.conf")
}

// loadOpeners reads openers.conf (none: the default preset).
func loadOpeners() (*openerConf, error) {
	p := openersPath()
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		c, _ := buildOpeners(p, "", goos, os.Getenv)
		return c, err
	}
	return buildOpeners(tildify(p), string(b), goos, os.Getenv)
}

// openers is the config in effect, read once when regia starts (changes
// need a restart: the rendered conversation is cached with its links).
var openers *openerConf

// theOpeners is openers, loading it on first use.
func theOpeners() *openerConf {
	if openers == nil {
		openers, _ = loadOpeners()
	}
	return openers
}

// splitArgs splits a template into arguments the way a shell would: spaces
// separate, '…' keeps everything, "…" and \ escape. Nothing else of a shell
// happens (no variables, globs, pipes).
func splitArgs(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	in, q := false, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '\'':
			if c == '\'' {
				q = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\\' && i+1 < len(s) && (q == 0 || strings.IndexByte(`"\$`+"`", s[i+1]) >= 0):
			i++
			cur.WriteByte(s[i])
		case q == '"':
			if c == '"' {
				q = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			q, in = c, true
		case c == ' ' || c == '\t':
			if in {
				out, in = append(out, cur.String()), false
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if q != 0 {
		return nil, fmt.Errorf("unclosed %c quote", q)
	}
	if in {
		out = append(out, cur.String())
	}
	return out, nil
}

// expandArgs turns a template into the program and its arguments, and
// whether it runs in the terminal. An argument that was only a placeholder
// and comes out empty ({base} with no base) is dropped, not passed as "".
func expandArgs(tpl string, vars map[string]string) (argv []string, term bool, err error) {
	if rest, ok := strings.CutPrefix(strings.TrimSpace(tpl), "term:"); ok {
		tpl, term = rest, true
	}
	args, err := splitArgs(tpl)
	if err != nil {
		return nil, false, err
	}
	for _, a := range args {
		v := fill(a, vars)
		if v == "" && placeholderRe.MatchString(a) && placeholderRe.ReplaceAllString(a, "") == "" {
			continue
		}
		argv = append(argv, v)
	}
	if len(argv) == 0 {
		return nil, false, errors.New("empty command")
	}
	return argv, term, nil
}

// fill replaces the placeholders in s.
func fill(s string, vars map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(s, func(p string) string {
		if v, ok := vars[p[1:len(p)-1]]; ok {
			return v
		}
		return p
	})
}
