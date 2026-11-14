package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
)

// Completion in the draft: a word that starts with "/" lists skills and
// commands, one that starts with "@" lists files of the directory the agent
// will start in. The word can be anywhere in the text, not just at the start:
// Claude Code runs a skill named in the middle of a prompt too.

type compItem struct {
	name, desc string
	dir        bool // a directory: accepting it keeps the popup open
}

type completion struct {
	kind  rune   // '/' or '@'
	query string // the word typed after the trigger
	items []compItem
	sel   int
}

const compMaxRows = 8

// compWord returns the word the cursor is at the end of, when it starts with
// "/" or "@" (at the start of the text or after a space).
func (m *model) compWord() (kind rune, query string, ok bool) {
	lines := strings.Split(m.draft(), "\n")
	row := m.ta.Line()
	if row >= len(lines) {
		return 0, "", false
	}
	line := []rune(lines[row])
	li := m.ta.LineInfo()
	col := min(len(line), li.StartColumn+li.ColumnOffset)
	if col < len(line) && !unicode.IsSpace(line[col]) {
		return 0, "", false // in the middle of a word
	}
	start := col
	for start > 0 && !unicode.IsSpace(line[start-1]) {
		start--
	}
	if start == col {
		return 0, "", false
	}
	w := string(line[start:col])
	switch w[0] {
	case '/':
		if strings.ContainsRune(w[1:], '/') { // a path, not a skill
			return 0, "", false
		}
		return '/', w[1:], true
	case '@':
		return '@', w[1:], true
	}
	return 0, "", false
}

// updateCompletion opens, refreshes or closes the popup after the draft changed.
func (m *model) updateCompletion() {
	kind, q, ok := m.compWord()
	if !ok {
		m.comp, m.compShut = nil, ""
		return
	}
	key := string(kind) + q
	if m.compShut != "" && strings.HasPrefix(key, m.compShut) {
		return // esc closed it for this word
	}
	m.compShut = ""
	_, _, cwd, _ := m.spawnTarget()
	var items []compItem
	if kind == '/' {
		items = rankSkills(skillsFor(cwd), q)
	} else {
		items = rankFiles(filesFor(cwd), q)
	}
	if len(items) == 0 {
		m.comp = nil
		return
	}
	sel := 0
	if m.comp != nil && m.comp.kind == kind && m.comp.sel < len(items) && m.comp.query == q {
		sel = m.comp.sel
	}
	m.comp = &completion{kind: kind, query: q, items: items, sel: sel}
}

