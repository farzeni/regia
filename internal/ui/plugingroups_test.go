package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/plugins"
)

// scriptPlugin is a plugin whose check.sh is script.
func scriptPlugin(t *testing.T, name, manifest, script string) *plugins.Manifest {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(dir, "check.sh"), []byte("#!/bin/sh\n"+script), 0o755)
	p, err := plugins.LoadOne(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// drain runs cmd and feeds what it returns back, a few rounds deep, the way
// Bubble Tea would; ticks and fetches are left out.
func drain(m *model, cmd tea.Cmd, depth int) {
	if cmd == nil || depth == 0 {
		return
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			drain(m, c, depth)
		}
		return
	}
	switch msg.(type) {
	case nil, pendingTickMsg, agentsMsg, pulseMsg, tickMsg:
		return
	}
	_, next := m.Update(msg)
	drain(m, next, depth-1)
}

// TestGroupPlugin: a plugin for groups reads the group (path, directory,
// coordinator, agents), and shows its badge on the group's row, its fields in
// the group's panel and its actions under !.
func TestGroupPlugin(t *testing.T) {
	m := teamModel(t)
	p := &plugins.Manifest{Name: "epic", Command: []string{"true"}, Targets: []string{"group"}}
	plugins.SetForTest([]*plugins.Manifest{p})
	defer plugins.SetForTest(nil)

	g := m.groupInfo("g:t")
	if g.Name != "team" || strings.Join(g.Path, "/") != "team" || g.Coordinator == nil || g.Coordinator.ID != "aaaa1111" || len(g.Agents) != 2 || g.Dir == "" {
		t.Fatalf("the group's input: %+v", g)
	}
	if fs := m.groupRefreshers("g:t"); len(fs) != 1 {
		t.Fatalf("a group plugin runs for the group: %d", len(fs))
	}
	if fs := m.pluginRefreshers(m.agents["aaaa1111"]); len(fs) != 0 {
		t.Fatal("…and not for its agents")
	}

	plugins.Seed(p, "g:t", plugins.Output{Badge: &plugins.Badge{Text: "E1"}, Fields: []plugins.Field{{Label: "journal", Text: "3 open questions"}},
		Actions: []plugins.Action{{ID: "fresh", Label: "Resume from journal"}}})
	m.cursorID = "g:t"
	m.rebuild()
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "E1") || !strings.Contains(v, "3 open questions") || !strings.Contains(v, "! actions") {
		t.Fatalf("badge on the row, fields in the panel:\n%s", v)
	}
	send(m, tkey("!"))
	if m.mode != modeMenu || !strings.Contains(m.menu[0].label, "Resume from journal") {
		t.Fatalf("! lists the group's actions: %v %+v", m.mode, m.menu)
	}
}

// stubStarts records what plugins start, and answers with id.
func stubStarts(t *testing.T, id string) *[]string {
	var got []string
	o1, o2 := spawnAgent, spawnAgentModel
	t.Cleanup(func() { spawnAgent, spawnAgentModel = o1, o2 })
	spawnAgent = func(dir, task string) (string, error) { got = append(got, task); return "started " + id + "\n", nil }
	spawnAgentModel = func(dir, model, task string) (string, error) {
		got = append(got, model+":"+task)
		return "started " + id + "\n", nil
	}
	return &got
}

// listed adds agent id to the listing, as claude agents would once it starts.
func listed(m *model, id string) tea.Cmd {
	dir := m.agents["aaaa1111"].Cwd
	m.setAgents([]byte(`[
	 {"id":"aaaa1111","cwd":"` + dir + `","kind":"background","name":"alpha","status":"busy","state":"working","startedAt":1794645285576},
	 {"id":"bbbb2222","cwd":"` + dir + `","kind":"background","name":"beta","status":"idle","state":"done","startedAt":1794645285577},
	 {"id":"` + id + `","cwd":"` + dir + `","kind":"background","name":"fresh","status":"busy","state":"working","startedAt":1794645285578}]`))
	return m.pendingListed()
}

// TestActionStartsCoordinator: an action on a group answering start as
// coordinator starts the agent in the group, and once it's listed the
// group's agents move under it.
func TestActionStartsCoordinator(t *testing.T) {
	m := teamModel(t)
	got := stubStarts(t, "cccc3333")
	_, cmd := m.Update(pluginActionMsg{plugin: "epic", label: "resume", agent: "g:t",
		out: plugins.Output{Start: &plugins.Start{Prompt: "resume E1 from journal.md", As: plugins.AsCoordinator, Model: "opus"}}})
	drain(m, cmd, 3)
	if len(*got) != 1 || (*got)[0] != "opus:resume E1 from journal.md" || m.pending == nil || m.layout.Parent["cccc3333"] != "g:t" {
		t.Fatalf("started in the group, waiting: %v %+v %q", *got, m.pending, m.layout.Parent["cccc3333"])
	}
	listed(m, "cccc3333")
	if m.pending != nil || m.layout.Parent["aaaa1111"] != "cccc3333" {
		t.Fatalf("the group's agents moved under it: %q (%s)", m.layout.Parent["aaaa1111"], m.status)
	}
}

