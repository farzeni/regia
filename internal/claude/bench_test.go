package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real-data benchmarks (skipped unless REGIA_BENCH_JOB names a job).
func benchAgent(b *testing.B) *Agent {
	id := os.Getenv("REGIA_BENCH_JOB")
	if id == "" {
		b.Skip("set REGIA_BENCH_JOB")
	}
	a := &Agent{ID: id}
	Enrich(a)
	return a
}

// Cold: first look at an agent (scan up to 4 MB of transcript).
func BenchmarkHistoryCold(b *testing.B) {
	a := benchAgent(b)
	for i := 0; i < b.N; i++ {
		ucMu.Lock()
		ucMap = map[string]userCache{}
		ucMu.Unlock()
		hcMu.Lock()
		hcMap = map[string]histEntry{}
		hcMu.Unlock()
		ReadHistory(a)
	}
}

// Tick: transcript grew a little since the last read (busy agent, every second).
func BenchmarkHistoryTick(b *testing.B) {
	a := benchAgent(b)
	ReadHistory(a)
	for i := 0; i < b.N; i++ {
		ucMu.Lock()
		c := ucMap[a.Transcript]
		c.size -= 4096 // pretend the last 4 KB are new
		ucMap[a.Transcript] = c
		ucMu.Unlock()
		ReadHistory(a)
	}
}

// key must normalize like the regexp version it replaced: whitespace runs
// collapsed, trimmed, lowercased, cut to 48 runes.
func TestKeyMatchesNorm(t *testing.T) {
	ref := func(s string) string {
		n := []rune(norm(s))
		if len(n) > 48 {
			n = n[:48]
		}
		return string(n)
	}
	for _, s := range []string{"", "  ", "Hello  World", "\tA\n\nb  ", "<pasted_content id=\"x\">  Some LONG text that goes past forty-eight runes easily, yes",
		strings.Repeat("ab ", 30), strings.Repeat("é", 60), "x" + strings.Repeat(" ", 47) + "y", strings.Repeat("a", 47) + "   bcd"} {
		if got, want := key(s), ref(s); got != want {
			t.Errorf("key(%q) = %q, want %q", s, got, want)
		}
	}
}

// Incremental reads must find the same messages as a full scan.
func TestIncrementalUserMessages(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jsonl")
	write := func(s string) {
		f, _ := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		_, _ = f.WriteString(s)
		_ = f.Close()
	}
	write(`{"type":"user","message":{"content":"first message"}}` + "\n" + `{"type":"user","message":{"conte`) // half line
	if k := userMessages(p); !k[key("first message")] || len(k) != 1 {
		t.Fatalf("after first read: %v", k)
	}
	write(`nt":"second message"}}` + "\n")
	if k := userMessages(p); !k[key("second message")] || !k[key("first message")] {
		t.Fatalf("after append: %v", k)
	}
}
