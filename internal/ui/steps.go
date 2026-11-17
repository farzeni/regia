package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/spawns"
)

// How each kind of step reads in the conversation, desktop-app style: an
// edit as `✎ path:line +8 −3` unfolding into its diff, a command as `$ cmd`
// unfolding into its output, a read, a search, a screenshot, a spawned agent
// with its live state, a subagent, a fetch, a todo list. Enter does the
// useful thing: opens the file at the edited line, the whole output in the
// pager, the image, the URL, or jumps to the spawned agent.

var (
	stStderr = lipgloss.NewStyle().Foreground(lipgloss.Color("210")) // a command's stderr: tinted, not alarming
	stBusy   = lipgloss.NewStyle().Foreground(cBusy)
)

const (
	maxDiffLines = 20 // diff lines an unfolded edit shows
	maxHeadLines = 12 // lines of output, paths or a report an unfolded step shows
)

func init() {
	edit := stepKind{line: editLine, detail: editDetail, activate: editActivate}
	read := stepKind{line: readLine, detail: readDetail, activate: readActivate}
	search := stepKind{line: searchLine, detail: searchDetail, activate: searchActivate}
	sub := stepKind{line: subagentLine, detail: subagentDetail, activate: subagentActivate}
	web := stepKind{line: webLine, detail: webDetail, activate: webActivate}
	for name, k := range map[string]stepKind{
		"Edit": edit, "Write": edit, "MultiEdit": edit, "NotebookEdit": edit,
		"Bash":  {line: bashLine, detail: bashDetail, activate: bashActivate},
		"Read":  read,
		"Grep":  search,
		"Glob":  search,
		"Agent": sub, "Task": sub,
		"WebFetch": web, "WebSearch": web,
		"TodoWrite": {line: todoLine, detail: todoDetail},
	} {
		k.line, k.detail = fittedLine(k.line), fitted(k.detail)
		stepKinds[name] = k
	}
}

// imageKind is for any other tool whose result holds images (a browser's
// screenshot): kindOf picks it when the tool has no kind of its own.
var imageKind = stepKind{line: fittedLine(imageLine), detail: fitted(imageDetail), activate: imageActivate}

// fittedLine cuts a step's line to its width (a very narrow panel).
func fittedLine(f func(stepCtx, *claude.Step, int) string) func(stepCtx, *claude.Step, int) string {
	return func(c stepCtx, s *claude.Step, w int) string {
		l := f(c, s, w)
		if ansi.StringWidth(l) > w {
			return ansi.Truncate(l, max(0, w), "…")
		}
		return l
	}
}

// fitted cuts every line of a detail to its width: short notes ("lines
// 1-200 of 300") stay on one line in a narrow panel.
func fitted(d func(stepCtx, *claude.Step, int) []string) func(stepCtx, *claude.Step, int) []string {
	return func(c stepCtx, s *claude.Step, w int) []string {
		ls := d(c, s, w)
		for i, l := range ls {
			if ansi.StringWidth(l) > w {
				ls[i] = ansi.Truncate(l, max(0, w), "…")
			}
		}
		return ls
	}
}

// --- shared helpers ------------------------------------------------------------

// shortPath is p relative to the agent's directory when inside it, else with
// ~ for your home.
func shortPath(c stepCtx, p string) string {
	if c.a != nil && c.a.Cwd != "" {
		if r, err := filepath.Rel(c.a.Cwd, p); err == nil && r != "." && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" && strings.HasPrefix(p, h+"/") {
		return "~" + p[len(h):]
	}
	return p
}

// truncPath cuts a path from the left (the file name matters most).
func truncPath(p string, w int) string {
	if ansi.StringWidth(p) <= w {
		return p
	}
	if w <= 1 {
		return trunc(p, w)
	}
	return ansi.TruncateLeft(p, ansi.StringWidth(p)-w+1, "…")
}

// flat is s with its whitespace runs (newlines too) as single spaces.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

// lines of s without its trailing newlines; none for blank s.
func lines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// headLines draws the first n of ls w wide in st, then "… +N more".
func headLines(ls []string, n, w int, st lipgloss.Style) []string {
	var out []string
	for _, l := range ls[:min(len(ls), n)] {
		out = append(out, st.Render(trunc(untab(l), w)))
	}
	if len(ls) > n {
		out = append(out, stDim.Render(fmt.Sprintf("… +%d more", len(ls)-n)))
	}
	return out
}