// TestAttachGateRunsAFreshSwap: enter asks the attach gate, which runs the
// plugin's action; it starts a fresh session as a swap, and once listed regia
// swaps it in and attaches to it instead of the old one.
func TestAttachGateRunsAFreshSwap(t *testing.T) {
	m := teamModel(t)
	stubStarts(t, "cccc3333")
	p := scriptPlugin(t, "handoff", `{"command":["./check.sh"],"gates":["attach"]}`,
		`grep -q '"action":"fresh"' && echo '{"start":{"prompt":"go on from handoff.md","as":"swap","note":"handoff.md"}}'`)
	plugins.SetForTest([]*plugins.Manifest{p})
	defer plugins.SetForTest(nil)

	var asked []string
	og := attachGate
	t.Cleanup(func() { attachGate = og })
	attachGate = func(in plugins.Input, dir string) plugins.AttachAnswer {
		asked = append(asked, in.Agent.ID)
		return plugins.AttachAnswer{Run: "fresh", Plugin: p}
	}
	var attached []string
	oa := attachSession
	t.Cleanup(func() { attachSession = oa })
	attachSession = func(id string) tea.Cmd { attached = append(attached, id); return nil }
	var swapped []string
	os := swapSessions
	t.Cleanup(func() { swapSessions = os })
	swapSessions = func(who, a, b, reason, note string) (string, error) {
		swapped = append(swapped, a+">"+b+" "+note+" by "+who)
		return "swapped", nil
	}

	m.cursorID = "bbbb2222"
	m.rebuild()
	_, cmd := m.Update(tkey("enter"))
	drain(m, cmd, 4)
	if len(asked) != 1 || asked[0] != "bbbb2222" || len(attached) != 0 || m.pending == nil || !m.pending.attach {
		t.Fatalf("the gate ran the action, which started a session; not attached yet: %v %v %+v (%s)", asked, attached, m.pending, m.status)
	}
	drain(m, listed(m, "cccc3333"), 2)
	if len(swapped) != 1 || swapped[0] != "bbbb2222>cccc3333 handoff.md by you, through handoff" || len(attached) != 1 || attached[0] != "cccc3333" {
		t.Fatalf("swapped in and attached to the fresh one: %v %v", swapped, attached)
	}
}

// TestAttachGateRefusesOrLetsThrough: a refusal keeps you out; no answer (or a
// failed gate) attaches.
func TestAttachGateRefusesOrLetsThrough(t *testing.T) {
	m := teamModel(t)
	p := &plugins.Manifest{Name: "g", Command: []string{"true"}, Gates: []string{"attach"}}
	plugins.SetForTest([]*plugins.Manifest{p})
	defer plugins.SetForTest(nil)
	ans := plugins.AttachAnswer{Refuse: "g: on call elsewhere"}
	og := attachGate
	t.Cleanup(func() { attachGate = og })
	attachGate = func(plugins.Input, string) plugins.AttachAnswer { return ans }
	var attached []string
	oa := attachSession
	t.Cleanup(func() { attachSession = oa })
	attachSession = func(id string) tea.Cmd { attached = append(attached, id); return nil }

	m.cursorID = "bbbb2222"
	m.rebuild()
	_, cmd := m.Update(tkey("enter"))
	drain(m, cmd, 2)
	if len(attached) != 0 || !strings.Contains(m.status, "on call elsewhere") {
		t.Fatalf("refused: %v %q", attached, m.status)
	}
	ans = plugins.AttachAnswer{Warn: "g couldn't check it (timeout)"}
	_, cmd = m.Update(tkey("enter"))
	drain(m, cmd, 2)
	if len(attached) != 1 || !strings.Contains(m.status, "attaching anyway") {
		t.Fatalf("a failed gate lets you through: %v %q", attached, m.status)
	}
}

// TestRefreshStart: a refresh's start (with its key) is started by regia.
func TestRefreshStart(t *testing.T) {
	m := teamModel(t)
	got := stubStarts(t, "cccc3333")
	p := scriptPlugin(t, "auto", `{"command":["./check.sh"],"targets":["group"]}`,
		`echo '{"start":{"prompt":"morning standup","key":"standup-2026-11-24"}}'`)
	plugins.SetForTest([]*plugins.Manifest{p})
	defer plugins.SetForTest(nil)
	for _, f := range m.groupRefreshers("g:t") {
		f()
	}
	_, cmd := m.Update(pluginMsg{})
	drain(m, cmd, 2)
	if len(*got) != 1 || (*got)[0] != "morning standup" || m.layout.Parent["cccc3333"] != "g:t" {
		t.Fatalf("started once in the group: %v %q", *got, m.layout.Parent["cccc3333"])
	}
}
