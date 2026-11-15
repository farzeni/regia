package group

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/farzeni/regia/internal/layout"
)

// Note is one line on a group's blackboard. Notes are kept (one jsonl file
// per group or coordinator in ~/.config/regia/notes); `regia context` shows
// the latest few. The user can edit or delete them from regia.
type Note struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`   // agent id, or FromUser for notes written in regia
	Name   string    `json:"name"` // the author's name when written
	Text   string    `json:"text"`
	Edited time.Time `json:"edited,omitzero"` // last edit, if any
}

// FromUser is Note.By for notes the user writes in regia.
const FromUser = "you"

// author names who wrote n for an agent reading it. A note the user wrote in
// regia must not read "you: …": agents take that for themselves.
func (n Note) author() string {
	if n.By == FromUser {
		return "the user (in regia)"
	}
	return n.Name
}

// touched is when the note last changed: an edit is news like a new note.
func (n Note) touched() time.Time {
	if n.Edited.After(n.At) {
		return n.Edited
	}
	return n.At
}

// Same says whether a and b are the same note (written at the same instant by
// the same author): how an edit or a delete finds it again.
func Same(a, b Note) bool { return a.At.Equal(b.At) && a.By == b.By }

const maxNote = 500

func notesDir() string { return filepath.Join(filepath.Dir(layout.Path()), "notes") }

func fileName(container string) string {
	r := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "..", "_")
	return r.Replace(container) + ".jsonl"
}

func notesPath(container string) string { return filepath.Join(notesDir(), fileName(container)) }

// ReadNotes returns a container's notes, oldest first.
func ReadNotes(container string) []Note {
	b, err := os.ReadFile(notesPath(container))
	if err != nil {
		return nil
	}
	var out []Note
	for _, line := range bytes.Split(b, []byte("\n")) {
		var n Note
		if json.Unmarshal(line, &n) == nil && n.Text != "" {
			out = append(out, n)
		}
	}
	return out
}

// clean makes a note's text one line, capped; "" if nothing is left.
func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxNote {
		s = string(r[:maxNote]) + "…"
	}
	return s
}

// locked opens a container's notes file and holds an exclusive lock on it
// while do runs: agents append while the user edits, and neither may lose
// the other's change.
func locked(container string, flag int, do func(f *os.File) error) error {
	_ = os.MkdirAll(notesDir(), 0o755)
	f, err := os.OpenFile(notesPath(container), flag, 0o644)
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

// AddNote appends a note to a container. The text is made one line and capped.
func AddNote(container string, n Note) error {
	if container == "" {
		return errors.New("no group to write to")
	}
	if n.Text = clean(n.Text); n.Text == "" {
		return errors.New("empty note")
	}
	if n.At.IsZero() {
		n.At = time.Now()
	}
	b, _ := json.Marshal(n)
	return locked(container, os.O_CREATE|os.O_APPEND|os.O_WRONLY, func(f *os.File) error {
		_, err := f.Write(append(b, '\n'))
		return err
	})
}

// EditNote replaces the text of note n in a container.
func EditNote(container string, n Note, text string) error {
	if text = clean(text); text == "" {
		return errors.New("empty note: delete it instead")
	}
	return rewrite(container, n, func(old Note) *Note {
		old.Text, old.Edited = text, time.Now()
		return &old
	})
}

// DeleteNote removes note n from a container.
func DeleteNote(container string, n Note) error {
	return rewrite(container, n, func(Note) *Note { return nil })
}

// rewrite changes the one line holding note n (nil drops it), in place under
// the lock: a rename would strand an append waiting on the old file. Lines
// it can't read are kept as they are.
func rewrite(container string, n Note, change func(Note) *Note) error {
	return locked(container, os.O_RDWR, func(f *os.File) error {
		b, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		var out bytes.Buffer
		found := false
		for _, line := range bytes.Split(b, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var cur Note
			if !found && json.Unmarshal(line, &cur) == nil && Same(cur, n) {
				found = true
				if c := change(cur); c != nil {
					line, _ = json.Marshal(c)
				} else {
					continue
				}
			}
			out.Write(line)
			out.WriteByte('\n')
		}
		if !found {
			return errors.New("note not found (changed meanwhile?)")
		}
		if err := f.Truncate(0); err != nil {
			return err
		}
		_, err = f.WriteAt(out.Bytes(), 0)
		return err
	})
}

// DeleteNotes removes a container's notes (its group was removed).
func DeleteNotes(container string) { _ = os.Remove(notesPath(container)) }

// fromOthers returns the notes in c not written by self.
func fromOthers(c, self string) []Note {
	var others []Note
	for _, n := range ReadNotes(c) {
		if n.By != self {
			others = append(others, n)
		}
	}
	return others
}

// latest is when the newest of ns last changed.
func latest(ns []Note) time.Time {
	var t time.Time
	for _, n := range ns {
		if n.touched().After(t) {
			t = n.touched()
		}
	}
	return t
}
