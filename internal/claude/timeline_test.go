package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTimelineFoldsAndFindsPRs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "timeline.jsonl")
	lines := `{"at":"2026-11-15T10:00:00Z","state":"working","detail":"reading"}
{"at":"2026-11-15T10:01:00Z","state":"working","detail":"still reading"}
not json
{"at":"bad","state":"done"}
{"at":"2026-11-15T10:02:00Z","state":"blocked","detail":"merge it?","text":"see https://github.com/o/r/pull/12 ok"}
{"at":"2026-11-15T10:03:00Z","state":"blocked","detail":"merge it?","text":"again https://github.com/o/r/pull/12"}
{"at":"2026-11-15T10:04:00Z","state":"working","detail":"merging"}
{"at":"2026-11-15T10:05:00Z","state":"done","detail":"merged"}
{"at":"2026-11-15T10:06:00Z","state":"weird"}
`
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	info := readTimelineFile(p)
	want := []string{"busy:reading", "blocked:merge it?", "busy:merging", "done:merged"}
	if len(info.Events) != len(want) {
		t.Fatalf("events = %+v", info.Events)
	}
	for i, e := range info.Events {
		if got := e.State + ":" + e.Detail; got != want[i] {
			t.Errorf("event %d = %q, want %q", i, got, want[i])
		}
	}
	at, ok := info.Links["https://github.com/o/r/pull/12"]
	if !ok || at.Minute() != 2 {
		t.Errorf("PR first seen = %v %v, want 10:02", at, ok)
	}
	if got := ReadTimeline("no-such-job-xyz"); len(got.Events) != 0 {
		t.Errorf("missing job gave %+v", got)
	}
}
