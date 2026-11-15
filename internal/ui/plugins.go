package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/gitinfo"
	"github.com/farzeni/regia/internal/layout"
	"github.com/farzeni/regia/internal/plugins"
)

// Plugins (internal/plugins, docs/plugins.md): your own scripts run per agent
// in the background. What they print shows up as fields on the card, a badge
// on the row, a reason to list the agent in the inbox ("attention"), and
// actions in the ! menu, next to links to open.

type pluginMsg struct{}

type pluginActionMsg struct {
	plugin, label string
	out           plugins.Output
	err           error
}

// pluginRunsPerTick caps how many plugin runs one tick may start.
const pluginRunsPerTick = 4

// pluginInput builds what a plugin reads on stdin about a, and where it runs.
func (m *model) pluginInput(a *claude.Agent, p *plugins.Manifest) func(last *plugins.Output) (plugins.Input, string) {
	return func(last *plugins.Output) (plugins.Input, string) {
		in := plugins.Input{
			Version: plugins.ProtocolVersion,
			Plugin:  plugins.PluginInfo{Name: p.Name, Dir: p.Dir},
			Agent:   agentInfo(a),
			Last:    last,
		}
		in.Agent.Title = m.titleOf(a.ID)
		in.Agent.Group = m.layout.Trail(a.ID, m.agents)
		if gi, ok := gitinfo.Cached(a.Cwd); ok && gi.OK {
			in.Git = &plugins.GitInfo{Branch: gi.Branch, Upstream: gi.Upstream, Ahead: gi.Ahead, Behind: gi.Behind,
				Changed: gi.Changed + gi.Untracked, Head: gi.Head, Subject: gi.Subject, Worktree: gi.Worktree}
			if gi.Branch != "" {
				in.Agent.Branch = gi.Branch // what's checked out now beats what the job file said
			}
		}
		if pr := prFor(a); pr.OK {
			in.PR = &plugins.PRInfo{Number: pr.Number, URL: pr.URL, Title: pr.Title, State: pr.State, Checks: pr.Checks, Review: pr.Review}
		}
		return in, a.Cwd
	}
}

func agentInfo(a *claude.Agent) plugins.AgentInfo {
	ai := plugins.AgentInfo{
		ID: a.ID, SessionID: a.SessionID, Name: a.Name, Cwd: a.Cwd, Kind: a.Kind, State: a.State,
		Interactive: a.Interactive, Branch: a.Branch, Intent: a.Intent, Detail: a.Detail, Needs: a.Needs,
		Transcript: a.Transcript, StartedAt: a.StartedAt, UpdatedAt: a.UpdatedAt, Links: []plugins.Link{},
	}
	for _, l := range a.PRs {
		ai.Links = append(ai.Links, plugins.Link{Kind: l.Kind, ID: l.ID, URL: l.Href})
	}
	return ai
}

// pluginRefreshers: the runs due for a, one per plugin that applies to it.
func (m *model) pluginRefreshers(a *claude.Agent) []func() {
	if a == nil {
		return nil
	}
	var fs []func()
	ai := agentInfo(a)
	for _, p := range plugins.Plugins() {
		if !p.Applies(ai) {
			continue
		}
		if f := plugins.Refresher(p, a.ID, m.pluginInput(a, p)); f != nil {
			fs = append(fs, f)
		}
	}
	return fs
}

func asCmds(fs []func()) []tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(fs))
	for _, f := range fs {
		cmds = append(cmds, func() tea.Msg { f(); return pluginMsg{} })
	}
	return cmds
}

// pluginCmd refreshes a's plugins that are due (the selected agent, every frame).
func (m *model) pluginCmd(a *claude.Agent) tea.Cmd {
	return tea.Batch(asCmds(m.pluginRefreshers(a))...)
}

