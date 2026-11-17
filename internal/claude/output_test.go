package claude

import (
	"strings"
	"testing"
)

// FullOutput reads a result's whole output back: a command's streams, or
// the result's text when there are none (a subagent's report).
func TestFullOutput(t *testing.T) {
	p := "testdata/steps.jsonl"
	got := map[string]string{}
	for _, m := range Messages(p) {
		if m.Result == nil {
			continue
		}
		out, err := FullOutput(p, m.Result)
		if err != nil {
			t.Fatal(err)
		}
		got[m.Result.ID] = out
	}
	if !strings.Contains(got["t3"], "ok line 0") || !strings.Contains(got["t3"], "ok line 19") {
		t.Fatalf("the Bash call's whole output, not its tail: %q", got["t3"])
	}
	if got["t7"] != "The panel looks fine." {
		t.Fatalf("a result without streams is its text: %q", got["t7"])
	}
}
