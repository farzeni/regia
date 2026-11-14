package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/layout"
)

// hiddenLine summarizes the rows scrolled out of view, so nobody waiting or
// working goes unnoticed: "↑ 3 · ↓ 2 more · 1 waiting, 1 busy".
func hiddenLine(above, below []layout.Row) string {
	var parts []string
	if len(above) > 0 {
		parts = append(parts, fmt.Sprintf("↑ %d", len(above)))
	}
	if len(below) > 0 {
		parts = append(parts, fmt.Sprintf("↓ %d", len(below)))
	}
	s := "  " + strings.Join(parts, " · ") + " more"
	var busy, blocked int
	for _, rs := range [][]layout.Row{above, below} {
		for _, r := range rs {
			if r.Agent == nil {
				continue
			}
			switch r.Agent.State {
			case "busy":
				busy++
			case "blocked":
				blocked++
			}
		}
	}
	var st []string
	if blocked > 0 {
		st = append(st, lipgloss.NewStyle().Foreground(cBlocked).Render(fmt.Sprintf("%d waiting", blocked)))
	}
	if busy > 0 {
		st = append(st, lipgloss.NewStyle().Foreground(cBusy).Render(fmt.Sprintf("%d busy", busy)))
	}
	if len(st) > 0 {
		return stDim.Render(s+" · ") + strings.Join(st, stDim.Render(", "))
	}
	return stDim.Render(s)
}
