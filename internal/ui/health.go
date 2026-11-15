package ui

import (
	"time"

	"github.com/farzeni/regia/internal/claude"
)

// Stuck detection (claude.Health): a busy agent with no activity for a while,
// or repeating the same failing tool call, gets a dim ⚠ on its row, a line on
// its card saying why, and an inbox item after the questions. It's a guess, so
// it's never counted in "need you".

const stuckGlyph = "⚠"

// checkHealth runs on every refresh. It only stats files and reads what was
// appended to busy agents' transcripts since the last tick.
func (m *model) checkHealth(now time.Time) {
	for _, a := range m.agents {
		a.Unhealthy = claude.Health(a, now)
	}
}

// stuck: a busy agent that looks stuck, for the inbox.
func stuck(a *claude.Agent) bool {
	return a.State == "busy" && a.Unhealthy != ""
}
