package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTaskTitle(t *testing.T) {
	for in, want := range map[string]string{
		"# CS Giacenze: impl — correzione\nFai parte dello **sciame**": "CS Giacenze: impl — correzione",
		"\n\n  **bold** `code` here\nmore":                             "bold code here",
		"plain task":                                                   "plain task",
		"> quoted\nx":                                                  "quoted",
	} {
		if got := taskTitle(in); got != want {
			t.Errorf("taskTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNeedsLines(t *testing.T) {
	strip := func(ls []string) string {
		for i := range ls {
			ls[i] = strings.TrimRight(ansi.Strip(ls[i]), " ")
		}
		return strings.Join(ls, "\n")
	}
	got := strip(needsLines("Decidere: merge su master (deploy); valore della Busta A4; rotazione del token", 80))
	if want := "Decidere:\n• merge su master (deploy)\n• valore della Busta A4\n• rotazione del token"; got != want {
		t.Errorf("semicolons:\n%s", got)
	}
	got = strip(needsLines("Decide: update the SKUs or flag them? Threshold and cron time? Revoke the old token.", 80))
	if want := "Decide:\n• update the SKUs or flag them?\n• Threshold and cron time?\n• Revoke the old token."; got != want {
		t.Errorf("questions:\n%s", got)
	}
	got = strip(needsLines("Com'è nel tuo caso?", 80))
	if got != "Com'è nel tuo caso?" {
		t.Errorf("single question: %q", got)
	}
}