// pluginCmds refreshes the plugins of listed agents, a few per tick, so the
// inbox and the badges know without the agent being selected. Archived
// agents and ones finished before today are left to when you select them.
func (m *model) pluginCmds() tea.Cmd {
	if len(plugins.Plugins()) == 0 {
		return nil
	}
	var fs []func()
	now := time.Now()
	for _, a := range m.agents {
		if len(fs) >= pluginRunsPerTick {
			break
		}
		if m.layout.IsArchived(a.ID) || (faded(a, now) && !m.layout.Pins[a.ID]) {
			continue
		}
		for _, f := range m.pluginRefreshers(a) {
			if len(fs) < pluginRunsPerTick {
				fs = append(fs, f)
			}
		}
	}
	return tea.Batch(asCmds(fs)...)
}

func statusStyle(s string) lipgloss.Style {
	switch s {
	case "ok":
		return lipgloss.NewStyle().Foreground(cReady)
	case "warn":
		return lipgloss.NewStyle().Foreground(cBlocked)
	case "error":
		return lipgloss.NewStyle().Foreground(cFailed)
	case "dim":
		return stDim
	}
	return lipgloss.NewStyle()
}

// The agent's box on the panel is tabbed: "agent" (regia's own context:
// task, status, git, pr) first, then one tab per plugin that has something
// to show for the agent. , and . (or a click on a tab) switch; the tab you
// pick stays as you move between agents (back to "agent" where that plugin
// has nothing). Tabs not shown take their worst field's color and their
// badge, so a failing plugin is visible from any tab.
//
//	╭─ agent │ gh-pr ✗ │ jira ──────────── ! actions ─╮
//	│ ci       ▰▰▰▰▰▰▰▰▰▰ 3/3 · 1 failed: flaky         │
//	╰─────────────────────────────────────────────────╯

const agentTab = "agent"

type panelTab struct {
	name    string
	body    []string
	status  string // the worst field's: "", ok, warn, error
	badge   *plugins.Badge
	actions bool
}

// tabHit is where a tab's name sits on the box's top line, in columns from
// the panel's left edge (for clicks).
type tabHit struct {
	x0, x1 int
	name   string
}

// pluginTabs: one per plugin with something to show for a, in plugin order.
func pluginTabs(a *claude.Agent, w int) []panelTab {
	var tabs []panelTab
	for _, r := range plugins.Results(a.ID) {
		if len(r.Out.Fields) == 0 && r.Err == "" {
			continue // nothing to say for this agent: no tab
		}
		body, worst := pluginBody(r, w)
		tabs = append(tabs, panelTab{name: r.Plugin.Name, body: body, status: worst, badge: r.Out.Badge, actions: len(r.Out.Actions) > 0})
	}
	return tabs
}

// tabNames: the tabs a's box has, "agent" first.
func tabNames(a *claude.Agent) []string {
	names := []string{agentTab}
	for _, r := range plugins.Results(a.ID) {
		if len(r.Out.Fields) > 0 || r.Err != "" {
			names = append(names, r.Plugin.Name)
		}
	}
	return names
}

// cycleTab moves to the next (d=1) or previous (d=-1) tab of the selected agent's box.
func (m *model) cycleTab(d int) {
	r := m.selected()
	if r == nil || r.Agent == nil {
		return
	}
	names := tabNames(r.Agent)
	if len(names) < 2 {
		m.flash(", and . switch between plugin tabs: no plugin has anything for this agent")
		return
	}
	i := 0
	for k, n := range names {
		if n == m.panelTab {
			i = k
		}
	}
	m.panelTab = names[((i+d)%len(names)+len(names))%len(names)]
}

