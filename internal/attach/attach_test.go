package attach

import (
	"errors"
	"strings"
	"testing"
)

func TestExitError(t *testing.T) {
	err := exitError([]string{"claude", "attach", "abc"}, errors.New("exit status 1"),
		"\x1b[?1049h\x1b[2J\x1b[31mError: session abc is not running\x1b[0m\r\n\r\n")
	if want := "claude attach: exit status 1: Error: session abc is not running"; err.Error() != want {
		t.Fatalf("got %q want %q", err, want)
	}
	if err := exitError([]string{"claude", "attach"}, errors.New("boom"), "\x1b[2J"); err.Error() != "claude attach: boom" {
		t.Fatalf("no output: %q", err)
	}
}

func TestAltFilter(t *testing.T) {
	var left, entered int
	f := &altFilter{onLeave: func() { left++ }, onEnter: func() { entered++ }}
	// sequence split across three reads, with text around it
	var out strings.Builder
	for _, chunk := range []string{"hello\x1b[?10", "49", "l bye", "\x1b[?1049h x \x1b"} {
		out.Write(f.Filter([]byte(chunk)))
	}
	out.Write(f.Filter([]byte("[1m"))) // a partial that turns out not to be leaveAlt
	got := out.String()
	if strings.Contains(got, leaveAlt) {
		t.Fatalf("leave sequence leaked: %q", got)
	}
	if want := "hello bye\x1b[?1049h x \x1b[1m"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if left != 1 || entered != 1 {
		t.Fatalf("callbacks: left=%d entered=%d", left, entered)
	}
}
