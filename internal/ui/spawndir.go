package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/layout"
)

// The directory a new agent starts in: layout.SpawnDir's rule, unless a
// directory was picked for this task (ctrl+o). The picker also sets a
// group's saved directory (w).

// dirWhy says in a few words where a directory came from.
func (m *model) dirWhy(src layout.DirSource) string {
	switch src.Kind {
	case "chosen":
		return "picked for this task"
	case "agent":
		return "dir of " + m.titleOf(src.From)
	case "group":
		return "group " + m.titleOf(src.From)
	case "agents":
		return fmt.Sprintf("guessed from %s in %s", count(src.N, "agent"), m.titleOf(src.From))
	case "folder":
		return "guessed from the name " + m.titleOf(src.From)
	}
	return "home: no group dir (w sets one)"
}

// nearestGroup is the group id is in (id itself when it is one), or "".
func (m *model) nearestGroup(id string) string {
	for n, i := id, 0; n != "" && i < 64; n, i = m.layout.EffParent(n, m.agents), i+1 {
		if m.layout.IsGroup(n) {
			return n
		}
	}
	return ""
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// expandDir turns what was typed (~/x, /x, x) into an absolute path; a
// relative path is taken from home.
func expandDir(s string) string {
	h := claude.Home()
	switch {
	case s == "~":
		return h
	case strings.HasPrefix(s, "~/"):
		return filepath.Join(h, s[2:])
	case filepath.IsAbs(s):
		return filepath.Clean(s)
	}
	return filepath.Join(h, s)
}

// ---------------------------------------------------------------- the picker

type dirItem struct {
	path, desc string
	clear      bool // "forget the group's saved directory"
}

type dirPicker struct {
	group string // set this group's saved directory; "" = pick for the draft
	query string
	items []dirItem
	sel   int
}

// openDirPick opens the picker: for the draft (group == "") or to set a
// group's saved directory, starting from what is saved.
func (m *model) openDirPick(group string) {
	p := &dirPicker{group: group}
	if d := m.layout.Dirs[group]; group != "" && d != "" {
		p.query = tildify(d)
	}
	m.dirPick, m.mode = p, modeDir
	m.refreshDirPick()
}

// groupDirKey is w: set the saved directory of the selected group, or of
// the group the selected agent is in.
func (m *model) groupDirKey(r *layout.Row) {
	g := ""
	if r != nil {
		g = m.nearestGroup(r.ID)
	}
	if g == "" {
		m.flash("not in a group: w sets a group's directory (ctrl+o picks one for a task)")
		return
	}
	m.openDirPick(g)
}

func pathQuery(q string) bool {
	return strings.HasPrefix(q, "/") || strings.HasPrefix(q, "~") || strings.HasPrefix(q, ".")
}

func (m *model) refreshDirPick() {
	p := m.dirPick
	if pathQuery(p.query) {
		p.items = pathItems(p.query)
	} else {
		p.items = nil
		q := strings.ToLower(p.query)
		for _, it := range m.dirCandidates() {
			if it.clear && q != "" {
				continue
			}
			if it.clear || strings.Contains(strings.ToLower(tildify(it.path)), q) {
				p.items = append(p.items, it)
			}
		}
	}
	p.sel = min(p.sel, max(0, min(len(p.items), compMaxRows)-1))
}

// dirCandidates: where it would start now, saved group directories, the
// directories agents work in (most recent first), then project folders.
func (m *model) dirCandidates() []dirItem {
	var out []dirItem
	seen := map[string]bool{}
	add := func(it dirItem) {
		if it.clear || (it.path != "" && !seen[it.path]) {
			seen[it.path] = true
			out = append(out, it)
		}
	}
	p := m.dirPick
	if p.group == "" {
		_, _, cwd, _, why := m.spawnTarget()
		add(dirItem{path: cwd, desc: "now: " + why})
	} else if m.layout.Dirs[p.group] != "" {
		g, src := m.layout.Guess(p.group, m.agents)
		desc := "go back to guessing: home"
		if g != "" {
			desc = "go back to guessing: " + tildify(g) + " (" + strings.TrimPrefix(m.dirWhy(src), "guessed ") + ")"
		}
		add(dirItem{clear: true, desc: desc})
	} else if g, src := m.layout.Guess(p.group, m.agents); g != "" {
		add(dirItem{path: g, desc: m.dirWhy(src)})
	}
	gids := make([]string, 0, len(m.layout.Dirs))
	for g := range m.layout.Dirs {
		gids = append(gids, g)
	}
	sort.Slice(gids, func(i, j int) bool { return m.titleOf(gids[i]) < m.titleOf(gids[j]) })
	for _, g := range gids {
		if m.layout.IsGroup(g) {
			add(dirItem{path: m.layout.Dirs[g], desc: "group " + m.titleOf(g)})
		}
	}
	home := claude.Home()
	as := make([]*claude.Agent, 0, len(m.agents))
	for _, a := range m.agents {
		as = append(as, a)
	}
	sort.Slice(as, func(i, j int) bool { return as[i].UpdatedAt.After(as[j].UpdatedAt) })
	for _, a := range as {
		if d := layout.RepoDir(a.Cwd); d != "" && d != home && isDir(d) {
			add(dirItem{path: d, desc: "agent " + m.titleOf(a.ID)})
		}
	}
	for _, root := range []string{"Projects", "Git"} {
		es, _ := os.ReadDir(filepath.Join(home, root))
		for _, e := range es {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				add(dirItem{path: filepath.Join(home, root, e.Name()), desc: "~/" + root})
			}
		}
	}
	add(dirItem{path: home, desc: "home"})
	return out
}

