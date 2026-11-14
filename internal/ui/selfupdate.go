package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/farzeni/regia/internal/update"
)

// autoUpdate runs in the background when the UI opens: for an installed
// build it checks at most once a day and installs a newer version in place.
// A variable so tests can stub it.
var autoUpdate = func() (update.Result, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return update.Auto(ctx, time.Now())
}

type updatedMsg struct {
	r  update.Result
	ok bool
}

func autoUpdateCmd() tea.Msg {
	r, ok := autoUpdate()
	return updatedMsg{r, ok}
}

// noteUpdate keeps the outcome in the header until regia restarts.
func (m *model) noteUpdate(msg updatedMsg) {
	switch {
	case !msg.ok:
	case msg.r.Err != nil:
		m.updateNote = "update to " + msg.r.To + " failed · regia update"
		m.flash("auto-update: %v", msg.r.Err)
	default:
		m.updateNote = "updated to " + msg.r.To + " · restart regia"
	}
}
