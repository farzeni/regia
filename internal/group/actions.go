package group

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/farzeni/regia/internal/layout"
)

// Action is one change an agent made to regia's tree through regia mcp,
// kept in ~/.config/regia/actions.jsonl so you can see it and undo it in
// regia.
type Action struct {
	At      time.Time     `json:"at"`
	By      string        `json:"by"`   // the agent's id
	Name    string        `json:"name"` // its name then
	Tool    string        `json:"tool"`
	Summary string        `json:"summary"` // "moved 3 agents into backend"
	Change  layout.Change `json:"change"`
	Undone  time.Time     `json:"undone,omitzero"`
	Final   bool          `json:"final,omitempty"` // reached into Claude Code (remove, stop): can't be undone here
}

// finalWhy says why a final action can't be undone, and what to do instead.
func finalWhy(a Action) string {
	if a.Tool == "stop" {
		return "a stopped session can't be undone here: it starts again when you message it"
	}
	return "a removed session can't be put back: find it with ctrl+g in search and resume it"
}

// Same says whether a and b are the same action.
func (a Action) Same(b Action) bool { return a.At.Equal(b.At) && a.By == b.By }

const (
	keepActions = 300 // kept when the log is trimmed
	maxActions  = 500 // the log is trimmed past this
)

func actionsPath() string { return filepath.Join(filepath.Dir(layout.Path()), "actions.jsonl") }

// withActions holds an exclusive lock on the log while do runs with it open.
func withActions(do func(f *os.File) error) error {
	_ = os.MkdirAll(filepath.Dir(actionsPath()), 0o755)
	f, err := os.OpenFile(actionsPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return do(f)
}

func readActions(f *os.File) []Action {
	var out []Action
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		var a Action
		if json.Unmarshal(sc.Bytes(), &a) == nil && a.By != "" {
			out = append(out, a)
		}
	}
	return out
}

func writeActions(f *os.File, as []Action) error {
	var b bytes.Buffer
	for _, a := range as {
		line, err := json.Marshal(a)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.WriteAt(b.Bytes(), 0)
	return err
}

// Record appends an action to the log, trimming the oldest when it grows long.
func Record(a Action) error {
	return withActions(func(f *os.File) error {
		as := append(readActions(f), a)
		if len(as) > maxActions {
			return writeActions(f, as[len(as)-keepActions:])
		}
		line, err := json.Marshal(a)
		if err != nil {
			return err
		}
		if _, err := f.Seek(0, 2); err != nil {
			return err
		}
		_, err = f.Write(append(line, '\n'))
		return err
	})
}

// Actions returns the agents' actions, oldest first.
func Actions() []Action {
	f, err := os.Open(actionsPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	return readActions(f)
}

// ActionsChanged reports the log's mtime, so regia can notice new actions
// without reading it.
func ActionsChanged() time.Time {
	fi, err := os.Stat(actionsPath())
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// Undo reverts action a on the tree and marks it undone. It refuses when a
// was undone already or what it changed changed again since.
func Undo(a Action) error {
	return withActions(func(f *os.File) error {
		as := readActions(f)
		for i := range as {
			if !as[i].Same(a) {
				continue
			}
			if !as[i].Undone.IsZero() {
				return errors.New("already undone")
			}
			if as[i].Final {
				return errors.New(finalWhy(as[i]))
			}
			if err := layout.Revert(as[i].Change); err != nil {
				return err
			}
			as[i].Undone = time.Now()
			return writeActions(f, as)
		}
		return errors.New("not in the log any more")
	})
}
