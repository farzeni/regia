package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/claude"
)

// Working agents' icons pulse: their green breathes up and down about once a
// second. The animation only ticks while some agent is working, so an idle
// regia doesn't redraw.

type pulseMsg struct{}

const pulseEvery = 125 * time.Millisecond

// pulseRamp is the 256-color green ramp around cBusy (42): dim to bright and
// back, 8 frames.
var pulseRamp = []lipgloss.Color{"30", "36", "42", "48", "48", "42", "36", "30"}

func pulseTick() tea.Cmd {
	return tea.Tick(pulseEvery, func(time.Time) tea.Msg { return pulseMsg{} })
}

func working(a *claude.Agent) bool { return a != nil && !a.Interactive && a.State == "busy" }

func (m *model) anyWorking() bool {
	for _, a := range m.agents {
		if working(a) {
			return true
		}
	}
	return false
}

// startPulse starts the animation if an agent is working and it isn't running.
func (m *model) startPulse() tea.Cmd {
	if m.pulsing || !m.anyWorking() {
		return nil
	}
	m.pulsing = true
	return pulseTick()
}

// onPulse advances a frame, or stops when nothing is working any more.
func (m *model) onPulse() tea.Cmd {
	if !m.anyWorking() {
		m.pulsing = false
		return nil
	}
	m.pulseN++
	return pulseTick()
}

// agentGlyph is glyph with the working color pulsing.
func (m *model) agentGlyph(a *claude.Agent) (string, lipgloss.Color) {
	g, c := glyph(a)
	if working(a) {
		c = pulseRamp[m.pulseN%len(pulseRamp)]
	}
	return g, c
}
