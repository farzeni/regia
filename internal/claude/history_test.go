package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic job shaped like the real files (timeline.jsonl + transcript).
func TestHistory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	job := filepath.Join(dir, "jobs", "abcd1234")
	_ = os.MkdirAll(job, 0o755)
	tl := strings.Join([]string{
		`{"at":"2026-10-19T07:00:00Z","state":"working","detail":"firewall applicato","text":""}`,                // you, sent while busy
		`{"at":"2026-10-19T07:01:00Z","state":"working","detail":"Verifying firewalls","text":""}`,               // step
		`{"at":"2026-10-19T07:02:00Z","state":"done","detail":"firewall ok","text":"Il firewall funziona"}`,      // result
		`{"at":"2026-10-24T09:38:00Z","state":"done","detail":"aggiorna portainer all'ultima lts","text":""}`,    // you: new task (after done, >2h)
		`{"at":"2026-10-24T09:42:00Z","state":"blocked","detail":"need node-1 access","text":"Mi serve node-1"}`, // result, waits on you
		`{"at":"2026-10-24T09:45:00Z","state":"blocked","detail":"ho questo <pasted_content id=\"b77f\"> services: x","text":""}`,
		`{"at":"2026-10-24T09:46:00Z","state":"blocked","detail":"ho questo <pasted_content id=\"b77f\"> services: x","text":""}`, // echo
		`{"at":"2026-10-24T09:47:00Z","state":"blocked","detail":"plan ready","text":"Il piano è pronto"}`,
	}, "\n")
	_ = os.WriteFile(filepath.Join(job, "timeline.jsonl"), []byte(tl), 0o644)
	tr := strings.Join([]string{
		`{"type":"user","message":{"content":"firewall applicato"}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"x"}]}}`,
		`{"type":"user","message":{"content":"<task-notification>done</task-notification>"}}`,
		`{"type":"user","message":{"content":"aggiorna portainer all'ultima lts"}}`,
		`{"type":"user","message":{"content":"ho questo \n\n<pasted_content id=\"b77f\">\nservices: x"}}`,
	}, "\n")
	trPath := filepath.Join(dir, "t.jsonl")
	_ = os.WriteFile(trPath, []byte(tr), 0o644)

	h := ReadHistory(&Agent{ID: "abcd1234", Transcript: trPath})
	var got []string
	for _, tn := range h.Turns {
		s := "you:" + tn.User.Detail
		for _, r := range tn.Results {
			s += " -> " + r.State + ":" + r.Detail
		}
		if tn.Steps > 0 {
			s += " +steps"
		}
		if tn.Repeats > 0 {
			s += " x2"
		}
		got = append(got, s)
	}
	want := []string{
		"you:firewall applicato -> done:firewall ok +steps",
		"you:aggiorna portainer all'ultima lts -> blocked:need node-1 access",
		`you:ho questo <pasted_content id="b77f"> services: x -> blocked:plan ready x2`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("turns\n got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if h.Task != "aggiorna portainer all'ultima lts" {
		t.Fatalf("task = %q (a follow-up after a blocked result continues the task)", h.Task)
	}
	if h.Status == nil || h.Status.Detail != "plan ready" {
		t.Fatalf("status = %+v", h.Status)
	}
	if Tidy(`ho questo <pasted_content id="b77f"> services: x`) != "ho questo [pasted]" {
		t.Fatalf("tidy = %q", Tidy(`ho questo <pasted_content id="b77f"> services: x`))
	}
}
