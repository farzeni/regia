package ui

import (
	"os/exec"
	"strings"
)

type removedMsg struct {
	id, out string
	err     error
}

// removeAgent runs `claude rm <id>`: the session leaves the list and its
// worktree is deleted when that's safe; the conversation stays resumable.
// It refuses (non-zero exit, with a reason) e.g. over unpushed commits, and
// regia never passes the --discard/--force flags. A variable for tests.
var removeAgent = func(id string) (string, error) {
	out, err := exec.Command("claude", "rm", id).CombinedOutput()
	return string(out), err
}

// oneLine condenses command output for the status line.
func oneLine(out string, err error) string {
	s := strings.Join(strings.Fields(out), " ")
	if s == "" && err != nil {
		s = err.Error()
	}
	return s
}