// accept replaces the word with the selected item.
func (m *model) acceptCompletion() {
	c := m.comp
	it := c.items[c.sel]
	for range []rune(c.query) {
		m.ta, _ = m.ta.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	ins := it.name
	if !it.dir {
		ins += " "
	}
	m.ta.InsertString(ins)
	m.comp = nil
	if it.dir {
		m.updateCompletion() // go on inside the directory
	}
}

// updateCompletionKeys handles the keys the popup takes while it is open.
func (m *model) updateCompletionKeys(k tea.KeyMsg) (handled bool) {
	c := m.comp
	if c == nil {
		return false
	}
	n := min(len(c.items), compMaxRows)
	switch k.String() {
	case "up", "ctrl+p":
		c.sel = (c.sel + n - 1) % n
	case "down", "ctrl+n":
		c.sel = (c.sel + 1) % n
	case "tab", "enter":
		m.acceptCompletion()
	case "esc":
		m.compShut = string(c.kind) + c.query
		m.comp = nil
	default:
		return false
	}
	return true
}

func (m *model) compRows() int {
	if m.comp == nil || !m.typing {
		return 0
	}
	return min(len(m.comp.items), compMaxRows)
}

// compView renders the popup: one item per line, the selected one lit.
func (m *model) compView() string {
	c := m.comp
	n := m.compRows()
	if n == 0 {
		return ""
	}
	nw := 0
	for _, it := range c.items[:n] {
		nw = max(nw, ansi.StringWidth(it.name)+1)
	}
	nw = min(nw, max(20, m.w/2))
	sel := lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	var out []string
	for i, it := range c.items[:n] {
		name := string(c.kind) + it.name
		l := "  " + pad(trunc(name, nw), nw) + "  "
		desc := trunc(it.desc, max(0, m.w-lipgloss.Width(l)-2))
		if i == c.sel {
			l = sel.Render("› "+pad(trunc(name, nw), nw)) + "  " + desc
		} else {
			l += stDim.Render(desc)
		}
		out = append(out, pad(l, m.w))
	}
	return strings.Join(out, "\n")
}

// --- skills -----------------------------------------------------------------

var builtinSkills = []compItem{
	{name: "code-review", desc: "built-in: review the current diff for bugs"},
	{name: "security-review", desc: "built-in: security review of the branch's changes"},
	{name: "simplify", desc: "built-in: clean up the changed code"},
	{name: "init", desc: "built-in: write a CLAUDE.md for the project"},
	{name: "run", desc: "built-in: launch the app to see a change working"},
	{name: "loop", desc: "built-in: run a prompt on an interval"},
	{name: "schedule", desc: "built-in: scheduled cloud agents"},
	{name: "fewer-permission-prompts", desc: "built-in: allowlist common read-only calls"},
	{name: "update-config", desc: "built-in: change settings.json"},
}

type cached[T any] struct {
	at  time.Time
	val T
}

var (
	cacheMu    sync.Mutex
	skillCache = map[string]cached[[]compItem]{}
	fileCache  = map[string]cached[[]compItem]{}
)

const cacheTTL = 20 * time.Second

func cachedFor(c map[string]cached[[]compItem], key string, load func() []compItem) []compItem {
	cacheMu.Lock()
	e, ok := c[key]
	cacheMu.Unlock()
	if ok && time.Since(e.at) < cacheTTL {
		return e.val
	}
	v := load()
	cacheMu.Lock()
	c[key] = cached[[]compItem]{time.Now(), v}
	cacheMu.Unlock()
	return v
}

// skillsFor lists what "/" can start in cwd: the user's skills and commands,
// the installed plugins' (as plugin:name), the project's (cwd up to home) and
// the built-ins.
var skillsFor = func(cwd string) []compItem {
	return cachedFor(skillCache, cwd, func() []compItem { return loadSkills(cwd) })
}

func loadSkills(cwd string) []compItem {
	seen := map[string]bool{}
	var out []compItem
	add := func(items []compItem) {
		for _, it := range items {
			if !seen[it.name] {
				seen[it.name] = true
				out = append(out, it)
			}
		}
	}
	// project first: it wins over the user's on a name clash
	home := claude.Home()
	for d := cwd; ; d = filepath.Dir(d) {
		add(skillsIn(filepath.Join(d, ".claude"), ""))
		if d == home || d == filepath.Dir(d) || !strings.HasPrefix(d, home) {
			break
		}
	}
	add(skillsIn(claude.Dir(), ""))
	for name, path := range installedPlugins() {
		add(skillsIn(path, name+":"))
	}
	add(builtinSkills)
	return out
}

// skillsIn reads dir/skills/*/SKILL.md and dir/commands/*.md.
func skillsIn(dir, prefix string) []compItem {
	var out []compItem
	ents, _ := os.ReadDir(filepath.Join(dir, "skills"))
	for _, e := range ents {
		p := filepath.Join(dir, "skills", e.Name(), "SKILL.md")
		if name, desc, ok := frontMatter(p); ok {
			if name == "" {
				name = e.Name()
			}
			out = append(out, compItem{name: prefix + name, desc: desc})
		}
	}
	filepath.WalkDir(filepath.Join(dir, "commands"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(filepath.Join(dir, "commands"), strings.TrimSuffix(p, ".md"))
		_, desc, ok := frontMatter(p)
		if ok {
			out = append(out, compItem{name: prefix + strings.ReplaceAll(rel, string(filepath.Separator), ":"), desc: desc})
		}
		return nil
	})
	return out
}

// frontMatter reads name and description from a markdown file's front matter.
// ok is false for a missing file or one hidden from the user.
func frontMatter(p string) (name, desc string, ok bool) {
	f, err := os.Open(p)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", "", true
	}
	block := false // inside a "description: >" block: indented lines follow
	for sc.Scan() {
		l := sc.Text()
		if strings.TrimSpace(l) == "---" {
			break
		}
		if block {
			if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
				desc = strings.TrimSpace(desc + " " + strings.TrimSpace(l))
				continue
			}
			block = false
		}
		k, v, found := strings.Cut(l, ":")
		if !found || strings.HasPrefix(k, " ") {
			continue
		}
		if strings.TrimSpace(k) == "description" && strings.ContainsAny(strings.TrimSpace(v), ">|") && len(strings.TrimSpace(v)) <= 2 {
			block = true
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "name":
			name = v
		case "description":
			desc = v
		case "user-invocable":
			if v == "false" {
				return "", "", false
			}
		}
	}
	return name, desc, true
}

