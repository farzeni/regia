package mcp

import (
	"strings"
	"testing"
)

func TestClaimThroughMCP(t *testing.T) {
	cs := connect(t, "w1")
	out, isErr := call(t, cs, "rename_agent", map[string]any{"id": "w2", "name": "frontend"})
	if !isErr || !strings.Contains(out, "call claim") {
		t.Fatalf("a worker is told to claim first: %s", out)
	}
	out, isErr = call(t, cs, "claim", map[string]any{"scope": "group", "reason": "tidy names"})
	if isErr || !strings.Contains(out, "claimed") {
		t.Fatalf("claim: %s", out)
	}
	if out, isErr := call(t, cs, "rename_agent", map[string]any{"id": "w2", "name": "frontend"}); isErr {
		t.Fatalf("after the claim: %s", out)
	}
	if out, isErr := call(t, cs, "release", map[string]any{}); isErr {
		t.Fatalf("release: %s", out)
	}
}
