package ui

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

// openTarget is what enter (or a click) opens from the conversation or the
// changes tab: a file at a line, an image, a URL, text in the pager, or a
// worktree's diff.
type openTarget struct {
	Kind string // "file", "image", "url", "pager", "diff"
	Path string // file or image path
	Line int    // file line, 0 when unknown
	URL  string
	Text string // what the pager shows
	Dir  string // the agent's worktree, for "diff"
	Base string // merge-base commit for "diff": the agent's changes start there ("": uncommitted ones)
}

// openDoneMsg reports an opener that failed, to flash it.
type openDoneMsg struct{ err error }

// openThing opens t with the user's tools (openers.conf, see openers.go).
// A replaceable var: tests swap it.
var openThing = func(t openTarget) tea.Cmd { return theOpeners().open(t) }

// linkify makes text a clickable OSC 8 link to t when the terminal shows
// links; otherwise, and for kinds with nothing to link to, text unchanged.
var linkify = func(text string, t openTarget) string {
	c := theOpeners()
	if !c.links {
		return text
	}
	return osc8Link(c.linkURI(t), text)
}

// newCmd builds the process an opener runs; tests replace it to see the
// exact arguments.
var newCmd = exec.Command

// openerKey is the openers.conf key that opens t: a diff that comes with
// its text goes to the pager.
func openerKey(t openTarget) string {
	if t.Kind == "diff" && t.Text != "" {
		return "pager"
	}
	return t.Kind
}

// command is the program and arguments that open t, and whether it runs in
// the terminal (the pager always does).
func (c *openerConf) command(t openTarget) (argv []string, term bool, err error) {
	key := openerKey(t)
	tpl, ok := c.vals[key]
	if !ok || key == "link" || key == "osc8" || key == "preset" {
		return nil, false, fmt.Errorf("nothing opens %q", t.Kind)
	}
	argv, term, err = expandArgs(tpl, targetVars(t))
	if err != nil {
		return nil, false, fmt.Errorf("%s = %s: %w", key, tpl, err)
	}
	return argv, term || key == "pager", nil
}

// open runs the opener for t: a terminal program in place of regia until it
// quits, any other started on its own. A program that isn't installed or
// fails comes back as openDoneMsg, flashed in the status line.
func (c *openerConf) open(t openTarget) tea.Cmd {
	argv, term, err := c.command(t)
	if err != nil {
		return func() tea.Msg { return openDoneMsg{err} }
	}
	key := openerKey(t)
	explain := func(err error) tea.Msg {
		if errors.Is(err, exec.ErrNotFound) {
			err = fmt.Errorf("%s not found: set %s = … in %s", argv[0], key, tildify(openersPath()))
		}
		return openDoneMsg{err}
	}
	cmd := newCmd(argv[0], argv[1:]...)
	if t.Dir != "" {
		cmd.Dir = t.Dir
	}
	if term {
		if cmd.Err != nil { // not installed: don't blank the screen for nothing
			return func() tea.Msg { return explain(cmd.Err) }
		}
		if key == "pager" {
			cmd.Stdin = strings.NewReader(t.Text)
		}
		return tea.ExecProcess(cmd, explain)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // quitting regia leaves it open
	return func() tea.Msg {
		if err := cmd.Start(); err != nil {
			return explain(err)
		}
		go cmd.Wait() //nolint:errcheck // reaped; nobody waits for an editor
		return openDoneMsg{}
	}
}

// targetVars are the placeholder values for t.
func targetVars(t openTarget) map[string]string {
	line := t.Line
	if line <= 0 {
		line = 1
	}
	return map[string]string{
		"path": absPath(t.Path, t.Dir),
		"line": strconv.Itoa(line),
		"url":  t.URL,
		"dir":  t.Dir,
		"base": t.Base,
		"host": hostName(),
	}
}

// hostName is this machine's name, for file:// links (asked once: links
// are made for every path in the conversation).
var hostName = sync.OnceValue(func() string {
	h, _ := os.Hostname()
	return h
})

// absPath makes p absolute: ~/ is your home, a relative path is in dir (the
// agent's worktree, where its tools ran).
func absPath(p, dir string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, rest)
	}
	if dir == "" {
		return p
	}
	return filepath.Join(dir, p)
}

// linkURI is where a link to t points ("" when it has none): the URL
// itself, or for a file the link template with its path escaped. An image
// links to file:// (your viewer, not the editor).
func (c *openerConf) linkURI(t openTarget) string {
	switch t.Kind {
	case "url":
		return t.URL
	case "file", "image":
		p := absPath(t.Path, t.Dir)
		if !filepath.IsAbs(p) {
			return ""
		}
		v := targetVars(t)
		v["path"] = uriEscape.Replace((&url.URL{Path: p}).EscapedPath())
		tpl := c.vals["link"]
		if t.Kind == "image" || tpl == "" {
			tpl = "file://{host}{path}"
		}
		return fill(tpl, v)
	}
	return ""
}

// uriEscape escapes what a path may hold that would end a link template's
// path early (idea://open?file={path}&line=…).
var uriEscape = strings.NewReplacer("&", "%26", "?", "%3F", "#", "%23")

// osc8Link wraps text in an OSC 8 hyperlink to uri. Terminals that don't
// know OSC 8 skip the sequences; the width is the text's.
func osc8Link(uri, text string) string {
	if uri == "" || text == "" {
		return text
	}
	return "\x1b]8;;" + uri + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}
