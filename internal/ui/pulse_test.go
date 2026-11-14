package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWorkingIconPulses(t *testing.T) {
	m := testModel(t) // alpha is working, beta is done
	alpha, beta := m.agents["aaaa1111"], m.agents["bbbb2222"]

	cmd := m.startPulse()
	if cmd == nil || !m.pulsing {
		t.Fatal("a working agent should start the pulse")
	}
	if m.startPulse() != nil {
		t.Fatal("started twice")
	}
	seen := map[lipgloss.Color]bool{}
	for i := 0; i < len(pulseRamp); i++ {
		_, c := m.agentGlyph(alpha)
		seen[c] = true
		if _, bc := m.agentGlyph(beta); bc != cDone {
			t.Fatalf("a done agent should not pulse: %v", bc)
		}
		if m.Update(pulseMsg{}); !m.pulsing {
			t.Fatal("stopped while working")
		}
	}
	if len(seen) < 3 {
		t.Fatalf("the working color should change across frames: %v", seen)
	}

	// nothing working: the animation stops, and restarts when work starts
	alpha.State = "done"
	if _, cmd := m.Update(pulseMsg{}); cmd != nil || m.pulsing {
		t.Fatal("pulse should stop when nothing works")
	}
	alpha.State = "busy"
	if m.startPulse() == nil {
		t.Fatal("pulse should restart")
	}
}
