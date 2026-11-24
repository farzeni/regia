package group

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/plugins"
)

// sessionsFixture is toolsFixture with claude rm and claude stop recorded
// instead of run, and states set per agent (idle otherwise).
func sessionsFixture(t *testing.T, states map[string]string) (rm, stop *[]string) {
	t.Helper()
	toolsFixture(t)
	plugins.SetForTest(nil)
	inner := Agents
	Agents = func() map[string]*claude.Agent {
		as := inner()
		for id, s := range states {
			as[id].State = s
		}
		return as
	}
	var rms, stops []string
	oldRm, oldStop := RunRm, RunStop
	RunRm = func(id string) (string, error) {
		if id == "a1" {
			return "Not removed: worktree has 2 unpushed commits.\n", errors.New("exit status 1")
		}
		rms = append(rms, id)
		return "removed\n", nil
	}
	RunStop = func(id string) (string, error) { stops = append(stops, id); return "", nil }
	t.Cleanup(func() { Agents, RunRm, RunStop = inner, oldRm, oldStop })
	return &rms, &stops
}

func TestRemoveScopedAndLogged(t *testing.T) {
	rm, _ := sessionsFixture(t, nil)
	_, err := Remove("w1", []string{"w2"}, "done")
	refused(t, err, "call claim")
	_, err = Remove("coord", []string{"w1"}, "  ")
	refused(t, err, "reason")
	_, err = Remove("coord", []string{"coord"}, "done")
	refused(t, err, "yourself")
	_, err = Remove("coord", []string{"lonely"}, "done")
	refused(t, err, "outside what you may organize")
	if len(*rm) != 0 {
		t.Fatalf("refused, yet claude rm ran: %v", *rm)
	}

	s, err := Remove("coord", []string{"w1"}, "handoff written")
	if err != nil || !slices.Equal(*rm, []string{"w1"}) || !strings.Contains(s, "removed 1 session") {
		t.Fatalf("coord removes its agent: %q %v %v", s, err, *rm)
	}
	as := Actions()
	if len(as) != 1 || !as[0].Final || as[0].Tool != "remove" || !strings.Contains(as[0].Summary, "handoff written") {
		t.Fatalf("logged with the reason, final: %+v", as)
	}
	if err := Undo(as[0]); err == nil || !strings.Contains(err.Error(), "can't be put back") {
		t.Fatalf("a removal can't be undone: %v", err)
	}
}

func TestRemoveRefusesLiveWork(t *testing.T) {
	rm, _ := sessionsFixture(t, map[string]string{"w1": "busy", "w2": "blocked"})
	_, err := Remove("coord", []string{"w1"}, "done")
	refused(t, err, "working")
	_, err = Remove("coord", []string{"w2"}, "done")
	refused(t, err, "waiting on the user")
	if len(*rm) != 0 {
		t.Fatalf("claude rm ran: %v", *rm)
	}
}

func TestRemoveChildrenFirst(t *testing.T) {
	rm, _ := sessionsFixture(t, nil)
	grant(t, "boss", Everything)
	_, err := Remove("boss", []string{"coord"}, "done")
	refused(t, err, "still listed under it (w1, w2)")

	// all in one call, the coordinator first: its agents still go before it
	s, err := Remove("boss", []string{"coord", "w1", "w2"}, "done")
	if err != nil || len(*rm) != 3 || (*rm)[2] != "coord" {
		t.Fatalf("children first: %q %v %v", s, err, *rm)
	}
}

func TestRemovePartlyRefused(t *testing.T) {
	rm, _ := sessionsFixture(t, nil)
	grant(t, "boss", Everything)
	s, err := Remove("boss", []string{"a1", "lonely"}, "done")
	if err != nil || !slices.Equal(*rm, []string{"lonely"}) || !strings.Contains(s, "refused copy (a1): Not removed: worktree has 2 unpushed commits.") {
		t.Fatalf("claude rm's refusal is passed on: %q %v", s, err)
	}
	_, err = Remove("boss", []string{"a1"}, "done")
	refused(t, err, "unpushed commits")
}

// gatePlugin installs a plugin that gates remove by running script.
func gatePlugin(t *testing.T, script string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "handoff")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"command": ["./gate.sh"], "gates": ["remove"]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "gate.sh"), []byte("#!/bin/sh\n"+script), 0o755)
	p, err := plugins.LoadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	plugins.SetForTest([]*plugins.Manifest{p})
}

func TestRemoveGate(t *testing.T) {
	rm, _ := sessionsFixture(t, nil)
	// the gate reads who asks and why, and refuses until the handoff exists
	gatePlugin(t, `in=$(cat)
case "$in" in *'"event":"remove"'*'"by":"orchestrator"'*'"reason":"done"'*) ;; *) echo "bad input: $in" >&2; exit 1;; esac
case "$in" in *'"id":"w1"'*) echo '{"refuse": "no handoff yet"}';; *) echo '{}';; esac`)
	_, err := Remove("coord", []string{"w1"}, "done")
	refused(t, err, "handoff: no handoff yet")
	if _, err := Remove("coord", []string{"w2"}, "done"); err != nil || !slices.Equal(*rm, []string{"w2"}) {
		t.Fatalf("the gate lets w2 go: %v %v", err, *rm)
	}

	gatePlugin(t, "exit 3")
	_, err = Remove("coord", []string{"w1"}, "done")
	refused(t, err, "handoff couldn't check it")
	if len(*rm) != 1 {
		t.Fatalf("a failing gate refuses: %v", *rm)
	}
}

func TestStop(t *testing.T) {
	_, stop := sessionsFixture(t, map[string]string{"w1": "busy", "w2": "done"})
	s, err := Stop("coord", []string{"w1"}, "runaway loop")
	if err != nil || !slices.Equal(*stop, []string{"w1"}) || !strings.Contains(s, "stopped 1 session") {
		t.Fatalf("a working agent can be stopped: %q %v", s, err)
	}
	_, err = Stop("coord", []string{"w2"}, "quiet")
	refused(t, err, "already done")
	_, err = Stop("w1", []string{"w2"}, "quiet")
	refused(t, err, "call claim")
	if as := Actions(); len(as) != 1 || !as[0].Final || Undo(as[0]) == nil {
		t.Fatalf("a stop is logged, not undoable: %+v", as)
	}
}
