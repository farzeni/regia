package ui

import (
	"bytes"
	"os"
	"os/exec"
)

func copyClipboard(s string) error {
	for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"pbcopy"}} {
		if _, err := exec.LookPath(c[0]); err == nil {
			cmd := exec.Command(c[0], c[1:]...)
			cmd.Stdin = bytes.NewBufferString(s)
			return cmd.Run()
		}
	}
	return os.ErrNotExist
}

func readClipboard() (string, error) {
	for _, c := range [][]string{{"wl-paste", "--no-newline"}, {"xclip", "-selection", "clipboard", "-o"}, {"pbpaste"}} {
		if _, err := exec.LookPath(c[0]); err == nil {
			out, err := exec.Command(c[0], c[1:]...).Output()
			return string(out), err
		}
	}
	return "", os.ErrNotExist
}
