package ui

import (
	"os"
	"testing"
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
