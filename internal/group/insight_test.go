package group

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
)

// insightFixture is toolsFixture with states, a timeline for w1 and a
// transcript where w2 edits a file, all in temp dirs.
func insightFixture(t *testing.T) {
	t.Helper()
	toolsFixture(t)
	cdir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cdir)
	now := time.Now()
	at := func(d time.Duration) string { return now.Add(-d).UTC().Format(time.RFC3339) }
	job := filepath.Join(cdir, "jobs", "w1")
	os.MkdirAll(job, 0o755)
	os.WriteFile(filepath.Join(job, "timeline.jsonl"), []byte(strings.Join([]string{
		`{"at":"` + at(30*time.Hour) + `","state":"working","detail":"long ago"}`,
		`{"at":"` + at(26*time.Hour) + `","state":"done","detail":"first pass"}`,
		`{"at":"` + at(2*time.Hour) + `","state":"working","detail":"KPI queries"}`,
		`{"at":"` + at(time.Hour) + `","state":"blocked","detail":"which table?"}`,
	}, "\n")+"\n"), 0o644)

	cwd := t.TempDir()
	tr := filepath.Join(t.TempDir(), "w2.jsonl")
	os.WriteFile(tr, []byte(strings.Join([]string{
		`{"type":"user","timestamp":"2026-11-14T09:00:00Z","message":{"content":"lay it out"}}`,
		`{"type":"assistant","timestamp":"2026-11-14T09:01:00Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"` + cwd + `/web/app.go"}}]}}`,
		`{"type":"user","timestamp":"2026-11-14T09:01:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]},"toolUseResult":{"filePath":"` + cwd + `/web/app.go","structuredPatch":[{"oldStart":3,"oldLines":1,"newStart":3,"newLines":2,"lines":[" a","-b","+c","+d"]}]}}`,
	}, "\n")+"\n"), 0o644)

	states := map[string]string{"w1": "blocked", "w2": "busy", "a1": "failed", "lonely": "blocked"}
	listed := map[string]string{"coord": "orchestrator", "w1": "KPI queries", "w2": "layout", "a1": "copy", "lonely": "lonely", "pp": "pp", "boss": "boss"}
	Agents = func() map[string]*claude.Agent {
		out := map[string]*claude.Agent{}
		for id, n := range listed {
			st := states[id]
			if st == "" {
				st = "idle"
			}
			out[id] = &claude.Agent{ID: id, Name: n, State: st, Needs: "which table?", UpdatedAt: now.Add(-time.Hour), StartedAt: now.Add(-3 * time.Hour)}
		}
		out["w2"].Transcript, out["w2"].Cwd = tr, cwd
		return out
	}
}

func TestInbox(t *testing.T) {
	insightFixture(t)
	in, err := Inbox("coord")
	if err != nil || len(in) != 1 || in[0].ID != "w1" || in[0].Why != "waiting" || in[0].Question != "which table?" {
		t.Fatalf("coord sees w1 waiting, nothing outside: %+v %v", in, err)
	}
	grant(t, "boss", Everything)
	in, _ = Inbox("boss")
	var got []string
	for _, it := range in {
		got = append(got, it.ID+":"+it.Why)
	}
	if strings.Join(got, ",") != "lonely:waiting,w1:waiting,a1:failed" {
		t.Fatalf("whole tree, waiting before failed: %v", got)
	}
	layout.Update(func(l *layout.Layout) error { l.Archive("lonely"); return nil })
	if in, _ := Inbox("boss"); len(in) != 2 {
		t.Fatalf("archived agents are left out: %+v", in)
	}
}

func TestTimeline(t *testing.T) {
	insightFixture(t)
	tl, err := Timeline("w2", "", time.Now().Add(-24*time.Hour))
	if err != nil || tl.Of != "orchestrator" {
		t.Fatalf("a worker's timeline is its group's: %+v %v", tl, err)
	}
	var got []string
	for _, e := range tl.Events {
		got = append(got, e.ID+" "+e.What+": "+e.Text)
	}
	// the first event (30h ago) is older than asked for, so the 2h one reads "back to work"
	if strings.Join(got, " | ") != "w1 back to work: KPI queries | w1 waiting: which table?" {
		t.Fatalf("events: %v", got)
	}
	_, err = Timeline("w2", "lonely", time.Time{})
	refused(t, err, "outside what you may read")
	_, err = Timeline("w2", "nobody", time.Time{})
	refused(t, err, "no group or agent")
}

func TestChanges(t *testing.T) {
	insightFixture(t)
	ch, err := Changes("w1", "w2")
	if err != nil || len(ch.Files) != 1 || ch.Files[0].File != filepath.Join("web", "app.go") || ch.Files[0].Added != 2 || ch.Files[0].Removed != 1 || ch.Files[0].Line != 4 {
		t.Fatalf("a sibling's changes: %+v %v", ch, err)
	}
	_, err = Changes("w1", "lonely")
	refused(t, err, "outside what you may read")
}

func TestRenameAndPin(t *testing.T) {
	insightFixture(t)
	if s, err := RenameAgent("coord", "w1", "  kpi   queries "); err != nil || !strings.Contains(s, `to "kpi queries"`) {
		t.Fatalf("rename: %q %v", s, err)
	}
	if _, err := RenameAgent("coord", "coord", "lead"); err != nil {
		t.Fatalf("an organizer may rename itself: %v", err)
	}
	if l := layout.Load(); l.Alias["w1"] != "kpi queries" || l.Alias["coord"] != "lead" {
		t.Fatalf("aliases: %v", l.Alias)
	}
	_, err := RenameAgent("w1", "w1", "me")
	refused(t, err, "call claim")
	_, err = RenameAgent("coord", "g:1", "x")
	refused(t, err, "rename_group renames groups")
	_, err = RenameAgent("coord", "a1", "x")
	refused(t, err, "outside what you may organize")

	if _, err := Pin("coord", []string{"w1", "w2"}, true); err != nil || !layout.Load().Pins["w2"] {
		t.Fatalf("pin: %v", err)
	}
	if _, err := Pin("coord", []string{"w2"}, false); err != nil || layout.Load().Pins["w2"] {
		t.Fatalf("unpin: %v", err)
	}
	_, err = Pin("coord", []string{"w1", "lonely"}, true)
	refused(t, err, "outside what you may organize")

	as := Actions()
	if len(as) != 4 || as[0].Tool != "rename_agent" || as[2].Summary != "pinned 2 agents" {
		t.Fatalf("logged: %+v", as)
	}
	if err := Undo(as[0]); err != nil || layout.Load().Alias["w1"] != "" {
		t.Fatalf("undo rename: %v", err)
	}
}
