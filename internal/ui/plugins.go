package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// pluginCards renders what the plugins have for a: one bordered card per
// plugin, titled with its name (colored by its worst field), "! actions" on
// the right when it offers some, its fields inside with their own label
// column, and why its last run failed at the bottom.
//
//	╭─ gh-pr ──────────────────────── ! actions ─╮
//	│ ci       ▰▰▰▰▰▰▰▰▰▰ 3/3 · 1 failed: flaky   │
//	│ reviews  no reviews yet                    │
//	╰────────────────────────────────────────────╯
func pluginCards(a *claude.Agent, w int) []string {
	var out []string
	for _, r := range plugins.Results(a.ID) {
		if len(r.Out.Fields) == 0 && r.Err == "" {
			continue // nothing to say for this agent: no empty box
		}
		out = append(out, "")
		out = append(out, pluginCard(r, w)...)
	}
	return out
}

func pluginCard(r plugins.Result, w int) []string {
	const maxLabel = 10
	w = max(24, w)
	inner := w - 4 // "│ " … " │"
	bst := lipgloss.NewStyle().Foreground(cDim)

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

	worst := ""
	rank := map[string]int{"error": 3, "warn": 2, "ok": 1}
	var body []string
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

	title := r.Plugin.Name
	tst := stBold
	if worst == "error" || worst == "warn" {
		tst = statusStyle(worst).Bold(true)
	}
	right, end := "", "╮" // "╭─ title ───╮" or "╭─ title ─── ! actions ─╮"
	if len(r.Out.Actions) > 0 {
		right, end = " ! actions ", "─╮"
	}
	title = trunc(title, max(4, w-6-lipgloss.Width(right)))
	fill := max(1, w-4-lipgloss.Width(title)-lipgloss.Width(right)-lipgloss.Width(end))
	top := bst.Render("╭─ ") + tst.Render(title) + bst.Render(" "+strings.Repeat("─", fill)) +
		stAccent.Render(right) + bst.Render(end)
	lines := []string{top}
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
			return r.Out.Attention, r.Plugin.LabelOr()
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
			add(p.LabelOr()+" · "+act.Label, func(m *model, key string) tea.Cmd { return m.runPluginAction(a, p, act, key) })
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
			add("open "+label+"  "+u, func(m *model, _ string) tea.Cmd { return openURLCmd(u) })
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
		m.flash("%s: %s…", p.LabelOr(), act.Label)
		return func() tea.Msg {
			out, err := plugins.RunAction(p, id, act.ID, in, dir)
			return pluginActionMsg{plugin: p.LabelOr(), label: act.Label, out: out, err: err}
		}
	}
	if act.Confirm {
		m.confirm(key, fmt.Sprintf("%s: %s for %q?", p.LabelOr(), act.Label, m.titleOf(id)), run)
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
