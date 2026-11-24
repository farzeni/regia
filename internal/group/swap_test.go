package group

import (
	"slices"
	"strings"
	"testing"

	"github.com/farzeni/regia/internal/layout"
)

func TestSwapTakesThePlace(t *testing.T) {
	rm, _ := sessionsFixture(t, nil)
	layout.Update(func(l *layout.Layout) error { l.Pins["w1"] = true; l.Grants["w1"] = "coord"; return nil })

	s, err := SwapAsYou("w1", "lonely", "context stale", "/epics/E1/journal.md")
	if err != nil || !strings.Contains(s, "swapped KPI queries (w1) for lonely (lonely)") {
		t.Fatalf("swap: %q %v", s, err)
	}
	if !slices.Equal(*rm, []string{"w1"}) {
		t.Fatalf("the old session is removed: %v", *rm)
	}
	l := layout.Load()
	if l.Parent["lonely"] != "coord" || !slices.Equal(l.Order["coord"], []string{"lonely", "w2"}) {
		t.Fatalf("lonely takes w1's place: parent %q order %v", l.Parent["lonely"], l.Order["coord"])
	}
	if slices.Contains(l.Order["p:x"], "lonely") {
		t.Fatalf("lonely left its old group: %v", l.Order["p:x"])
	}
	if l.Alias["lonely"] != "KPI queries" || !l.Pins["lonely"] || l.Pins["w1"] || l.Grants["lonely"] != "coord" || !l.Pinned["lonely"] {
		t.Fatalf("name, pin, grant move over: alias %q pins %v grants %v", l.Alias["lonely"], l.Pins, l.Grants)
	}
	if l.Archived["w1"].IsZero() || l.Parent["w1"] != "lonely" {
		t.Fatalf("w1 is archived under lonely until it's gone: %v %q", l.Archived["w1"], l.Parent["w1"])
	}
	chain := l.Swaps["lonely"]
	if len(chain) != 1 || chain[0].From != "w1" || chain[0].Title != "KPI queries" || chain[0].Note != "/epics/E1/journal.md" || chain[0].By != "you" {
		t.Fatalf("the chain: %+v", chain)
	}
	as := Actions()
	if len(as) != 1 || as[0].Tool != "swap" || !as[0].Final || as[0].Name != "you (regia swap)" {
		t.Fatalf("logged, final: %+v", as)
	}
}

func TestSwapChildrenAndChain(t *testing.T) {
	sessionsFixture(t, nil)
	if _, err := SwapAsYou("coord", "lonely", "rotation", ""); err != nil {
		t.Fatal(err)
	}
	l := layout.Load()
	if l.Parent["w1"] != "lonely" || l.Parent["w2"] != "lonely" || l.Parent["lonely"] != "g:1" || !slices.Equal(l.Order["g:1"], []string{"lonely", "a1"}) {
		t.Fatalf("the agents follow: %v / %v", l.Parent, l.Order)
	}
	if _, err := SwapAsYou("lonely", "pp", "rotation", "day 2"); err != nil {
		t.Fatal(err)
	}
	l = layout.Load()
	if c := l.Swaps["pp"]; len(c) != 2 || c[0].From != "coord" || c[1].From != "lonely" || c[1].Title != "orchestrator" || l.Swaps["lonely"] != nil {
		t.Fatalf("the chain goes on, the name stays: %+v", c)
	}
	if l.Alias["pp"] != "orchestrator" || l.Parent["w1"] != "pp" {
		t.Fatalf("pp is the orchestrator now: %q %q", l.Alias["pp"], l.Parent["w1"])
	}
	if b, sw := l.Replaced("coord"); b != "pp" || sw.Reason != "rotation" {
		t.Fatalf("coord was replaced, in the end by pp: %q %+v", b, sw)
	}
}

