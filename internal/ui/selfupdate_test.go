package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/update"
)

func TestAutoUpdateNotice(t *testing.T) {
	m := testModel(t)
	m.Update(updatedMsg{ok: false})
	if strings.Contains(ansi.Strip(m.View()), "⬆") {
		t.Fatal("notice without an update")
	}
	m.Update(updatedMsg{r: update.Result{From: "v1", To: "v2"}, ok: true})
	if head := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.Contains(head, "updated to v2 · restart regia") {
		t.Fatalf("header: %q", head)
	}
	m.Update(updatedMsg{r: update.Result{To: "v3", Err: errors.New("offline")}, ok: true})
	if head := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.Contains(head, "update to v3 failed") {
		t.Fatalf("header: %q", head)
	}
}

func TestInitDoesNotUpdateDevBuilds(t *testing.T) {
	// the test binary is a dev build: the real check must return without
	// asking the network or installing anything
	if _, ok := update.Auto(t.Context(), time.Now()); ok {
		t.Fatal("dev build auto-updated")
	}
}
