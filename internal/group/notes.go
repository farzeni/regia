package group

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/farzeni/regia/internal/layout"
)

// Note is one line shared with a group: a decision, a fact, a warning or a
// result. Notes are kept forever (one jsonl file per group or coordinator in
// ~/.config/regia/notes); `regia context` shows the latest few.
type Note struct {
	At   time.Time `json:"at"`
	By   string    `json:"by"`   // agent id, or FromUser for notes written in regia
	Name string    `json:"name"` // the author's name when written
	Text string    `json:"text"`
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

const maxNote = 500

func notesDir() string { return filepath.Join(filepath.Dir(layout.Path()), "notes") }

func fileName(container string) string {
	r := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "..", "_")
	return r.Replace(container) + ".jsonl"
}

func notesPath(container string) string { return filepath.Join(notesDir(), fileName(container)) }

// ReadNotes returns a container's notes, oldest first.
func ReadNotes(container string) []Note {
	f, err := os.Open(notesPath(container))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Note
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var n Note
		if json.Unmarshal(sc.Bytes(), &n) == nil && n.Text != "" {
			out = append(out, n)
		}
	}
	return out
}

// AddNote appends a note to a container. The text is made one line and capped.
func AddNote(container string, n Note) error {
	if container == "" {
		return errors.New("no group to write to")
	}
	n.Text = strings.Join(strings.Fields(n.Text), " ")
	if n.Text == "" {
		return errors.New("empty note")
	}
	if r := []rune(n.Text); len(r) > maxNote {
		n.Text = string(r[:maxNote]) + "…"
	}
	if n.At.IsZero() {
		n.At = time.Now()
	}
	_ = os.MkdirAll(notesDir(), 0o755)
	f, err := os.OpenFile(notesPath(container), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(n)
	_, err = f.Write(append(b, '\n')) // one write: O_APPEND keeps concurrent writers apart
	return err
}

// DeleteNotes removes a container's notes (its group was removed).
func DeleteNotes(container string) { _ = os.Remove(notesPath(container)) }

// fromOthers counts the notes in c not written by self.
func fromOthers(c, self string) (int, []Note) {
	var others []Note
	for _, n := range ReadNotes(c) {
		if n.By != self {
			others = append(others, n)
		}
	}
	return len(others), others
}