func TestSwapRefusals(t *testing.T) {
	rm, _ := sessionsFixture(t, map[string]string{"w2": "busy", "pp": "blocked"})
	_, err := SwapIn("coord", "w1", "lonely", "stale", "")
	refused(t, err, "outside what you may organize")
	_, err = SwapIn("coord", "w2", "w1", "stale", "")
	refused(t, err, "working")
	_, err = SwapAsYou("pp", "lonely", "stale", "")
	refused(t, err, "waiting on the user")
	_, err = SwapAsYou("w1", "w1", "stale", "")
	refused(t, err, "itself")
	_, err = SwapAsYou("w1", "coord", "stale", "")
	refused(t, err, "sits under")
	_, err = SwapAsYou("w1", "lonely", " ", "")
	refused(t, err, "reason")
	_, err = SwapAsYou("w1", "nope", "stale", "")
	refused(t, err, "not a listed agent")
	if len(*rm) != 0 || len(Actions()) != 0 {
		t.Fatalf("refused, yet something ran: %v %v", *rm, Actions())
	}
	if _, err := SwapIn("coord", "w1", "w2", "stale", ""); err != nil {
		t.Fatalf("w2 is busy, but it is the new one: allowed (%v)", err)
	}
}

func TestSwapRmRefusedKeepsTheSwap(t *testing.T) {
	sessionsFixture(t, nil)
	s, err := SwapAsYou("a1", "lonely", "stale", "")
	if err != nil || !strings.Contains(s, "claude rm refused copy") || !strings.Contains(s, "stays archived") {
		t.Fatalf("%q %v", s, err)
	}
	l := layout.Load()
	if l.Swaps["lonely"][0].RmErr == "" || l.Parent["lonely"] != "g:1" || l.Archived["a1"].IsZero() {
		t.Fatalf("the swap stands, a1 flagged: %+v", l.Swaps["lonely"])
	}
}

func TestSwapGate(t *testing.T) {
	rm, _ := sessionsFixture(t, nil)
	gatePluginFor(t, "swap", `in=$(cat)
case "$in" in *'"event":"swap"'*'"to":{"id":"w2"'*'"note":"h.md"'*) echo '{}';; *) echo '{"refuse": "no handoff"}';; esac`)
	_, err := SwapIn("coord", "w1", "w2", "stale", "")
	refused(t, err, "handoff: no handoff")
	if _, err := SwapIn("coord", "w1", "w2", "stale", "h.md"); err != nil || !slices.Equal(*rm, []string{"w1"}) {
		t.Fatalf("the gate sees the new session and the note: %v %v", err, *rm)
	}
}

func TestSwapSelfRetiresAfterTurn(t *testing.T) {
	states := map[string]string{"coord": "busy"}
	rm, _ := sessionsFixture(t, states)
	s, err := SwapIn("coord", "coord", "w1", "context stale", "journal.md")
	if err != nil || !strings.Contains(s, "removed once this turn ends") || len(*rm) != 0 {
		t.Fatalf("a busy session swaps itself out, removed later: %q %v %v", s, err, *rm)
	}
	l := layout.Load()
	if l.Retire["coord"] == "" || l.Parent["w1"] != "g:1" || l.Parent["w2"] != "w1" || l.Alias["w1"] != "orchestrator" {
		t.Fatalf("w1 is in place, coord waits: %v %v", l.Retire, l.Parent)
	}

	old := retireWait
	retireWait = 0
	t.Cleanup(func() { retireWait = old })
	if err := Retire("coord"); err == nil || len(*rm) != 0 {
		t.Fatalf("still working: left for later: %v %v", err, *rm)
	}
	delete(states, "coord") // its turn ended
	if err := Retire("coord"); err != nil || !slices.Equal(*rm, []string{"coord"}) {
		t.Fatalf("removed once idle: %v %v", err, *rm)
	}
	if layout.Load().Retire["coord"] != "" {
		t.Fatal("done: no longer waiting")
	}
	if err := Retire("coord"); err != nil {
		t.Fatalf("a second run finds nothing to do: %v", err)
	}
}