// tabbedBox draws tabs[0] ("agent") and the plugin tabs as one box showing
// the selected tab, as tall as the tallest so switching doesn't move what's
// below. It records where the tabs are for clicks.
func (m *model) tabbedBox(tabs []panelTab, w int) []string {
	sel := 0
	for i, t := range tabs {
		if t.name == m.panelTab {
			sel = i
		}
	}
	h := 0
	actions := false
	for _, t := range tabs {
		h = max(h, len(t.body))
		actions = actions || t.actions
	}
	var parts []string
	m.tabHits = m.tabHits[:0]
	x := 3 // after "╭─ "
	for i, t := range tabs {
		label := t.name
		if t.badge != nil {
			label += " " + t.badge.Text
		}
		alarm := t.status == "error" || t.status == "warn"
		st := stDim
		if alarm {
			st = statusStyle(t.status)
		}
		if i == sel {
			st = lipgloss.NewStyle().Bold(true).Underline(true)
			if alarm {
				st = statusStyle(t.status).Bold(true).Underline(true)
			}
		}
		parts = append(parts, st.Render(label))
		lw := lipgloss.Width(label)
		m.tabHits = append(m.tabHits, tabHit{x, x + lw, t.name})
		x += lw + 3 // " │ "
	}
	right := ""
	if actions {
		right = "! actions"
	}
	body := append([]string{}, tabs[sel].body...)
	if len(body) == 0 {
		body = append(body, stDim.Render("nothing yet"))
	}
	for len(body) < h {
		body = append(body, "")
	}
	return box(strings.Join(parts, stDim.Render(" │ ")), right, body, w)
}

// pluginBody is a plugin's fields as lines inside a box w wide (a label
// column sized to its own labels), and why its last run failed at the
// bottom; worst is its worst field's status.
func pluginBody(r plugins.Result, w int) (body []string, worst string) {
	const maxLabel = 10
	inner := boxInner(w)

	lw := 0
	labels := make([]string, len(r.Out.Fields))
	for i, f := range r.Out.Fields {
		l := f.Label
		if l == "" {
			l = r.Plugin.LabelOr()
		}
		if rs := []rune(l); len(rs) > maxLabel {
			l = string(rs[:maxLabel])
		}
		labels[i] = l
		lw = max(lw, lipgloss.Width(l))
	}
	if lw > 0 {
		lw += 2
	}

	rank := map[string]int{"error": 3, "warn": 2, "ok": 1}
	for i, f := range r.Out.Fields {
		if rank[f.Status] > rank[worst] {
			worst = f.Status
		}
		text := f.Text
		if f.URL != "" {
			text += "  " + stDim.Render(f.URL)
		}
		tw := max(10, inner-lw)
		for j, l := range clip(wrap(text, tw), 2, tw) {
			lab := strings.Repeat(" ", lw)
			if j == 0 {
				lab = stDim.Render(fmt.Sprintf("%-*s", lw, labels[i]))
			}
			body = append(body, lab+statusStyle(f.Status).Render(l))
		}
	}
	if r.Err != "" {
		for _, l := range clip(wrap("⚠ "+r.Err, inner), 2, inner) {
			body = append(body, stDim.Render(l))
		}
	}
	return body, worst
}

// boxInner is the room inside a box w wide: "│ " … " │".
func boxInner(w int) int { return max(24, w) - 4 }

// box draws body (lines at most boxInner(w) wide) in a rounded border w
// wide, with head (already styled: a title, or the tab bar) top-left and
// right (accent) top-right; either may be empty:
//
//	╭─ head ───────── right ─╮
//	│ body                   │
//	╰────────────────────────╯
func box(head string, right string, body []string, w int) []string {
	w = max(24, w)
	inner := boxInner(w)
	bst := lipgloss.NewStyle().Foreground(cDim)
	lead, tail := bst.Render("╭"), bst.Render("╮")
	if right != "" {
		tail = stAccent.Render(" "+right+" ") + bst.Render("─╮")
	}
	if head != "" {
		head = ansi.Truncate(head, max(4, w-6-lipgloss.Width(tail)), "…")
		lead = bst.Render("╭─ ") + head + bst.Render(" ")
	}
	fill := max(1, w-lipgloss.Width(lead)-lipgloss.Width(tail))
	lines := []string{lead + bst.Render(strings.Repeat("─", fill)) + tail}
	for _, l := range body {
		lines = append(lines, bst.Render("│ ")+pad(l, inner)+bst.Render(" │"))
	}
	return append(lines, bst.Render("╰"+strings.Repeat("─", w-2)+"╯"))
}

