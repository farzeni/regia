package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/farzeni/regia/internal/claude"
	"github.com/farzeni/regia/internal/group"
	"github.com/farzeni/regia/internal/layout"
)

// connect starts self's server and a client on in-memory pipes, over a tree
// where "coord" coordinates w1 and w2 in group g:1.
func connect(t *testing.T, self string) *mcp.ClientSession {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("REGIA_GROUP", "")
	old := group.Agents
	group.Agents = func() map[string]*claude.Agent {
		out := map[string]*claude.Agent{}
		for id, n := range map[string]string{"coord": "orchestrator", "w1": "api", "w2": "web"} {
			out[id] = &claude.Agent{ID: id, Name: n, State: "busy", Detail: "working on " + n}
		}
		return out
	}
	t.Cleanup(func() { group.Agents = old })
	layout.Update(func(l *layout.Layout) error {
		l.Groups["g:1"] = "shop"
		l.Parent = map[string]string{"g:1": "", "coord": "g:1", "w1": "coord", "w2": "coord"}
		l.Order = map[string][]string{"": {"g:1"}, "g:1": {"coord"}, "coord": {"w1", "w2"}}
		return nil
	})

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := NewServer(self, "test").Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func TestToolsListed(t *testing.T) {
	cs := connect(t, "w1")
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
		readOnly := tl.Annotations != nil && tl.Annotations.ReadOnlyHint
		if want := slices.Contains([]string{"whoami", "tree", "agent", "inbox", "timeline", "changes"}, tl.Name); readOnly != want {
			t.Errorf("%s: read-only %v", tl.Name, readOnly)
		}
		if tl.Description == "" || tl.InputSchema == nil {
			t.Errorf("%s: no description or schema", tl.Name)
		}
	}
	slices.Sort(names)
	if strings.Join(names, " ") != "agent archive changes create_group delete_group inbox move pin rename_agent rename_group timeline tree unarchive unpin whoami" {
		t.Fatalf("tools: %v", names)
	}
	if !strings.Contains(cs.InitializeResult().Instructions, "Call whoami first") {
		t.Fatal("server instructions missing")
	}
}

func TestCoordinatorThroughMCP(t *testing.T) {
	cs := connect(t, "coord")
	out, isErr := call(t, cs, "whoami", map[string]any{})
	var me group.WhoamiOut
	if isErr || json.Unmarshal([]byte(out), &me) != nil || me.Role != "coordinator" || me.GroupTitle != "shop" {
		t.Fatalf("whoami: %s", out)
	}

	out, isErr = call(t, cs, "create_group", map[string]any{"title": "backend", "agents": []string{"w1"}})
	if isErr || !strings.Contains(out, `created group \"backend\"`) {
		t.Fatalf("create_group: %s", out)
	}
	out, _ = call(t, cs, "tree", map[string]any{})
	var tree group.Node
	if json.Unmarshal([]byte(out), &tree) != nil || tree.ID != "coord" || len(tree.Children) != 2 {
		t.Fatalf("tree: %s", out)
	}
	if b := tree.Children[1]; b.Title != "backend" || len(b.Children) != 1 || b.Children[0].Doing != "working on api" {
		t.Fatalf("backend holds w1: %s", out)
	}

	// a refusal comes back as a tool error the model can read
	out, isErr = call(t, cs, "move", map[string]any{"agents": []string{"w2"}, "to": "g:1"})
	if !isErr || !strings.Contains(out, "outside what you may organize") {
		t.Fatalf("move out of scope: %v %s", isErr, out)
	}
	// bad arguments too
	if _, isErr := call(t, cs, "move", map[string]any{"to": "coord"}); !isErr {
		t.Fatal("move without agents should fail")
	}
	if len(group.Actions()) != 1 {
		t.Fatalf("one change recorded, got %d", len(group.Actions()))
	}
}
