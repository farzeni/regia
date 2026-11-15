package ui

import (
	"testing"
	"time"

	"github.com/farzeni/regia/internal/claude"
)

func TestFaded(t *testing.T) {
	now := time.Now()
	recent, old := now.Add(-2*time.Hour), now.Add(-25*time.Hour)
	for _, c := range []struct {
		state string
		at    time.Time
		stale bool
		want  bool
	}{
		{"done", recent, false, false},
		{"done", old, false, true},
		{"failed", recent, false, false},
		{"failed", old, false, true},
		{"busy", old, false, false},
		{"blocked", old, true, true},
		{"blocked", recent, false, false},
	} {
		a := &claude.Agent{State: c.state, UpdatedAt: c.at, Stale: c.stale}
		if got := faded(a, now); got != c.want {
			t.Errorf("%s %v ago stale=%v: faded=%v, want %v", c.state, now.Sub(c.at), c.stale, got, c.want)
		}
	}
}
