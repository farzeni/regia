package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistorySourcesAndMeta(t *testing.T) {
	cd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cd)
	write := func(rel, body string) string {
		p := filepath.Join(cd, "projects", rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("-a/listed.jsonl", "{}\n")
	write("-a/old.jsonl", "{}\n")
	big := write("-b/old.jsonl", `{"type":"user","cwd":"/w/b","message":{"content":"hi"}}`+"\n"+
		`{"type":"ai-title","aiTitle":"Claude's title"}`+"\n"+
		`{"type":"custom-title","customTitle":"Mine"}`+"\n"+
		`{"type":"ai-title","aiTitle":"A later one"}`+"\n"+
		`{"type":"user","cwd":"/w/elsewhere","message":{"content":"bye"}}`+"\n")
	write("-a/old/subagents/agent-1.jsonl", "{}\n")

	srcs := HistorySources(map[string]bool{"listed": true})
	if len(srcs) != 1 || srcs[0].ID != "old" || srcs[0].Path != big {
		t.Fatalf("want only old, from its bigger transcript: %+v", srcs)
	}
	if m := Meta(big); m.Title != "Mine" || m.Cwd != "/w/b" {
		t.Fatalf("your title over Claude's, the first directory: %+v", m)
	}
	if got := FirstPrompt(Messages(big)); got != "hi" {
		t.Fatalf("first prompt %q", got)
	}
}