// resInfo is a result's info field k ("" while the step runs).
func resInfo(s *claude.Step, k string) string {
	if s.Res == nil {
		return ""
	}
	return s.Res.Info[k]
}

// untab: tabs as two spaces, so widths hold.
func untab(s string) string { return strings.ReplaceAll(s, "\t", "  ") }

// failMark is a red " ✗" for a failed step, a green " …" for one running.
func failMark(s *claude.Step) string {
	switch {
	case s.Failed():
		return stErr.Render(" ✗")
	case s.Running():
		return stBusy.Render(" …")
	}
	return ""
}

// errorDetail is a failed step's error text.
func errorDetail(s *claude.Step, w int) []string {
	return headLines(lines(s.Out), maxHeadLines, w, stErr)
}

// openFile opens path at line, or says why not.
func openFile(m *model, path string, line int) tea.Cmd {
	if path == "" {
		m.flash("no file to open")
		return nil
	}
	return openThing(openTarget{Kind: "file", Path: path, Line: line})
}

// pageText shows text in the pager, or says there's nothing.
func pageText(m *model, text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		m.flash("nothing to show")
		return nil
	}
	return openThing(openTarget{Kind: "pager", Text: text})
}

// --- Edit / Write ----------------------------------------------------------------

// editPath is the file an edit changed.
func editPath(s *claude.Step) string {
	if s.Res != nil && s.Res.FilePath != "" {
		return s.Res.FilePath
	}
	if p := s.In["file_path"]; p != "" {
		return p
	}
	return s.In["notebook_path"]
}

// editAt is the first changed line of the new file (0 when unknown).
func editAt(s *claude.Step) int {
	if s.Res == nil || len(s.Res.Patch) == 0 {
		return 0
	}
	h := s.Res.Patch[0]
	n := h.NewStart
	for _, l := range h.Lines {
		if !strings.HasPrefix(l, " ") {
			return max(n, 1)
		}
		n++
	}
	return max(h.NewStart, 1)
}

// editLine is "✎ path:line +8 −3" (a new file: "+12 new").
func editLine(c stepCtx, s *claude.Step, w int) string {
	p, at := editPath(s), editAt(s)
	var tail string
	if s.Res != nil && !s.Failed() {
		tail = " " + stAdd.Render(fmt.Sprintf("+%d", s.Res.Added))
		if s.Res.Removed > 0 {
			tail += " " + stDel.Render(fmt.Sprintf("−%d", s.Res.Removed))
		}
		if s.Res.Info["type"] == "create" {
			tail += stDim.Render(" new")
		}
	}
	tail += failMark(s)
	where := shortPath(c, p)
	if at > 0 {
		where += ":" + strconv.Itoa(at)
	}
	where = truncPath(where, w-2-lipgloss.Width(tail))
	return stDim.Render("✎ ") + linkify(where, openTarget{Kind: "file", Path: p, Line: at}) + tail
}

// editDetail is the diff with line numbers, maxDiffLines at most.
func editDetail(c stepCtx, s *claude.Step, w int) []string {
	if s.Failed() {
		return errorDetail(s, w)
	}
	if s.Res == nil || len(s.Res.Patch) == 0 {
		return genericDetail(c, s, w)
	}
	last := 0
	for _, h := range s.Res.Patch {
		last = max(last, h.OldStart+h.OldLines, h.NewStart+h.NewLines)
	}
	nw := len(strconv.Itoa(last))
	var out []string
	kept, changed, shown := 0, 0, 0
	for i, h := range s.Res.Patch {
		if i > 0 && len(out) < maxDiffLines {
			out = append(out, stHunk.Render(strings.Repeat(" ", nw)+" ⋯"))
		}
		o, n := h.OldStart, h.NewStart
		for _, l := range h.Lines {
			kept++
			sign, text := " ", l
			if l != "" {
				sign, text = l[:1], l[1:]
			}
			num, st := n, stDim
			switch sign {
			case "-":
				num, st = o, stDel
				o++
				changed++
			case "+":
				st = stAdd
				n++
				changed++
			default:
				o++
				n++
			}
			if len(out) < maxDiffLines {
				shown++
				out = append(out, st.Render(trunc(fmt.Sprintf("%*d %s %s", nw, num, sign, untab(text)), w)))
			}
		}
	}
	// lines past the cap, and changed lines the parse didn't keep
	more := kept - shown + max(0, s.Res.Added+s.Res.Removed-changed)
	if more > 0 {
		out = append(out, stDim.Render(fmt.Sprintf("… +%d more", more)))
	}
	return out
}