// pluginShowsPR: a plugin has a field linking to a's PR (prURL, or else the
// last PR link the agent recorded), so regia's own pr line would repeat it.
func pluginShowsPR(a *claude.Agent, prURL string) bool {
	if prURL == "" {
		for i := len(a.PRs) - 1; i >= 0 && prURL == ""; i-- {
			if a.PRs[i].Kind == "pr" {
				prURL = a.PRs[i].Href
			}
		}
	}
	if prURL == "" {
		return false
	}
	for _, r := range plugins.Results(a.ID) {
		for _, f := range r.Out.Fields {
			if strings.TrimSuffix(f.URL, "/") == strings.TrimSuffix(prURL, "/") {
				return true
			}
		}
	}
	return false
}

// pluginBadges is the badges a's row shows, rendered ("" when none).
func pluginBadges(id string) string {
	var bs []string
	for _, r := range plugins.Results(id) {
		if b := r.Out.Badge; b != nil {
			bs = append(bs, statusStyle(b.Status).Render(b.Text))
		}
	}
	return strings.Join(bs, " ")
}

// pluginAttention is the first reason a plugin gives to look at a ("" when none).
func pluginAttention(a *claude.Agent) (why, plugin string) {
	if a == nil {
		return "", ""
	}
	for _, r := range plugins.Results(a.ID) {
		if r.Out.Attention != "" {
			return r.Out.Attention, r.Plugin.Name
		}
	}
	return "", ""
}

// ---- the ! menu: plugin actions and links for the selected agent

type menuItem struct {
	key   string
	label string
	do    func(m *model) tea.Cmd
}

const menuKeys = "123456789abcdefghijklmnopqrstuvwxyz"

func (m *model) pluginMenuKey(r *layout.Row) tea.Cmd {
	if r == nil || r.Agent == nil {
		m.flash("! shows plugin actions for an agent")
		return nil
	}
	a := r.Agent
	var items []menuItem
	add := func(label string, do func(m *model, key string) tea.Cmd) {
		if len(items) < len(menuKeys) {
			key := string(menuKeys[len(items)])
			items = append(items, menuItem{key, label, func(m *model) tea.Cmd { return do(m, key) }})
		}
	}
	for _, res := range plugins.Results(a.ID) {
		p := res.Plugin
		for _, act := range res.Out.Actions {
			act := act
			add(p.Name+" · "+act.Label, func(m *model, key string) tea.Cmd { return m.runPluginAction(a, p, act, key) })
		}
	}
	for _, res := range plugins.Results(a.ID) {
		for _, f := range res.Out.Fields {
			if f.URL == "" {
				continue
			}
			label, u := f.Label, f.URL
			if label == "" {
				label = res.Plugin.LabelOr()
			}
			add("open "+res.Plugin.Name+" · "+label+"  "+u, func(m *model, _ string) tea.Cmd { return openURLCmd(u) })
		}
	}
	if len(items) == 0 {
		if len(plugins.Plugins()) == 0 {
			m.flash("no plugins: see docs/plugins.md (they go in %s)", tildify(plugins.Dir()))
		} else {
			m.flash("no plugin actions or links for %s", m.titleOf(a.ID))
		}
		return nil
	}
	m.menu, m.menuTitle, m.mode = items, "plugins · "+m.titleOf(a.ID), modeMenu
	return nil
}

func (m *model) updateMenu(k tea.KeyMsg) tea.Cmd {
	s := k.String()
	m.mode = modeNormal
	for _, it := range m.menu {
		if it.key == s {
			m.menu = nil
			return it.do(m)
		}
	}
	m.menu = nil
	if s != "esc" && s != "!" && s != "q" {
		m.flash("cancelled")
	}
	return nil
}

