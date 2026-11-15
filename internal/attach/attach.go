// Package attach runs `claude attach <id>` behind a pty proxy so leaving the
// session returns to regia in ~80 ms without flashing the shell.
package attach

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

// Cmd runs `claude attach <id>` behind a small pty proxy so leaving the
// session (← or Ctrl+Z) lands back in regia with no shell flash and no wait.
//
// Measured on claude 2.1.325: when the viewer is closed it resets the terminal
// and leaves the alternate screen within ~4 ms, then takes ~460 ms to exit.
// Run directly, that shows the shell's screen for half a second. Through the
// proxy, regia drops the "leave alternate screen" sequence and returns as soon
// as the child has left it and gone quiet; the child finishes exiting in the
// background. The child has its own pty, so shell job control never sees it.
type Cmd struct {
	id   string
	argv []string // override for tests; default: claude attach <id>
}

// Session attaches to the background session with this short id.
func Session(id string) *Cmd { return &Cmd{id: id} }

// Command runs any command through the same proxy (testing).
func Command(argv ...string) *Cmd { return &Cmd{argv: argv} }

func (c *Cmd) SetStdin(io.Reader)  {}
func (c *Cmd) SetStdout(io.Writer) {}
func (c *Cmd) SetStderr(io.Writer) {}

const (
	leaveAlt = "\x1b[?1049l"
	enterAlt = "\x1b[?1049h"
	// quiet period after the child leaves the alternate screen before regia
	// takes the terminal back (re-entering it within this window cancels).
	leaveQuiet = 80 * time.Millisecond
)

func (c *Cmd) Run() error {
	argv := c.argv
	if len(argv) == 0 {
		argv = []string{"claude", "attach", c.id}
	}
	in, out := os.Stdin, os.Stdout
	fd := int(in.Fd())

	// Stay on the alternate screen the whole time.
	_, _ = out.WriteString(enterAlt + "\x1b[2J\x1b[H")

	cmd := exec.Command(argv[0], argv[1:]...)
	size, _ := pty.GetsizeFull(in)
	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return err
	}
	exited := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(exited) }()

	oldState, err := term.MakeRaw(fd)
	if err == nil {
		defer func() { _ = term.Restore(fd, oldState) }()
	}

	// Window size follows the real terminal.
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			_ = pty.InheritSize(in, ptmx)
		}
	}()

	// Keyboard -> child. Cancelable, so no goroutine keeps reading stdin
	// (and stealing keys from regia) after we return.
	cr, err := cancelreader.NewReader(in)
	if err != nil {
		_ = ptmx.Close()
		return err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(ptmx, cr)
	}()

	// Child -> screen, filtering the alternate-screen exit.
	left := make(chan struct{}, 1) // child left the alternate screen
	back := make(chan struct{}, 1) // child re-entered it
	outDone := make(chan struct{})
	tail := &tailBuf{}
	go func() {
		defer close(outDone)
		f := &altFilter{onLeave: func() { notify(left) }, onEnter: func() { notify(back) }}
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				tail.Write(buf[:n])
				_, _ = out.Write(f.Filter(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

	var quiet <-chan time.Time
	failed := false
wait:
	for {
		select {
		case <-exited:
			failed = waitErr != nil
			break wait
		case <-outDone: // its pty closed: it's exiting, see how
			select {
			case <-exited:
				failed = waitErr != nil
			case <-time.After(300 * time.Millisecond):
			}
			break wait
		case <-left:
			quiet = time.After(leaveQuiet)
		case <-back:
			quiet = nil
		case <-quiet:
			break wait // detached: don't wait for the slow shutdown
		}
	}

	if failed { // let the last of its output arrive: it says why
		select {
		case <-outDone:
		case <-time.After(200 * time.Millisecond):
		}
	}
	cr.Cancel()
	wg.Wait()
	_ = ptmx.Close() // child (if still exiting) gets SIGHUP on its own pty
	if failed {
		return exitError(argv, waitErr, tail.String())
	}
	return nil
}

// exitError says why the child quit on its own with an error: its exit
// status and the last line it printed (gone from the screen by now).
func exitError(argv []string, err error, output string) error {
	name := strings.Join(argv[:min(2, len(argv))], " ")
	msg := fmt.Sprintf("%s: %v", name, err)
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		msg = fmt.Sprintf("%s exited %d", name, ee.ExitCode())
	}
	lines := strings.Split(strings.ReplaceAll(ansi.Strip(output), "\r", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			if r := []rune(l); len(r) > 200 {
				l = string(r[:200]) + "…"
			}
			return errors.New(msg + ": " + l)
		}
	}
	return errors.New(msg)
}

// tailBuf keeps the last few KB written to it.
type tailBuf struct {
	mu sync.Mutex
	b  []byte
}

func (t *tailBuf) Write(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if over := len(t.b) - 4096; over > 0 {
		t.b = append([]byte{}, t.b[over:]...)
	}
}

func (t *tailBuf) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}

func notify(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// altFilter removes "leave alternate screen" sequences from a byte stream,
// handling sequences split across reads, and reports enter/leave.
type altFilter struct {
	pending          []byte
	onLeave, onEnter func()
}

func (f *altFilter) Filter(p []byte) []byte {
	data := append(f.pending, p...)
	f.pending = nil
	if bytes.Contains(data, []byte(enterAlt)) && f.onEnter != nil {
		f.onEnter()
	}
	if bytes.Contains(data, []byte(leaveAlt)) {
		data = bytes.ReplaceAll(data, []byte(leaveAlt), nil)
		if f.onLeave != nil {
			f.onLeave()
		}
	}
	// Hold back a trailing partial "\x1b[?1049l" for the next read.
	for k := min(len(leaveAlt)-1, len(data)); k > 0; k-- {
		if bytes.HasSuffix(data, []byte(leaveAlt[:k])) {
			f.pending = append([]byte{}, data[len(data)-k:]...)
			return data[:len(data)-k]
		}
	}
	return data
}
