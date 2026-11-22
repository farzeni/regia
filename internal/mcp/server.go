// Package mcp is `regia mcp`: an MCP server on stdio that Claude Code starts
// in every session (the plugin's .mcp.json), giving agents regia's tree as
// tools. The tools themselves, and who may do what, live in internal/group;
// this package only describes them to the model and passes calls through.
package mcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/farzeni/regia/internal/group"
)

// instructions is what the model is told about the server as a whole.
const instructions = `regia is the control room the user runs Claude Code agents from: a tree of groups and agents, where a coordinator holds the agents it started. These tools read that tree and, if you may, organize it. Call whoami first; inbox says who needs the user, timeline what happened lately, changes which files an agent edited. A worker can only read its group; a coordinator organizes the agents under it; an orchestrator organizes the group the user granted it. A refusal is final: don't work around it. Every change is shown to the user and can be undone in regia. The regia:organize skill explains how to use these tools well.`

type noArgs struct{}

type treeArgs struct {
	Archived bool `json:"archived,omitempty" jsonschema:"also list archived agents (to bring one back)"`
}

type agentArgs struct {
	ID string `json:"id" jsonschema:"the agent's id, from tree"`
}

type createArgs struct {
	Title  string   `json:"title" jsonschema:"the group's title"`
	Parent string   `json:"parent,omitempty" jsonschema:"where the group goes: a group's or an agent's id; empty for the top of what you may organize"`
	Agents []string `json:"agents,omitempty" jsonschema:"ids of agents or groups to move into it"`
}

type renameArgs struct {
	Group string `json:"group" jsonschema:"the group's id (g:…)"`
	Title string `json:"title" jsonschema:"its new title"`
}

type groupArgs struct {
	Group string `json:"group" jsonschema:"the group's id (g:…)"`
}

type moveArgs struct {
	Agents []string `json:"agents" jsonschema:"ids of agents or groups to move; what is under them moves too"`
	To     string   `json:"to" jsonschema:"the id of the group or agent to put them under"`
	After  string   `json:"after,omitempty" jsonschema:"the id of a node already in 'to' to put them after; empty for the end"`
}

type agentsArgs struct {
	Agents []string `json:"agents" jsonschema:"ids of the agents"`
}

type timelineArgs struct {
	ID    string  `json:"id,omitempty" jsonschema:"a group's or agent's id (an agent includes the agents under it); empty for everything you may read"`
	Hours float64 `json:"hours,omitempty" jsonschema:"how far back, in hours (default 24)"`
}

type renameAgentArgs struct {
	ID   string `json:"id" jsonschema:"the agent's id"`
	Name string `json:"name" jsonschema:"the name regia shows for it; empty to go back to its own name"`
}

// inboxOut wraps the inbox: a tool's structured result must be an object.
type inboxOut struct {
	Agents []group.InboxItem `json:"agents"`
}

// done is what a write tool returns.
type done struct {
	Done string `json:"done"`
}

func wrote(s string, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	return nil, done{Done: s}, nil
}

func ro(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(false)}
}

func rw(title string, destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: new(destructive), OpenWorldHint: new(false)}
}

