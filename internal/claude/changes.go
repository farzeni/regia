package claude

import (
	"path/filepath"
	"sort"
	"strings"
)

// EditTools are the tools that change a file.
var EditTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

// ChangedFile is one file an agent touched, its edits summed.
type ChangedFile struct {
	Path           string `json:"path"` // absolute
	Rel            string `json:"-"`    // relative to the agent's directory when inside it, else Path
	Added, Removed int    `json:"-"`
	Line           int    `json:"-"` // where its last edit starts (0: unknown)
	last           int    // the step that last touched it, for the order
}

// ChangedFiles sums the successful edits in es (Conversation) by file,
// newest first.
func ChangedFiles(es []Entry, cwd string) []ChangedFile {
	idx := map[string]int{}
	var out []ChangedFile
	for n, e := range es {
		s := e.Step
		if s == nil || s.Res == nil || s.Res.IsError || !EditTools[s.Name] {
			continue
		}
		p := s.Res.FilePath
		if p == "" {
			p = s.In["file_path"]
		}
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		i, ok := idx[p]
		if !ok {
			i = len(out)
			idx[p] = i
			rel := p
			if r, err := filepath.Rel(cwd, p); err == nil && cwd != "" && !strings.HasPrefix(r, "..") {
				rel = r
			}
			out = append(out, ChangedFile{Path: p, Rel: rel})
		}
		f := &out[i]
		f.Added += s.Res.Added
		f.Removed += s.Res.Removed
		f.Line, f.last = editLine(s.Res), n
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].last > out[j].last })
	return out
}

// editLine is the first changed line of an edit's last hunk.
func editLine(r *Result) int {
	if len(r.Patch) == 0 {
		return 0
	}
	h := r.Patch[len(r.Patch)-1]
	n := h.NewStart
	for _, l := range h.Lines {
		if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
			return n
		}
		n++
	}
	return h.NewStart
}
