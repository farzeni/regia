package ui

import (
	"os"
	"testing"

	"github.com/farzeni/regia/internal/claude"
)

// Real-data benchmarks of the per-tick and per-frame work (skipped unless
// REGIA_BENCH_JOB names a job to select, as in internal/claude):
//
//	REGIA_BENCH_JOB=<id> go test ./internal/ui -run x -bench Real
func benchModel(b *testing.B) *model {
	id := os.Getenv("REGIA_BENCH_JOB")
	if id == "" {
		b.Skip("set REGIA_BENCH_JOB")
	}
	m := newModel()
	m.w, m.h = 200, 50
	for i, r := range m.rows {
		if r.ID == id {
			m.moveCursor(i - m.cursor)
		}
	}
	m.View()
	return m
}

// Every second: job files re-read, rows rebuilt, the selected history loaded.
func BenchmarkRealRefresh(b *testing.B) {
	m := benchModel(b)
	b.ReportAllocs()
	for b.Loop() {
		m.refresh()
	}
}

// Every frame (8 a second while an agent works).
func BenchmarkRealView(b *testing.B) {
	m := benchModel(b)
	b.ReportAllocs()
	for b.Loop() {
		m.View()
	}
}

// The agent panel's conversation on a real transcript (REGIA_BENCH_TRANSCRIPT),
// 100 columns:
//
//	REGIA_BENCH_TRANSCRIPT=~/.claude/projects/…/x.jsonl go test ./internal/ui -run x -bench Conv
//
// cold: first draw (markdown rendered); new: a step was added (the markdown
// cache holds, the blocks are laid out again); frame: nothing changed, in
// focus (the cursor's bar painted on a copy); open: every run unfolded.
var benchW = 100

func BenchmarkConv(b *testing.B) {
	p := os.Getenv("REGIA_BENCH_TRANSCRIPT")
	if p == "" {
		b.Skip("set REGIA_BENCH_TRANSCRIPT")
	}
	if os.Getenv("REGIA_BENCH_LINKS") != "" { // paths and URLs in replies linked
		orig := openers
		defer func() { openers = orig }()
		openers, _ = buildOpeners("openers.conf", "preset = vscode\nosc8 = on", "linux", noEnv)
	}
	m := &model{}
	a := &claude.Agent{ID: "bench", Transcript: p, Cwd: "/wt"}
	claude.Messages(p) // parsed once, before: the parse is BenchmarkMessagesCold
	reset := func() {
		for i := range convCache {
			convCache[i].lines = nil
		}
	}
	b.Run("cold", func(b *testing.B) {
		for b.Loop() {
			reset()
			clear(mdCache)
			clear(linkCache)
			m.conversationLines(a, benchW)
		}
	})
	b.Run("new", func(b *testing.B) {
		for b.Loop() {
			reset()
			m.conversationLines(a, benchW)
		}
	})
	b.Run("frame", func(b *testing.B) {
		m.convFocus = true
		b.ReportAllocs()
		for b.Loop() {
			m.conversationLines(a, benchW)
		}
		m.convFocus = false
	})
	b.Run("open", func(b *testing.B) {
		st := m.convOf(a.ID)
		for _, bl := range m.conv.blocks {
			if bl.kind == blockRun {
				st.open[bl.key] = true
			}
		}
		st.ver++
		for b.Loop() {
			reset()
			m.conversationLines(a, benchW)
		}
		b.ReportMetric(float64(len(m.conversationLines(a, 100))), "lines")
	})
}