// installedPlugins maps plugin name to its install path.
func installedPlugins() map[string]string {
	b, err := os.ReadFile(filepath.Join(claude.Dir(), "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var f struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	out := map[string]string{}
	for id, vs := range f.Plugins {
		if len(vs) > 0 {
			name, _, _ := strings.Cut(id, "@")
			out[name] = vs[len(vs)-1].InstallPath
		}
	}
	return out
}

// --- files ------------------------------------------------------------------

const maxFiles = 20000

// filesFor lists the files and directories of cwd, relative to it: the ones
// git knows (tracked and untracked, not ignored), or a capped walk outside a
// repository.
var filesFor = func(cwd string) []compItem {
	return cachedFor(fileCache, cwd, func() []compItem { return loadFiles(cwd) })
}

func loadFiles(cwd string) []compItem {
	var paths []string
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = cwd
	if out, err := cmd.Output(); err == nil && cwd != claude.Home() {
		paths = strings.Split(strings.TrimSpace(string(out)), "\n")
	} else {
		paths = walkFiles(cwd)
	}
	if len(paths) > maxFiles {
		paths = paths[:maxFiles]
	}
	dirs := map[string]bool{}
	var out []compItem
	for _, p := range paths {
		if p == "" {
			continue
		}
		for d := filepath.Dir(p); d != "." && !dirs[d]; d = filepath.Dir(d) {
			dirs[d] = true
			out = append(out, compItem{name: d + "/", dir: true})
		}
		out = append(out, compItem{name: p})
	}
	return out
}

func walkFiles(root string) []string {
	var out []string
	skip := map[string]bool{"node_modules": true, "vendor": true, "__pycache__": true, "target": true, "dist": true, "build": true}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if len(out) >= maxFiles {
			return filepath.SkipAll
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || skip[d.Name()] || strings.Count(rel, string(filepath.Separator)) >= 5 {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, rel)
		return nil
	})
	return out
}

// --- ranking ----------------------------------------------------------------

type scored struct {
	it    compItem
	score int
}

func rankBy(items []compItem, q string, score func(compItem, string) int) []compItem {
	q = strings.ToLower(q)
	var s []scored
	for _, it := range items {
		if sc := score(it, q); sc > 0 {
			s = append(s, scored{it, sc})
		}
	}
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].score != s[j].score {
			return s[i].score > s[j].score
		}
		return len(s[i].it.name) < len(s[j].it.name)
	})
	out := make([]compItem, 0, min(len(s), 50))
	for i := 0; i < len(s) && i < 50; i++ {
		out = append(out, s[i].it)
	}
	return out
}

func rankSkills(items []compItem, q string) []compItem {
	return rankBy(items, q, func(it compItem, q string) int {
		n := strings.ToLower(it.name)
		_, short, _ := strings.Cut(n, ":") // plugin:skill also matches on skill
		switch {
		case q == "":
			return 1
		case strings.HasPrefix(n, q) || strings.HasPrefix(short, q):
			return 4
		case strings.Contains(n, q):
			return 3
		case subseq(n, q):
			return 2
		case strings.Contains(strings.ToLower(it.desc), q):
			return 1
		}
		return 0
	})
}

func rankFiles(items []compItem, q string) []compItem {
	return rankBy(items, q, func(it compItem, q string) int {
		n := strings.ToLower(it.name)
		base := strings.ToLower(filepath.Base(strings.TrimSuffix(it.name, "/")))
		switch {
		case q == "":
			if strings.Contains(strings.TrimSuffix(n, "/"), "/") {
				return 0 // empty query: the top level only
			}
			return 1
		case strings.HasPrefix(n, q):
			return 5
		case strings.HasPrefix(base, q):
			return 4
		case strings.Contains(base, q):
			return 3
		case strings.Contains(n, q):
			return 2
		case subseq(n, q):
			return 1
		}
		return 0
	})
}

// subseq says whether q's letters appear in s in order.
func subseq(s, q string) bool {
	qr, i := []rune(q), 0
	for _, r := range s {
		if i < len(qr) && r == qr[i] {
			i++
		}
	}
	return i == len(qr)
}