// NewServer builds the server for the agent self (group.SelfFromEnv() in a
// session).
func NewServer(self, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "regia", Version: version}, &mcp.ServerOptions{Instructions: instructions})

	mcp.AddTool(s, &mcp.Tool{Name: "whoami", Annotations: ro("Who am I in regia"),
		Description: "Your place in regia: your id and name, your role (worker, coordinator, orchestrator), what you may organize, and your group."},
		func(context.Context, *mcp.CallToolRequest, noArgs) (*mcp.CallToolResult, any, error) {
			out, err := group.Whoami(self)
			return nil, out, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "tree", Annotations: ro("regia's tree"),
		Description: "The part of regia's tree you may read, nested: groups and agents with each agent's state (working, waiting on the user, idle, done, failed), its question if it waits, what it is doing and when it last changed. A worker sees its group; a coordinator or orchestrator sees what it may organize."},
		func(_ context.Context, _ *mcp.CallToolRequest, a treeArgs) (*mcp.CallToolResult, any, error) {
			out, err := group.TreeOf(self, a.Archived)
			return nil, out, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "agent", Annotations: ro("One agent"),
		Description: "One agent you may read, in detail: state, question, what it is doing, its directory and branch, PRs, whether it looks stuck, the agents under it, and its last few messages."},
		func(_ context.Context, _ *mcp.CallToolRequest, a agentArgs) (*mcp.CallToolResult, any, error) {
			out, err := group.AgentInfo(self, a.ID)
			return nil, out, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "inbox", Annotations: ro("Who needs the user"),
		Description: "The agents you may read that need the user: waiting on a question (with the question), looking stuck (with why), or failed; the longest waiting first. Start a triage or a status report here."},
		func(context.Context, *mcp.CallToolRequest, noArgs) (*mcp.CallToolResult, any, error) {
			as, err := group.Inbox(self)
			if as == nil {
				as = []group.InboxItem{}
			}
			return nil, inboxOut{as}, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "timeline", Annotations: ro("What happened"),
		Description: "What happened in a group (or under an agent) lately, oldest first: agents started, waiting, back to work, done or failed, the PRs they opened and the commits on their branches. For 'what happened while I was away' and status reports."},
		func(_ context.Context, _ *mcp.CallToolRequest, a timelineArgs) (*mcp.CallToolResult, any, error) {
			h := a.Hours
			if h <= 0 {
				h = 24
			}
			out, err := group.Timeline(self, a.ID, time.Now().Add(-time.Duration(h*float64(time.Hour))))
			return nil, out, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "changes", Annotations: ro("Files an agent changed"),
		Description: "The files an agent edited in its conversation, the most recent first, with lines added and removed. Compare siblings' files to spot overlapping work before it conflicts."},
		func(_ context.Context, _ *mcp.CallToolRequest, a agentArgs) (*mcp.CallToolResult, any, error) {
			out, err := group.Changes(self, a.ID)
			return nil, out, err
		})

	mcp.AddTool(s, &mcp.Tool{Name: "rename_agent", Annotations: rw("Rename an agent", false),
		Description: "Set the name regia shows for an agent inside what you may organize (or yourself), so the tree reads well; empty goes back to its own name. Claude Code is not touched."},
		func(_ context.Context, _ *mcp.CallToolRequest, a renameAgentArgs) (*mcp.CallToolResult, any, error) {
			return wrote(group.RenameAgent(self, a.ID, a.Name))
		})

	mcp.AddTool(s, &mcp.Tool{Name: "pin", Annotations: rw("Pin agents", false),
		Description: "Keep agents listed in regia when they are done (the user hides finished ones otherwise): for work the user should still look at. Inside what you may organize, or yourself."},
		func(_ context.Context, _ *mcp.CallToolRequest, a agentsArgs) (*mcp.CallToolResult, any, error) {
			return wrote(group.Pin(self, a.Agents, true))
		})

	mcp.AddTool(s, &mcp.Tool{Name: "unpin", Annotations: rw("Unpin agents", false),
		Description: "Let finished agents be hidden again (undoes pin)."},
		func(_ context.Context, _ *mcp.CallToolRequest, a agentsArgs) (*mcp.CallToolResult, any, error) {
			return wrote(group.Pin(self, a.Agents, false))
		})

	mcp.AddTool(s, &mcp.Tool{Name: "create_group", Annotations: rw("Create a group", false),
		Description: "Create a group in regia's tree, optionally moving agents into it. Coordinators and orchestrators only, inside what they may organize."},
		func(_ context.Context, _ *mcp.CallToolRequest, a createArgs) (*mcp.CallToolResult, any, error) {
			return wrote(group.CreateGroup(self, a.Title, a.Parent, a.Agents))
		})

	mcp.AddTool(s, &mcp.Tool{Name: "rename_group", Annotations: rw("Rename a group", false),
		Description: "Rename a group inside what you may organize."},
		func(_ context.Context, _ *mcp.CallToolRequest, a renameArgs) (*mcp.CallToolResult, any, error) {
			return wrote(group.RenameGroup(self, a.Group, a.Title))
		})

	mcp.AddTool(s, &mcp.Tool{Name: "delete_group", Annotations: rw("Remove a group", false),
		Description: "Remove a group inside what you may organize. Nothing is lost: what was in it moves up into its place."},
		func(_ context.Context, _ *mcp.CallToolRequest, a groupArgs) (*mcp.CallToolResult, any, error) {
			return wrote(group.DeleteGroup(self, a.Group))
		})

	mcp.AddTool(s, &mcp.Tool{Name: "move", Annotations: rw("Move agents", false),
		Description: "Move agents or groups under another group or agent, both inside what you may organize."},
		func(_ context.Context, _ *mcp.CallToolRequest, a moveArgs) (*mcp.CallToolResult, any, error) {
			return wrote(group.Move(self, a.Agents, a.To, a.After))
		})

	mcp.AddTool(s, &mcp.Tool{Name: "archive", Annotations: rw("Archive agents", false),
		Description: "Hide finished or abandoned agents in regia (Claude Code is not touched; an archived agent that starts working comes back by itself). Inside what you may organize, never yourself."},
		func(_ context.Context, _ *mcp.CallToolRequest, a agentsArgs) (*mcp.CallToolResult, any, error) {
			return wrote(group.Archive(self, a.Agents, true))
		})

	mcp.AddTool(s, &mcp.Tool{Name: "unarchive", Annotations: rw("Bring agents back", false),
		Description: "Bring archived agents back into regia's tree (find them with tree and archived set)."},
		func(_ context.Context, _ *mcp.CallToolRequest, a agentsArgs) (*mcp.CallToolResult, any, error) {
			return wrote(group.Archive(self, a.Agents, false))
		})
	return s
}

// Run serves self's session on stdin/stdout until Claude Code closes it.
func Run(ctx context.Context, self, version string) error {
	return NewServer(self, version).Run(ctx, &mcp.StdioTransport{})
}
