package plugins

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTargets(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadOne(writePlugin(t, root, "bad", `{"command":["./check.sh"],"targets":["row"]}`, "")); err == nil {
		t.Fatal("unknown target refused")
	}
	if _, err := LoadOne(writePlugin(t, root, "badgate", `{"command":["./check.sh"],"gates":["open"]}`, "")); err == nil {
		t.Fatal("unknown gate refused")
	}
	p, err := LoadOne(writePlugin(t, root, "g", `{"command":["./check.sh"],"targets":["group"],"gates":["attach"],"dirs":["/work"]}`, ""))
	if err != nil {
		t.Fatal(err)
	}
	if p.ForAgents() || p.Applies(AgentInfo{Cwd: "/work/x"}) || !p.AppliesGroup("/work/x") || p.AppliesGroup("/else") {
		t.Fatal("groups only, under its dirs")
	}
	both := &Manifest{Targets: []string{"agent", "group"}}
	plain := &Manifest{}
	if !both.ForAgents() || !both.AppliesGroup("/a") || !plain.ForAgents() || plain.AppliesGroup("/a") {
		t.Fatal("agents by default; both when both")
	}
}

func TestGroupInputLeavesAgentOut(t *testing.T) {
	b, _ := json.Marshal(Input{Version: 1, Event: "refresh", Group: &GroupInfo{ID: "g:1", Name: "E1", Agents: []AgentInfo{{ID: "a"}}}})
	if strings.Contains(string(b), `"agent":`) || !strings.Contains(string(b), `"group":{"id":"g:1"`) {
		t.Fatalf("group input: %s", b)
	}
	b, _ = json.Marshal(Input{Agent: AgentInfo{ID: "a"}})
	if !strings.Contains(string(b), `"agent":{"id":"a"`) || strings.Contains(string(b), `"group":{`) {
		t.Fatalf("agent input: %s", b)
	}
}

func TestParseStart(t *testing.T) {
	o, err := Parse([]byte(`{"start":{"prompt":"go on from journal.md","as":"coordinator","key":"E1-1"}}`))
	if err != nil || o.Start == nil || o.Start.As != AsCoordinator || o.Start.Key != "E1-1" {
		t.Fatalf("%+v %v", o.Start, err)
	}
	if o, _ := Parse([]byte(`{"start":{"prompt":"x"}}`)); o.Start.As != AsAgent {
		t.Fatal("as defaults to agent")
	}
	if o, err := Parse([]byte(`{"start":{"prompt":" "}}`)); o.Start != nil || err == nil {
		t.Fatal("no prompt: dropped with a warning")
	}
	if o, err := Parse([]byte(`{"start":{"prompt":"x","as":"boss"}}`)); o.Start != nil || err == nil {
		t.Fatal("unknown as: dropped with a warning")
	}
}

func TestAttachGate(t *testing.T) {
	root := t.TempDir()
	load := func(name, script string) *Manifest {
		p, err := LoadOne(writePlugin(t, root, name, `{"command":["./check.sh"],"gates":["attach"]}`, script))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	in := Input{Agent: AgentInfo{ID: "a", Cwd: root}}

	SetForTest([]*Manifest{load("broken", "exit 2"), load("fresh", `grep -q '"event":"attach"' && echo '{"run":"fresh"}'`)})
	if !AttachGating(root) {
		t.Fatal("gating")
	}
	ans := Attach(in, root)
	if ans.Run != "fresh" || ans.Plugin.Name != "fresh" || !strings.Contains(ans.Warn, "broken couldn't check it") {
		t.Fatalf("a broken gate is passed with a warning, the next one runs: %+v", ans)
	}

	SetForTest([]*Manifest{load("no", `echo '{"refuse":"busy elsewhere"}'`)})
	if ans := Attach(in, root); ans.Refuse != "no: busy elsewhere" {
		t.Fatalf("refused: %+v", ans)
	}

	old := attachWait
	attachWait = 200 * time.Millisecond
	defer func() { attachWait = old }()
	SetForTest([]*Manifest{load("slow", `sleep 5; echo '{"refuse":"late"}'`)})
	t0 := time.Now()
	if ans := Attach(in, root); ans.Refuse != "" || ans.Warn == "" || time.Since(t0) > 3*time.Second {
		t.Fatalf("a slow gate lets you through, quickly: %+v after %s", ans, time.Since(t0))
	}
	SetForTest(nil)
	if AttachGating(root) {
		t.Fatal("no gates: nothing to ask")
	}
}

func TestRefreshStartOncePerKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	p, err := LoadOne(writePlugin(t, root, "resume", `{"command":["./check.sh"],"targets":["group"]}`,
		`echo '{"start":{"prompt":"resume E1","as":"coordinator","key":"E1-day1"}}'`))
	if err != nil {
		t.Fatal(err)
	}
	nokey, _ := LoadOne(writePlugin(t, root, "nokey", `{"command":["./check.sh"]}`, `echo '{"start":{"prompt":"x"}}'`))
	SetForTest([]*Manifest{p, nokey})
	build := func(*Output) (Input, string) { return Input{}, root }
	for i := 0; i < 3; i++ {
		f := Refresher(p, "g:1", build)
		if f == nil {
			t.Fatal("due")
		}
		f()
		reg.Lock()
		delete(reg.cache, key(p.Name, "g:1")) // due again
		reg.Unlock()
	}
	got := TakeStarts()
	if len(got) != 1 || got[0].Target != "g:1" || got[0].Start.Prompt != "resume E1" {
		t.Fatalf("started once for its key: %+v", got)
	}
	if !claimStart("other", "E1-day1", time.Now()) {
		t.Fatal("keys are per plugin")
	}

	Refresher(nokey, "a", build)()
	if len(TakeStarts()) != 0 || !strings.Contains(Results("a")[0].Err, "needs a \"key\"") {
		t.Fatalf("a refresh start without key is ignored, with a warning: %+v", Results("a"))
	}
}
