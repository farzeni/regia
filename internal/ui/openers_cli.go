package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// OpenersUsage is `regia openers` help.
const OpenersUsage = `regia openers list                  the presets you can pick with preset = <name>
regia openers show                  your openers in effect, and where each comes from
regia openers test file <path>[:line] | image <path> | url <url> | pager | diff [dir]
                                    open it now, the way enter in the conversation would`

// Openers is the `regia openers` command: list the presets, show the
// config in effect, or try an opener from the shell.
func Openers(args []string, out io.Writer) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list":
		for _, p := range presetNames() {
			fmt.Fprintf(out, "%-14s %s\n", p[0], p[1])
		}
		fmt.Fprintf(out, "\npick one in %s:  preset = <name>\n", tildify(openersPath()))
		return nil
	case "show":
		c, err := loadOpeners()
		showOpeners(out, c, err)
		return nil
	case "test":
		c, err := loadOpeners()
		if err != nil {
			return err
		}
		t, err := testTarget(args[1:])
		if err != nil {
			return err
		}
		return c.runNow(t, out)
	}
	return fmt.Errorf("usage:\n%s", OpenersUsage)
}

// showOpeners prints every key, its value and where it was set.
func showOpeners(out io.Writer, c *openerConf, err error) {
	if err != nil {
		fmt.Fprintf(out, "error: %v\n(using the defaults until it's fixed)\n\n", err)
	}
	if _, serr := os.Stat(openersPath()); serr != nil {
		fmt.Fprintf(out, "# no %s: the default preset\n", tildify(openersPath()))
	}
	fmt.Fprintf(out, "%-6s = %s\n", "preset", c.preset)
	for _, k := range openerKeys[1:] {
		fmt.Fprintf(out, "%-6s = %-42s # %s\n", k, c.vals[k], c.from[k])
	}
	state := "off"
	if c.links {
		state = "on"
	}
	fmt.Fprintf(out, "\nlinks in the conversation: %s\n", state)
}

// testTarget reads `file x.go:12`, `url …` and the like.
func testTarget(args []string) (openTarget, error) {
	if len(args) == 0 {
		return openTarget{}, fmt.Errorf("usage:\n%s", OpenersUsage)
	}
	dir, _ := os.Getwd()
	arg := ""
	if len(args) > 1 {
		arg = args[1]
	}
	t := openTarget{Kind: args[0], Dir: dir}
	switch t.Kind {
	case "file", "image":
		if arg == "" {
			return t, fmt.Errorf("regia openers test %s <path>", t.Kind)
		}
		t.Path = arg
		if i := strings.LastIndex(arg, ":"); i > 0 && t.Kind == "file" {
			if n, err := strconv.Atoi(arg[i+1:]); err == nil {
				t.Path, t.Line = arg[:i], n
			}
		}
	case "url":
		if arg == "" {
			return t, errors.New("regia openers test url <url>")
		}
		t.URL = arg
	case "pager":
		t.Text = "regia openers test: this text is in your pager.\nquit it to come back.\n"
	case "diff":
		if arg != "" {
			t.Dir = absPath(arg, dir)
		}
	default:
		return t, fmt.Errorf("nothing opens %q: file, image, url, pager or diff", t.Kind)
	}
	return t, nil
}

// runNow opens t from the shell: a terminal program runs here until it
// quits, any other is started on its own.
func (c *openerConf) runNow(t openTarget, out io.Writer) error {
	argv, term, err := c.command(t)
	if err != nil {
		return err
	}
	how := "detached"
	if term {
		how = "in this terminal"
	}
	fmt.Fprintf(out, "running %q (%s)\n", argv, how)
	cmd := newCmd(argv[0], argv[1:]...)
	cmd.Dir = t.Dir
	if !term {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		return cmd.Start()
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if openerKey(t) == "pager" {
		cmd.Stdin = strings.NewReader(t.Text)
	}
	return cmd.Run()
}