// runPluginAction runs act; one with confirm asks first (key, the menu key
// that picked it, pressed again confirms).
func (m *model) runPluginAction(a *claude.Agent, p *plugins.Manifest, act plugins.Action, key string) tea.Cmd {
	in, dir := m.pluginInput(a, p)(nil)
	id := a.ID
	run := func(m *model) tea.Cmd {
		m.flash("%s: %s…", p.Name, act.Label)
		return func() tea.Msg {
			out, err := plugins.RunAction(p, id, act.ID, in, dir)
			return pluginActionMsg{plugin: p.Name, label: act.Label, out: out, err: err}
		}
	}
	if act.Confirm {
		m.confirm(key, fmt.Sprintf("%s: %s for %q?", p.Name, act.Label, m.titleOf(id)), run)
		return nil
	}
	return run(m)
}

func (m *model) pluginActionDone(msg pluginActionMsg) tea.Cmd {
	if msg.err != nil {
		m.flash("%s: %s failed: %v", msg.plugin, msg.label, msg.err)
		return nil
	}
	note := msg.out.Message
	if note == "" {
		note = msg.label + " done"
	}
	if msg.out.Copy != "" {
		if err := copyClipboard(msg.out.Copy); err != nil {
			note += " · clipboard: " + err.Error()
		} else {
			note += " · copied"
		}
	}
	m.flash("%s: %s", msg.plugin, note)
	if msg.out.Open != "" {
		return openURLCmd(msg.out.Open)
	}
	return nil
}

func openURLCmd(u string) tea.Cmd {
	return func() tea.Msg {
		name := "xdg-open"
		if runtime.GOOS == "darwin" {
			name = "open"
		}
		c := exec.Command(name, u)
		if err := c.Start(); err == nil {
			go func() { _ = c.Wait() }()
		}
		return nil
	}
}

// menuModal renders the ! menu as a box over the screen.
func (m *model) menuModal() string {
	var ls []string
	ls = append(ls, stBold.Render(m.menuTitle)+stDim.Render("   esc closes"), "")
	for _, it := range m.menu {
		ls = append(ls, stAccent.Render("  "+it.key+"  ")+trunc(it.label, max(20, m.w-16)))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(0, 1).MaxWidth(m.w).Render(strings.Join(ls, "\n"))
}

// PluginInput is what plugin p would read on stdin for agent id (a short id,
// a session id or a prefix of either), where it would run, and whether its
// manifest applies to that agent. For `regia plugin input|run`.
func PluginInput(id string, p *plugins.Manifest) (in plugins.Input, dir string, applies bool, err error) {
	b, err := claude.FetchAgents()
	if err != nil {
		return in, "", false, fmt.Errorf("claude agents --json failed: %w", err)
	}
	m := newModel()
	m.setAgents(b)
	a, err := findAgent(m.agents, id)
	if err != nil {
		return in, "", false, err
	}
	if c := gitCmd(a.Cwd); c != nil {
		c()
	}
	if c := prCmd(a); c != nil {
		c()
	}
	in, dir = m.pluginInput(a, p)(nil)
	in.Event = "refresh"
	return in, dir, p.Applies(agentInfo(a)), nil
}

func findAgent(as map[string]*claude.Agent, id string) (*claude.Agent, error) {
	if a := as[id]; a != nil {
		return a, nil
	}
	var found []*claude.Agent
	for _, a := range as {
		if strings.HasPrefix(a.ID, id) || strings.HasPrefix(a.SessionID, id) {
			found = append(found, a)
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("no agent %q in `claude agents` (regia --dump lists ids)", id)
	case 1:
		return found[0], nil
	}
	return nil, fmt.Errorf("%q matches %d agents: give more of the id", id, len(found))
}
