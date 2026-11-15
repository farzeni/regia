package plugins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePlugin(t *testing.T, root, name, manifest, script string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if script != "" {
		if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParse(t *testing.T) {
	o, err := Parse([]byte(`{"fields":[{"label":"jira","text":"PROJ-1 · In Review","status":"ok","url":"https://x"}],
		"badge":"J","attention":"look","actions":[{"id":"mv","label":"move"},{"label":"no id"}],"every":"30s"}`))
	if _, w := err.(*Warning); !w {
		t.Fatalf("an action without id is a warning, got %v", err)
	}
	if len(o.Fields) != 1 || o.Fields[0].URL != "https://x" || o.Badge.Text != "J" || o.Attention != "look" {
		t.Fatalf("%+v", o)
	}
	if len(o.Actions) != 1 || o.Actions[0].ID != "mv" {
		t.Fatalf("actions %+v", o.Actions)
	}

	o, err = Parse([]byte(`{"badge":{"text":"CI!","status":"error"}}`))
	if o.Badge.Text != "CI" || o.Badge.Status != "error" || err == nil {
		t.Fatalf("badge object, cut to 2: %+v %v", o.Badge, err)
	}
	if o, err := Parse([]byte("  \n")); err != nil || len(o.Fields) != 0 {
		t.Fatal("empty stdout is an empty result")
	}
	if _, err := Parse([]byte("not json")); err == nil {
		t.Fatal("garbage is an error")
	}
	if _, err := Parse([]byte(`{"fields":[{"text":"x","status":"red"}]}`)); err == nil {
		t.Fatal("unknown status is a warning")
	}
	if o, err := Parse([]byte(`{"actions":[{"id":"x"}]}`)); err != nil || o.Actions[0].Label != "x" {
		t.Fatal("label defaults to id")
	}
}

func TestApplies(t *testing.T) {
	home, _ := os.UserHomeDir()
	m := &Manifest{Dirs: []string{"~/work", "/srv/*/repo"}, States: []string{"done"}}
	cases := []struct {
		a    AgentInfo
		want bool
	}{
		{AgentInfo{Cwd: home + "/work/api/.claude/worktrees/x", State: "done"}, true},
		{AgentInfo{Cwd: home + "/work", State: "ready"}, true}, // ready is done, not seen yet
		{AgentInfo{Cwd: home + "/workshop", State: "done"}, false},
		{AgentInfo{Cwd: "/srv/a/repo/sub", State: "done"}, true},
		{AgentInfo{Cwd: home + "/work/api", State: "busy"}, false},
		{AgentInfo{Cwd: home + "/work/api", State: "done", Interactive: true}, false},
	}
	for _, c := range cases {
		if got := m.Applies(c.a); got != c.want {
			t.Errorf("%+v: got %v", c.a, got)
		}
	}
	if !(&Manifest{}).Applies(AgentInfo{State: "busy"}) {
		t.Error("no dirs, no states: every background agent")
	}
	if !(&Manifest{Dirs: []string{"~/work/**"}}).Applies(AgentInfo{Cwd: home + "/work/x"}) {
		t.Error("trailing /** means the same as the directory")
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	writePlugin(t, root, "good", `{"command":["./check.sh"],"every":"1m"}`, "echo {}")
	writePlugin(t, root, "off", `{"command":["x"],"disabled":true}`, "")
	writePlugin(t, root, "typo", `{"comand":["x"]}`, "")
	writePlugin(t, root, "nocmd", `{"every":"1m"}`, "")
	writePlugin(t, root, "badstate", `{"command":["x"],"states":["waiting"]}`, "")
	_ = os.MkdirAll(filepath.Join(root, "notaplugin"), 0o755) // no plugin.json: ignored
	ps, errs := Load(root)
	if len(ps) != 1 || ps[0].Name != "good" || ps[0].every() != time.Minute {
		t.Fatalf("plugins %+v", ps)
	}
	if len(errs) != 3 {
		t.Fatalf("errors %+v", errs)
	}
	for _, e := range errs {
		if e.Name == "typo" && !strings.Contains(e.Err.Error(), "comand") {
			t.Errorf("unknown key named: %v", e.Err)
		}
	}
}

func TestRun(t *testing.T) {
	root := t.TempDir()
	// echoes back what it was told, through stdin and the environment
	dir := writePlugin(t, root, "echo", `{"command":["./check.sh","arg1"],"timeout":"5s"}`, `
in=$(cat)
case "$in" in *'"branch":"PROJ-7-x"'*) b=yes;; *) b=no;; esac
printf '{"fields":[{"text":"%s %s %s %s"}]}' "$b" "$REGIA_EVENT" "$1" "$(pwd)"
`)
	p, err := LoadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	in := Input{Version: 1, Event: "refresh", Agent: AgentInfo{ID: "a1", Branch: "PROJ-7-x"}}
	out, _, err := Run(context.Background(), p, in, work)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := out.Fields[0].Text, "yes refresh arg1 "+work; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	out, _, _ = Run(context.Background(), p, in, filepath.Join(work, "gone"))
	if !strings.HasSuffix(out.Fields[0].Text, dir) {
		t.Fatalf("agent dir gone: runs in the plugin's, got %q", out.Fields[0].Text)
	}

	// an interpreter with the script as argument: the script is found in the plugin dir, flags and words pass through
	py := writePlugin(t, root, "py", `{"command":["sh","check.sh","-x","word"]}`, `printf '{"fields":[{"text":"%s %s"}]}' "$1" "$2"`)
	p, _ = LoadOne(py)
	out, _, err = Run(context.Background(), p, in, work)
	if err != nil || out.Fields[0].Text != "-x word" {
		t.Fatalf("interpreter + script arg: %+v %v", out, err)
	}

	fail := writePlugin(t, root, "fail", `{"command":["./check.sh"]}`, "echo 'jira: not logged in' >&2; exit 3")
	p, _ = LoadOne(fail)
	if _, se, err := Run(context.Background(), p, in, ""); err == nil || !strings.Contains(err.Error(), "not logged in") || se == "" {
		t.Fatalf("exit 3: %v / %q", err, se)
	}

	slow := writePlugin(t, root, "slow", `{"command":["./check.sh"],"timeout":"200ms"}`, "sleep 5")
	p, _ = LoadOne(slow)
	t0 := time.Now()
	if _, _, err := Run(context.Background(), p, in, ""); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: %v", err)
	}
	if time.Since(t0) > 3*time.Second {
		t.Fatal("the timeout didn't stop it")
	}
}

func TestCacheAndBackoff(t *testing.T) {
	root := t.TempDir()
	counter := filepath.Join(root, "n")
	dir := writePlugin(t, root, "count", `{"command":["./check.sh"],"every":"1m"}`,
		`echo x >> `+counter+`; if [ "$REGIA_EVENT" = action ]; then echo '{"message":"done"}'; else echo '{"fields":[{"text":"ok"}],"badge":"C"}'; fi`)
	p, _ := LoadOne(dir)
	SetForTest([]*Manifest{p})
	build := func(last *Output) (Input, string) { return Input{Agent: AgentInfo{ID: "a1"}}, "" }

	f := Refresher(p, "a1", build)
	if f == nil {
		t.Fatal("first run is due")
	}
	if Refresher(p, "a1", build) != nil {
		t.Fatal("in flight: not started twice")
	}
	f()
	rs := Results("a1")
	if len(rs) != 1 || rs[0].Out.Badge.Text != "C" || !rs[0].OK() {
		t.Fatalf("%+v", rs)
	}
	if Refresher(p, "a1", build) != nil {
		t.Fatal("fresh: not due for a minute")
	}

	// an action with nothing to show keeps the fields and asks for a refresh
	out, err := RunAction(p, "a1", "go", Input{Agent: AgentInfo{ID: "a1"}}, "")
	if err != nil || out.Message != "done" {
		t.Fatalf("%+v %v", out, err)
	}
	if rs := Results("a1"); rs[0].Out.Badge == nil {
		t.Fatal("action without fields keeps the last result")
	}
	if f := Refresher(p, "a1", build); f == nil {
		t.Fatal("after an action, a refresh is due")
	} else {
		f()
	}

	// failures keep the last good output, say why, and back off
	r := Result{Plugin: p, Fetched: time.Now().Add(-90 * time.Second), fails: 2}
	if due(r, time.Now()) {
		t.Fatal("2 fails at every=1m: next run after 2m")
	}
	r.fails = 20
	r.Fetched = time.Now().Add(-29 * time.Minute)
	if due(r, time.Now()) {
		t.Fatal("backoff tops out at 30m, not before")
	}
	store(p, "a1", Output{}, os.ErrPermission, false)
	if rs := Results("a1"); rs[0].Err == "" || rs[0].Out.Badge == nil || rs[0].OK() {
		t.Fatalf("failed run: %+v", rs[0])
	}

	// "every" in the output overrides the manifest's
	never := Result{Plugin: p, Fetched: time.Now().Add(-time.Hour), Out: Output{Every: "never"}}
	if due(never, time.Now()) {
		t.Fatal("never is never")
	}
}
