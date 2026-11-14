package main

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

// attachCmd runs `claude attach <id>` so that leaving the session — with ←
// or with Ctrl+Z — lands straight back in regia.
//
// Ctrl+Z in claude suspends only its own process (SIGTSTP to itself). In the
// same process group as regia, the shell would see the job stop and take the
// terminal back. So the client runs in its own process group as the terminal's
// foreground group; when it stops, regia closes that viewer (the session keeps
// running in the daemon), takes the terminal back and redraws.
type attachCmd struct {
	id     string
	argv   []string // override for tests; default: claude attach <id>
	stdout io.Writer
}

func (c *attachCmd) SetStdin(io.Reader)    {}
func (c *attachCmd) SetStdout(w io.Writer) { c.stdout = w }
func (c *attachCmd) SetStderr(io.Writer)   {}

func (c *attachCmd) Run() error {
	tty := os.Stdin
	fd := int(tty.Fd())
	// Stay on the alternate screen so the shell's scrollback never flashes.
	_, _ = os.Stdout.WriteString("\x1b[?1049h\x1b[2J\x1b[H")

	argv := c.argv
	if len(argv) == 0 {
		argv = []string{"claude", "attach", c.id}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, os.Stdout, os.Stderr
	// Foreground uses Ctty as the parent's fd of the controlling terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Foreground: true, Ctty: fd}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid

	var runErr error
	for {
		var ws syscall.WaitStatus
		_, err := syscall.Wait4(pid, &ws, syscall.WUNTRACED, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			runErr = err
			break
		}
		if ws.Stopped() {
			// Ctrl+Z: the viewer suspended itself. It has already restored
			// the terminal mode; regia redraws everything, so end it now.
			_ = syscall.Kill(pid, syscall.SIGKILL)
			_ = syscall.Kill(pid, syscall.SIGCONT)
			continue
		}
		if ws.Exited() || ws.Signaled() {
			break
		}
	}
	reclaimForeground(fd)
	return runErr
}

// reclaimForeground makes regia's process group the terminal's foreground
// group again. A background group writing tcsetpgrp gets SIGTTOU: ignore it
// for the call.
func reclaimForeground(fd int) {
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	_ = unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, syscall.Getpgrp())
}