func editActivate(m *model, _ *claude.Agent, s *claude.Step) tea.Cmd {
	return openFile(m, editPath(s), editAt(s))
}

// --- Bash (and the agents it spawns) ---------------------------------------------

var (
	reBgCmd    = regexp.MustCompile(`claude\b[^\n|;&]*\s--bg\b`)
	reBgBanner = regexp.MustCompile(`(?m)^\s*backgrounded · ([0-9a-f]{8})\b`)
)

// spawnedIDs are the agents a command started with `claude --bg` (or a
// wrapper of it, when regia recorded the link): ids in its output's banner.
func spawnedIDs(c stepCtx, s *claude.Step) []string {
	if s.Res == nil || s.Failed() {
		return nil
	}
	const banner = "backgrounded · "
	out := s.Res.Stdout // most commands: one cheap look, no regexp
	if !strings.Contains(out, banner) {
		if !strings.Contains(s.Out, banner) {
			return nil
		}
		out = s.Out
	}
	cmd := reBgCmd.MatchString(s.In["command"])
	var links map[string]spawns.Link
	var ids []string
	seen := map[string]bool{}
	for _, m := range reBgBanner.FindAllStringSubmatch(out, -1) {
		id := m[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		if !cmd {
			if links == nil {
				links = spawns.Cached()
			}
			if l, ok := links[id]; !ok || c.a == nil || l.Parent != c.a.ID {
				continue
			}
		}
		ids = append(ids, id)
	}
	return ids
}

// stateWords: an agent's state as a spawn line says it.
var stateWords = map[string]string{"busy": "working", "blocked": "needs you", "idle": "idle", "done": "done", "ready": "done", "failed": "failed"}

// spawnName is "fb-inbox (working)": the spawned agent's name and live state.
func spawnName(c stepCtx, id string) string {
	a := c.m.agents[id]
	if a == nil {
		return id + stDim.Render(" (gone)")
	}
	name := a.Name
	if name == "" {
		name = id
	}
	st := stateWords[a.State]
	if st == "" {
		st = a.State
	}
	return name + stDim.Render(" ("+st+")")
}

// bashLine is "$ cmd" (✗ when it failed), or "⇢ spawned fb-inbox (working)".
func bashLine(c stepCtx, s *claude.Step, w int) string {
	if ids := spawnedIDs(c, s); len(ids) > 0 {
		names := make([]string, len(ids))
		for i, id := range ids {
			names[i] = spawnName(c, id)
		}
		return stDim.Render("⇢ spawned ") + trunc(strings.Join(names, ", "), w-10)
	}
	tail := failMark(s)
	if s.Res != nil && s.Res.Interrupted {
		tail += stErr.Render(" ⏹")
	}
	return stDim.Render("$ ") + trunc(flat(s.In["command"]), w-2-lipgloss.Width(tail)) + tail
}

// bashDetail is the whole command, then the last lines of its output
// (stderr tinted) and whether it was interrupted.
func bashDetail(c stepCtx, s *claude.Step, w int) []string {
	var out []string
	for i, l := range lines(s.In["command"]) {
		p := "$ "
		if i > 0 {
			p = "  "
		}
		for _, wl := range wrap(untab(l), max(10, w-2)) {
			out = append(out, stDim.Render(p)+strings.TrimRight(wl, " "))
			p = "  "
		}
	}
	if ids := spawnedIDs(c, s); len(ids) > 0 {
		out = append(out, stDim.Render("enter: go to "+ids[0]))
	}
	switch {
	case s.Running():
		return append(out, stBusy.Render("running…"))
	case s.Res == nil:
		return out
	}
	r := s.Res
	so, se := lines(r.Stdout), lines(r.Stderr)
	if len(so)+len(se) == 0 { // no streams recorded (an error): the result text
		st := stDim
		if s.Failed() {
			st = stErr
		}
		out = append(out, headLines(lines(s.Out), 15, w, st)...)
	}
	for _, l := range so {
		out = append(out, trunc(untab(l), w))
	}
	for _, l := range se {
		out = append(out, stStderr.Render(trunc(untab(l), w)))
	}
	if r.Interrupted {
		out = append(out, stErr.Render("⏹ interrupted"))
	}
	if len(so) >= 15 || len(se) >= 15 || r.Info["persistedOutputPath"] != "" {
		out = append(out, stDim.Render("… enter: the whole output"))
	}
	return out
}

// bashActivate: a spawn jumps to the agent; else the whole output in the pager.
func bashActivate(m *model, a *claude.Agent, s *claude.Step) tea.Cmd {
	if ids := spawnedIDs(stepCtx{m, a}, s); len(ids) > 0 {
		m.jumpTo(ids[0])
		return nil
	}
	if s.Res == nil {
		m.flash("still running")
		return nil
	}
	var text string
	if p := s.Res.Info["persistedOutputPath"]; p != "" {
		if b, err := os.ReadFile(p); err == nil {
			text = string(b)
		}
	}
	if text == "" {
		t, err := claude.FullOutput(claude.TranscriptPath(a), s.Res)
		if err != nil {
			m.flash("output: %v", err)
			return nil
		}
		text = t
	}
	if strings.TrimSpace(text) == "" {
		m.flash("no output")
		return nil
	}
	return pageText(m, "$ "+s.In["command"]+"\n\n"+text+"\n")
}

// jumpTo moves the list's cursor to agent id (unfolding what hides it) and
// gives it the keys back: its panel shows.
func (m *model) jumpTo(id string) {
	if m.agents[id] == nil {
		m.flash("%s isn't running anymore", id)
		return
	}
	for p := m.layout.Parent[id]; p != ""; p = m.layout.Parent[p] {
		delete(m.layout.Collapsed, p)
	}
	if m.view != viewTree {
		m.setView(viewTree)
	}
	m.convFocus = false
	m.cursorID = id
	m.rebuild()
	m.saveLayoutQuiet()
	if m.cursorID != id {
		m.flash("%s is hidden in the list", id)
	}
}

// --- Read / Grep / Glob ------------------------------------------------------------

// readRange is the lines a Read covered: from, to (0, 0 when unknown).
func readRange(s *claude.Step) (int, int) {
	if s.Res != nil {
		from, _ := strconv.Atoi(s.Res.Info["file.startLine"])
		n, _ := strconv.Atoi(s.Res.Info["file.numLines"])
		if from > 0 && n > 0 {
			return from, from + n - 1
		}
	}
	from, _ := strconv.Atoi(s.In["offset"])
	n, _ := strconv.Atoi(s.In["limit"])
	if n > 0 {
		return max(from, 1), max(from, 1) + n - 1
	}
	return 0, 0
}

// readLine is "◦ read x.go 1-200" (an image: "▣ x.png 1280×720").
func readLine(c stepCtx, s *claude.Step, w int) string {
	if hasImages(s) {
		return imageLine(c, s, w)
	}
	p := s.In["file_path"]
	r := ""
	if from, to := readRange(s); from > 0 {
		r = fmt.Sprintf(" %d-%d", from, to)
	}
	tail := stDim.Render(r) + failMark(s)
	at, _ := readRange(s)
	where := truncPath(shortPath(c, p), w-7-lipgloss.Width(tail))
	return stDim.Render("◦ read ") + linkify(where, openTarget{Kind: "file", Path: p, Line: at}) + tail
}

func readDetail(c stepCtx, s *claude.Step, w int) []string {
	if s.Failed() {
		return errorDetail(s, w)
	}
	if hasImages(s) {
		return imageDetail(c, s, w)
	}
	p := s.In["file_path"]
	at, to := readRange(s)
	out := []string{linkify(truncPath(p, w), openTarget{Kind: "file", Path: p, Line: at})}
	if at > 0 {
		l := fmt.Sprintf("lines %d-%d", at, to)
		if t := resInfo(s, "file.totalLines"); t != "" {
			l += " of " + t
		}
		out = append(out, stDim.Render(l))
	}
	return out
}

func readActivate(m *model, a *claude.Agent, s *claude.Step) tea.Cmd {
	if hasImages(s) {
		return imageActivate(m, a, s)
	}
	at, _ := readRange(s)
	return openFile(m, s.In["file_path"], at)
}

// searchLine is `◦ grep "p" 12 hits` or `◦ glob "*.go" 8 files`.
func searchLine(_ stepCtx, s *claude.Step, w int) string {
	verb := strings.ToLower(s.Name)
	n, what := searchCount(s)
	tail := ""
	if s.Res != nil && !s.Failed() {
		tail = stDim.Render(" " + count(n, what))
	}
	tail += failMark(s)
	q := strconv.Quote(s.In["pattern"])
	return stDim.Render("◦ "+verb+" ") + trunc(q, w-3-len(verb)-lipgloss.Width(tail)) + tail
}

// searchCount is how much a search found: grep's lines (or files, as asked),
// glob's files; the result's lines when Claude Code didn't count.
func searchCount(s *claude.Step) (int, string) {
	if s.Res == nil {
		return 0, "hit"
	}
	info := func(k string) (int, bool) {
		n, err := strconv.Atoi(s.Res.Info[k])
		return n, err == nil
	}
	if s.Name == "Glob" {
		if n, ok := info("numFiles"); ok {
			return n, "file"
		}
		return len(searchPaths(s)), "file"
	}
	switch s.Res.Info["mode"] {
	case "content":
		if n, ok := info("numLines"); ok {
			return n, "hit"
		}
	case "count":
		if n, ok := info("numMatches"); ok {
			return n, "hit"
		}
	}
	if n, ok := info("numFiles"); ok {
		return n, "file"
	}
	return len(searchPaths(s)), "hit"
}

// searchPaths are the result's lines that aren't a note ("Found 3 files").
func searchPaths(s *claude.Step) []string {
	var out []string
	for _, l := range lines(s.Out) {
		if strings.HasPrefix(l, "Found ") || strings.HasPrefix(l, "No files found") || strings.HasPrefix(l, "No matches") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// searchDetail is what it found: paths (or matching lines), the first few.
func searchDetail(c stepCtx, s *claude.Step, w int) []string {
	if s.Failed() {
		return errorDetail(s, w)
	}
	ps := searchPaths(s)
	if len(ps) == 0 {
		return []string{stDim.Render("nothing found")}
	}
	out := []string{}
	for _, l := range ps[:min(len(ps), maxHeadLines)] {
		p, at := hitAt(c, s, l)
		out = append(out, linkify(trunc(untab(shortPath(c, p)+l[len(p):]), w), openTarget{Kind: "file", Path: absIn(c, s, p), Line: at}))
	}
	if len(ps) > maxHeadLines {
		out = append(out, stDim.Render(fmt.Sprintf("… +%d more", len(ps)-maxHeadLines)))
	}
	return out
}

// hitAt splits a search result line into its path and line ("a.go:12:x").
func hitAt(_ stepCtx, _ *claude.Step, l string) (string, int) {
	i := strings.IndexByte(l, ':')
	if i <= 0 {
		return l, 0
	}
	rest := l[i+1:]
	j := strings.IndexByte(rest, ':')
	if j < 0 {
		j = len(rest)
	}
	if n, err := strconv.Atoi(rest[:j]); err == nil {
		return l[:i], n
	}
	return l, 0
}

// absIn makes a found path absolute: against the search's path, else the
// agent's directory.
func absIn(c stepCtx, s *claude.Step, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if d := s.In["path"]; filepath.IsAbs(d) {
		return filepath.Join(d, p)
	}
	if c.a != nil {
		return filepath.Join(c.a.Cwd, p)
	}
	return p
}

// searchActivate opens the first thing found.
func searchActivate(m *model, a *claude.Agent, s *claude.Step) tea.Cmd {
	ps := searchPaths(s)
	if len(ps) == 0 {
		m.flash("nothing found to open")
		return nil
	}
	c := stepCtx{m, a}
	p, at := hitAt(c, s, ps[0])
	return openFile(m, absIn(c, s, p), at)
}

// --- images ---------------------------------------------------------------------------

func hasImages(s *claude.Step) bool { return s.Res != nil && len(s.Res.Images) > 0 }

// imageLine is "▣ screenshot 1280×720" (a Read: the file's name).
func imageLine(c stepCtx, s *claude.Step, w int) string {
	im := s.Res.Images[0]
	what := toolLabel(s.Name)
	switch {
	case s.Name == "Read":
		what = shortPath(c, s.In["file_path"])
	case strings.Contains(strings.ToLower(s.Name), "screenshot"):
		what = "screenshot"
	}
	tail := ""
	if im.W > 0 {
		tail = fmt.Sprintf(" %d×%d", im.W, im.H)
	}
	if n := len(s.Res.Images); n > 1 {
		tail += fmt.Sprintf(" +%d more", n-1)
	}
	tail = stDim.Render(tail) + failMark(s)
	return stDim.Render("▣ ") + truncPath(what, w-2-lipgloss.Width(tail)) + tail
}

// imageDetail lists the images: type, size, bytes.
func imageDetail(_ stepCtx, s *claude.Step, w int) []string {
	var out []string
	for _, im := range s.Res.Images {
		l := im.MediaType
		if im.W > 0 {
			l += fmt.Sprintf(" %d×%d", im.W, im.H)
		}
		out = append(out, stDim.Render(trunc(fmt.Sprintf("%s · %d KB", l, (im.Size+1023)/1024), w)))
	}
	return append(out, stDim.Render("enter: open"))
}

var reNotID = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// imageDir is where enter writes images to open them.
func imageDir() string { return filepath.Join(os.TempDir(), "regia-images") }

// imageActivate writes the step's images to temp files and opens them.
func imageActivate(m *model, a *claude.Agent, s *claude.Step) tea.Cmd {
	if !hasImages(s) {
		return nil
	}
	path := claude.TranscriptPath(a)
	var cmds []tea.Cmd
	for _, im := range s.Res.Images {
		p, err := saveImage(path, s.Res, im)
		if err != nil {
			m.flash("image: %v", err)
			return nil
		}
		cmds = append(cmds, openThing(openTarget{Kind: "image", Path: p}))
	}
	return tea.Batch(cmds...)
}

// saveImage decodes an image from the transcript into imageDir.
func saveImage(transcript string, r *claude.Result, im claude.Image) (string, error) {
	b, err := claude.ImageData(transcript, r, im)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(imageDir(), 0o700); err != nil {
		return "", err
	}
	ext := strings.TrimPrefix(im.MediaType, "image/")
	if ext == "" || strings.ContainsAny(ext, "/.") {
		ext = "img"
	}
	id := reNotID.ReplaceAllString(r.ID, "")
	p := filepath.Join(imageDir(), fmt.Sprintf("%s-%d.%s", id, im.N, ext))
	return p, os.WriteFile(p, b, 0o600)
}

// --- Agent / Task subagents ------------------------------------------------------------

// subagentLine is "⇢ subagent: review panel (done)".
func subagentLine(_ stepCtx, s *claude.Step, w int) string {
	tail := ""
	switch {
	case s.Running():
		tail = stBusy.Render(" (running)")
	case s.Failed():
		tail = stErr.Render(" ✗")
	case resInfo(s, "status") == "completed":
		tail = stDim.Render(" (done)")
	case resInfo(s, "status") == "async_launched":
		tail = stDim.Render(" (in background)")
	}
	d := flat(s.In["description"])
	if d == "" {
		d = s.In["subagent_type"]
	}
	return stDim.Render("⇢ subagent: ") + trunc(d, w-12-lipgloss.Width(tail)) + tail
}

// subagentReport is the subagent's report: its last reply, read from its own
// transcript (Claude Code's outputFile) when that's there, else the result.
func subagentReport(s *claude.Step) string {
	if s.Res != nil {
		if p := s.Res.Info["outputFile"]; p != "" {
			if _, err := os.Stat(p); err == nil {
				msgs := claude.Messages(p)
				for i := len(msgs) - 1; i >= 0; i-- {
					if msgs[i].Role == claude.RoleAgent {
						return msgs[i].Text
					}
				}
			}
		}
	}
	return s.Out
}

func subagentDetail(_ stepCtx, s *claude.Step, w int) []string {
	if s.Running() {
		return []string{stBusy.Render("running…")}
	}
	var ls []string
	for _, l := range lines(subagentReport(s)) {
		for _, wl := range wrap(untab(l), w) {
			ls = append(ls, strings.TrimRight(wl, " "))
		}
	}
	st := lipgloss.NewStyle()
	if s.Failed() {
		st = stErr
	}
	return headLines(ls, maxHeadLines, w, st)
}

// subagentActivate pages the subagent's whole conversation (its transcript),
// or its report.
func subagentActivate(m *model, _ *claude.Agent, s *claude.Step) tea.Cmd {
	if s.Res != nil {
		if p := s.Res.Info["outputFile"]; p != "" {
			if _, err := os.Stat(p); err == nil {
				var b strings.Builder
				for _, msg := range claude.Messages(p) {
					if msg.Role == claude.RoleTool {
						continue
					}
					who := "agent"
					if msg.Role == claude.RoleYou {
						who = "prompt"
					}
					fmt.Fprintf(&b, "▸ %s %s\n%s\n\n", who, clock(msg.At), strings.TrimSpace(msg.Text))
				}
				return pageText(m, b.String())
			}
		}
	}
	return pageText(m, s.Out)
}

// --- WebFetch / WebSearch -------------------------------------------------------------

// webLine is "◦ fetch https://…" or `◦ search "query"`.
func webLine(_ stepCtx, s *claude.Step, w int) string {
	tail := failMark(s)
	if s.Name == "WebSearch" {
		return stDim.Render("◦ search ") + trunc(strconv.Quote(s.In["query"]), w-9-lipgloss.Width(tail)) + tail
	}
	if code := resInfo(s, "code"); code != "" && code != "200" {
		tail = stErr.Render(" "+code) + tail
	}
	u := s.In["url"]
	return stDim.Render("◦ fetch ") + linkify(trunc(u, w-8-lipgloss.Width(tail)), openTarget{Kind: "url", URL: u}) + tail
}

// webDetail is the first lines of what came back.
func webDetail(_ stepCtx, s *claude.Step, w int) []string {
	if s.Running() {
		return []string{stBusy.Render("running…")}
	}
	var ls []string
	for _, l := range lines(s.Out) {
		for _, wl := range wrap(untab(l), w) {
			ls = append(ls, strings.TrimRight(wl, " "))
		}
	}
	st := stDim
	if s.Failed() {
		st = stErr
	}
	return headLines(ls, 8, w, st)
}

var reURL = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)

// webActivate opens the fetched URL, or a search's first result.
func webActivate(m *model, _ *claude.Agent, s *claude.Step) tea.Cmd {
	u := s.In["url"]
	if u == "" {
		u = reURL.FindString(s.Out)
	}
	if u == "" {
		m.flash("no link to open")
		return nil
	}
	return openThing(openTarget{Kind: "url", URL: u})
}

// --- TodoWrite ----------------------------------------------------------------------------

// todoLine is "☐ todos 3/7" (done of all).
func todoLine(_ stepCtx, s *claude.Step, w int) string {
	done := 0
	for _, t := range s.Todos {
		if t.Status == "completed" {
			done++
		}
	}
	return stDim.Render(trunc(fmt.Sprintf("☐ todos %d/%d", done, len(s.Todos)), w))
}

// todoDetail is the list as written then: ✓ done, ▸ doing, ◻ to do.
func todoDetail(_ stepCtx, s *claude.Step, w int) []string {
	var out []string
	for _, t := range s.Todos {
		switch t.Status {
		case "completed":
			out = append(out, stDim.Render(trunc("✓ "+flat(t.Content), w)))
		case "in_progress":
			out = append(out, stBusy.Render(trunc("▸ "+flat(t.Content), w)))
		default:
			out = append(out, trunc("◻ "+flat(t.Content), w))
		}
	}
	return out
}
