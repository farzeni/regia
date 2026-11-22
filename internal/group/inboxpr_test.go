package group

import (
	"strings"
	"testing"
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/prinfo"
)

// A finished agent whose PR fails CI is in the inbox, after the questions.
func TestInboxPR(t *testing.T) {
	insightFixture(t)
	dir := t.TempDir()
	inner := Agents
	Agents = func() map[string]*claude.Agent {
		as := inner()
		as["w2"].State, as["w2"].Cwd, as["w2"].Branch = "done", dir, "feat-ci"
		as["w2"].UpdatedAt = time.Now().Add(-2 * time.Hour)
		return as
	}
	prinfo.Seed(dir, "feat-ci", prinfo.Info{OK: true, Number: 31, State: "open", Checks: "failing", URL: "https://gh/o/r/pull/31"})
	in, err := Inbox("coord")
	if err != nil || len(in) != 2 || in[0].ID != "w1" || in[1].ID != "w2" || in[1].Why != "pr" {
		t.Fatalf("w1 waiting, then w2's PR: %+v %v", in, err)
	}
	if in[1].PR != "PR #31: CI failing https://gh/o/r/pull/31" {
		t.Fatalf("pr: %q", in[1].PR)
	}
	prinfo.Seed(dir, "feat-ci", prinfo.Info{OK: true, Number: 31, State: "merged", Checks: "failing"})
	if in, _ := Inbox("coord"); len(in) != 1 || strings.Contains(in[0].ID, "w2") {
		t.Fatalf("a merged PR needs nothing: %+v", in)
	}
}
