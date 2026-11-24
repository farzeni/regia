package layout

import (
	"maps"
	"testing"
	"time"

	"github.com/farzeni/regia/internal/claude"
)

func TestPruneForgetsAfterAWeek(t *testing.T) {
	l, agents := fixture(t)
	l.Alias["w1"] = "kpi"
	l.Pins["w1"] = true
	l.Swaps = map[string][]Swap{"w1": {{From: "w0"}}}
	l.Retire = map[string]string{"w1": "stale"}
	l.Seen = map[string]time.Time{"w1": time.Now(), "b1": time.Now()}
	l.Reparent("w2", "w1", "", agents) // w2 sits under w1
	before := render(l, agents, ViewOpts{ShowDone: true})

	if l.Prune(map[string]*claude.Agent{}, time.Now()) {
		t.Fatal("an empty listing (claude failed) prunes nothing")
	}
	now := time.Now()
	left := maps.Clone(agents)
	delete(left, "w1") // removed: claude rm
	if !l.Prune(left, now) || l.Gone["w1"] != now || len(l.Gone) != 1 {
		t.Fatalf("w1 is noted missing: %v", l.Gone)
	}
	if l.Prune(left, now.Add(ForgetAfter-time.Minute)) || l.Alias["w1"] != "kpi" {
		t.Fatal("kept for a week")
	}
	shown, was := render(l, left, ViewOpts{ShowDone: true}), l.Parent["w1"]

	if !l.Prune(left, now.Add(ForgetAfter)) {
		t.Fatal("forgotten after a week")
	}
	for name, m := range map[string]bool{"alias": l.Alias["w1"] != "", "pins": l.Pins["w1"], "parent": l.Parent["w1"] != "", "seen": !l.Seen["w1"].IsZero(), "gone": !l.Gone["w1"].IsZero(), "swaps": l.Swaps["w1"] != nil, "retire": l.Retire["w1"] != ""} {
		if m {
			t.Errorf("w1 still in %s", name)
		}
	}
	if l.Seen["b1"].IsZero() {
		t.Error("listed agents are kept")
	}
	if got := render(l, left, ViewOpts{ShowDone: true}); got != shown {
		t.Fatalf("the tree looks the same after forgetting:\n%s\n%s (before removal: %s)", shown, got, before)
	}
	if l.Parent["w2"] != was {
		t.Fatalf("w2 takes w1's place in %s: %q", was, l.Parent["w2"])
	}
}

func TestPruneComesBack(t *testing.T) {
	l, agents := fixture(t)
	left := maps.Clone(agents)
	delete(left, "b1")
	now := time.Now()
	l.Prune(left, now)
	if !l.Prune(agents, now.Add(ForgetAfter)) || len(l.Gone) != 0 || l.Parent["b1"] == "" {
		t.Fatalf("listed again: no longer missing, nothing forgotten: %v", l.Gone)
	}
}