// pathItems completes a typed path: the folder itself when it exists, then
// its subfolders matching what follows the last "/".
func pathItems(q string) []dirItem {
	full := expandDir(q)
	dir, base := filepath.Dir(full), filepath.Base(full)
	var out []dirItem
	if strings.HasSuffix(q, "/") || q == "~" {
		dir, base = full, ""
	}
	if isDir(full) {
		out = append(out, dirItem{path: full, desc: "this folder"})
	}
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		n := e.Name()
		if !e.IsDir() || (strings.HasPrefix(n, ".") && !strings.HasPrefix(base, ".")) {
			continue
		}
		if p := filepath.Join(dir, n); p != full && strings.HasPrefix(strings.ToLower(n), strings.ToLower(base)) {
			out = append(out, dirItem{path: p})
		}
	}
	return out
}

func (m *model) updateDirPick(k tea.KeyMsg) tea.Cmd {
	p := m.dirPick
	n := min(len(p.items), compMaxRows)
	switch k.String() {
	case "esc", "ctrl+c":
		m.closeDirPick()
		return nil
	case "up", "ctrl+p":
		if n > 0 {
			p.sel = (p.sel + n - 1) % n
		}
		return nil
	case "down", "ctrl+n":
		if n > 0 {
			p.sel = (p.sel + 1) % n
		}
		return nil
	case "tab": // into the selected folder, to go deeper
		if n > 0 && !p.items[p.sel].clear {
			p.query, p.sel = tildify(p.items[p.sel].path)+"/", 0
			m.refreshDirPick()
		}
		return nil
	case "enter", "ctrl+s":
		var it dirItem
		switch {
		case n > 0:
			it = p.items[p.sel]
		case isDir(expandDir(p.query)):
			it = dirItem{path: expandDir(p.query)}
		default:
			m.flash("no such directory: %s", p.query)
			return nil
		}
		m.pickDir(it, k.String() == "ctrl+s")
		return nil
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
		}
	case "ctrl+u":
		p.query = ""
	case "ctrl+w", "alt+backspace": // back one path segment
		q := strings.TrimSuffix(p.query, "/")
		if i := strings.LastIndex(q, "/"); i >= 0 {
			p.query = q[:i+1]
		} else {
			p.query = ""
		}
	case " ":
		p.query += " "
	default:
		if k.Type != tea.KeyRunes {
			return nil
		}
		p.query += string(k.Runes)
	}
	p.sel = 0
	m.refreshDirPick()
	return nil
}

func (m *model) closeDirPick() { m.dirPick, m.mode = nil, modeNormal }

// pickDir applies the choice: to the draft, to the group, or (ctrl+s from
// the draft) to both.
func (m *model) pickDir(it dirItem, alsoGroup bool) {
	p := m.dirPick
	m.closeDirPick()
	g := p.group
	if g == "" {
		m.draftDir = it.path
		if !alsoGroup {
			m.flash("this task starts in %s", tildify(it.path))
			return
		}
		if g = m.nearestGroup(m.cursorID); g == "" {
			m.flash("this task starts in %s (not in a group: nothing saved)", tildify(it.path))
			return
		}
	}
	m.syncLayout()
	if it.clear {
		delete(m.layout.Dirs, g)
		m.flash("group %s: no saved dir, guessing again", m.titleOf(g))
	} else {
		m.layout.Dirs[g] = it.path
		m.flash("group %s: new agents start in %s", m.titleOf(g), tildify(it.path))
	}
	m.saveLayout()
}

func (m *model) dirPickRows() int {
	if m.mode != modeDir || m.dirPick == nil {
		return 0
	}
	return max(1, min(len(m.dirPick.items), compMaxRows))
}

func (m *model) dirPickPrompt() string {
	p := m.dirPick
	label := "dir for this task: "
	keys := "  ↑↓ tab enter · ctrl+s also for the group · esc"
	if p.group != "" {
		label = "dir for group " + m.titleOf(p.group) + ": "
		keys = "  ↑↓ tab enter · esc"
	}
	return stAccent.Render(label) + p.query + "█" + stDim.Render(keys)
}

// dirPickView renders the picker's list where the completion popup goes.
func (m *model) dirPickView() string {
	p := m.dirPick
	n := min(len(p.items), compMaxRows)
	if n == 0 {
		return pad(stDim.Render("  no matching folder — type a path: ~/… or /…"), m.w)
	}
	nw := 0
	for _, it := range p.items[:n] {
		nw = max(nw, len([]rune(m.dirItemName(it)))+1)
	}
	nw = min(nw, max(20, m.w/2))
	sel := lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	var out []string
	for i, it := range p.items[:n] {
		name := pad(trunc(m.dirItemName(it), nw), nw)
		l := "  " + name + "  "
		desc := trunc(it.desc, max(0, m.w-lipgloss.Width(l)-2))
		if i == p.sel {
			l = sel.Render("› "+name) + "  " + desc
		} else {
			l += stDim.Render(desc)
		}
		out = append(out, pad(l, m.w))
	}
	return strings.Join(out, "\n")
}

func (m *model) dirItemName(it dirItem) string {
	if it.clear {
		return "(no saved dir)"
	}
	return tildify(it.path)
}

// groupDirLine is the group panel's line about where its new agents start.
func (m *model) groupDirLine(gid string) string {
	if d := m.layout.Dirs[gid]; d != "" {
		return stDim.Render("new agents start in ") + tildify(d) + stDim.Render(" · saved (w changes)")
	}
	if d, src := m.layout.Guess(gid, m.agents); d != "" {
		return stDim.Render("new agents start in ") + tildify(d) + stDim.Render(" · "+m.dirWhy(src)+" (w saves one)")
	}
	return stDim.Render("new agents start in ~ · no dir (w sets one)")
}
