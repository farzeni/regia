# Design: agents that act on regia (`regia mcp`)

Status: proposal, not implemented. Decisions still open are marked **Decision**.

## Goal

Agents already *know* they run under regia: the plugin's group hook tells a grouped agent about its group,
coordinator and siblings, and `regia context` / `regia note` let it read the group and write on its blackboard.

This design lets some agents also *act* on regia's structure, the same tree the user organizes in the TUI:
read the whole tree, create groups, move agents, archive the lost ones. It applies least privilege. A worker
can do what it does today, and nothing more. A coordinator can organize what is under it. Only an agent the
user picks can touch the wider tree.

The requests it must serve:

- "Reorganize the workers on domain X into two subgroups, frontend and backend."
- "Ask your siblings for their status and whether we can share some tasks."
- "Sessions X, Y, Z, W look stale or lost and belong to different domains: sort them out."

## Summary

- **No daemon.** A stdio MCP server, `regia mcp`, declared in the plugin's `.mcp.json`. Claude Code starts one
  per session when the session starts and stops it when the session ends, as it already does for the hooks.
- **The caller is known from the session.** The server inherits the session's environment, so
  `group.SelfFromEnv()` names the calling agent. Every tool works from that agent's position in the tree.
- **Scope is checked by the server, not left to the agent.** Each write is checked against the caller's role
  and the part of the tree it covers.
- **A skill** teaches how and when to use the tools (`skills/regia/SKILL.md` in the plugin).
- **Safe writes first.** Today the TUI can overwrite a change another process made to `layout.json`. That is
  fixed before any agent can write.

## Why not a daemon, why not only the CLI

A daemon would provide a long-lived process, a push channel and one place where writes are serialized.
regia doesn't need any of them:

- State is files (`layout.json`, `notes/`, `spawns.jsonl`). Every process re-reads them when it needs them.
- The push channel already exists: `regia hook group` adds a line to an agent's context when something
  changes for it (a new note, being moved).
- Serialization is a file lock (see "Safe concurrent writes").

Write actions use MCP rather than a skill driving the `regia` CLI through Bash, for three reasons:

- **One tool per action means per-action permissions.** `mcp__plugin_regia_regia__context` can be always
  allowed while `…__move` prompts, using normal Claude Code settings. Bash rules (`Bash(regia *)`) are coarse.
- **The caller is set by Claude Code.** The MCP server's environment comes from Claude Code. A Bash command's
  environment comes from the agent, which can write `CLAUDE_JOB_DIR=… regia …`.
- **Results are structured** (JSON for the tree), so the agent has no text to parse.

The read-only CLI (`regia context`, `regia note`) stays as it is. It works where MCP doesn't, for example in a
session started before the plugin update.

## Threat model

Agents are cooperative but can make mistakes, and they can be steered by what they read (prompt injection
from a web page, an issue or a file). Scoping limits the damage of a confused or steered agent. It is **not**
a boundary against a hostile local process: any agent with Bash can edit `~/.config/regia/layout.json`
directly. Being honest about this keeps the design small, so there is no auth, no tokens and no signatures.

What the scoping guarantees:

- an agent cannot reorganize, archive or rename anything outside its scope through the tools;
- every write is recorded and can be undone from the TUI;
- nothing an agent does through regia touches Claude Code itself: no stopping, deleting or messaging sessions
  in v1 (archive only hides an agent in regia, and an archived agent that starts working comes back).

## Roles and scope

A role is derived from the caller's position. Nothing is configured, except the orchestrator grant.

| Role | Who | Scope (what it may change) |
|---|---|---|
| worker | any agent (default) | nothing in the tree; can write notes to its nearest group, as today |
| coordinator | an agent with agents under it (`Position.Family`), or one that started agents (spawns) | its own subtree: the agents under it and the groups among them |
| orchestrator | an agent the user grants it to in the TUI | the granted subtree (a group), or the whole tree |

Rules inside the scope:

- **Read** is wider than write. Every agent can read its neighbourhood, which is what `regia context` shows
  today. Coordinators and orchestrators can read their whole scope. The whole tree is readable only by an
  orchestrator with a whole-tree grant.
- **Move**: both the agent and the destination must be inside the scope. A coordinator cannot move itself out
  of its own group, and cannot pull in agents from outside.
- **Create group**: only under a node inside the scope (a coordinator's groups go under itself).
- **Rename / delete group**: only groups inside the scope. Delete lifts the children into the group's place,
  as in the TUI (`DeleteGroup`), so no agent is lost.
- **Archive / unarchive**: agents inside the scope, never the caller itself.
- **Hand placements** (`Pinned`: placed by the user in the TUI) can be moved by an orchestrator but not by a
  coordinator. A coordinator's request to move one fails with "placed by the user in regia".

**Decision: storing the grant.** Proposed: a new `layout.json` field `Grants map[agentID]string`
(value: a group id, or `"*"` for the whole tree), set and revoked with a new key in the TUI on the selected
agent, shown as a mark next to its name. An alternative is a grant that expires when the agent goes idle;
it is safer, but surprising when an agent resumes.

Muted groups (`M` in the TUI) remain invisible to group awareness, and they are also outside every scope
except a whole-tree orchestrator's.

## Tools (v1)

The tool names below are bare. Claude Code prefixes them as `mcp__plugin_regia_regia__<name>`. Read tools are
annotated `readOnlyHint` so auto mode and permission rules can let them through.

Read:

| Tool | Returns |
|---|---|
| `whoami` | the caller's id, name, role, scope, nearest group, coordinator |
| `tree` | the subtree the caller may read, as JSON: groups (id, title), agents (id, name, state, now-doing, last activity, waiting on the user?, archived?) |
| `agent` | one agent in the readable scope: state, question, now-doing, tokens, PR links, last few messages (roughly what `regia --panel` prints) |
| `notes` | a container's notes (the caller's visible containers only) |

Write:

| Tool | Does |
|---|---|
| `note` | writes on the nearest group's blackboard (same as `regia note`) |
| `create_group` | `{title, parent?, agents?}`: a new group under `parent` (default: the caller), with the given agents moved in |
| `rename_group` | `{group, title}` |
| `delete_group` | `{group}`: children lifted into its place |
| `move` | `{agents[], to, after?}` |
| `archive` / `unarchive` | `{agents[]}` |

Every write:

1. resolves the caller, loads the tree fresh, checks scope (an error names the rule that refused);
2. applies the change through the same `layout` functions the TUI uses (`GroupOf`, `Put`, `NewGroup`,
   `DeleteGroup`, `Archive`…) under the layout lock;
3. appends one line to `~/.config/regia/actions.jsonl`
   (`{at, by, tool, args, before}`, where `before` is the minimum needed to undo);
4. writes a note to each affected group ("backend-coordinator moved 3 agents into backend"), so the agents
   that were moved learn it through the existing hook.

Not in v1: stopping, starting or deleting sessions; aliases; changing other agents' muting; anything in
`~/.claude`.

## Talking to siblings

"Ask your siblings for their status" is two different things:

- **Reading status**: done already. `regia context` and the `tree` / `agent` tools show what each sibling is
  doing, from its job state.
- **Asking a question and getting an answer**: this needs a message delivered into another session. Options:
  1. the blackboard: write a note addressed to them ("@frontend: can you take the API client?"). Siblings see
     it through the hook within a minute and answer with a note. This works today, but it is asynchronous and
     only reaches agents that run tools or get a message;
  2. Claude Code's own cross-session messaging (`ListAgents` / `SendMessage` for local sessions), if it
     reaches background sessions. regia would only map its ids to the names those tools use;
  3. a regia tool that sends text into a session (as typing it in `claude attach` would). This is the most
     direct, but it puts words in another agent's mouth, so it needs the strongest scoping.

**Decision:** v1 ships option 1. The skill teaches agents to address notes to a sibling by name, and a note
that names an agent makes the hook speak to that agent at once instead of after the throttle. Check option 2
before designing option 3.

## Safe concurrent writes (prerequisite)

Today `layout.Save` writes atomically (temp file and rename), but the TUI only reloads a changed
`layout.json` on its 1 s refresh (`syncLayout`). If another process saves between that reload and a TUI save,
the TUI writes its stale copy back and the other change is lost. The TUI also saves often (`Seen` on every
agent you look at, placements on refresh), so with agents writing the window is real.

Proposal:

- `layout.Update(func(*Layout) error) error`: takes an exclusive `flock` on `layout.json.lock`, loads fresh,
  applies the function, saves, unlocks. The MCP server and any other non-TUI writer use only this.
- In the TUI, each mutation becomes a function given to a model helper (`m.edit(func(l *layout.Layout) error)`)
  that runs it the same way and then sets `m.layout` to the result. That is about 20 call sites in `ui`
  (`archive.go`, `move.go`, `pin.go`, `update.go`, `model.go`'s placement on refresh).
- The lock is only held for a load, apply and save of a small file, a few milliseconds, so the TUI never
  waits in a way the user notices. `View` never takes it.

This is worth doing even without agents: two regia instances have the same race today.

## Awareness changes

The hook's intro (SessionStart, and again after a compaction) gets one extra line when the server is
available and the agent has a role above worker: "You coordinate N agents in regia. The regia MCP tools let
you organize them (see the regia skill)." Workers' intro stays as it is. The hook stays quiet and fast,
because the role comes from the same `Tree.Resolve` it already runs.

## The skill

`skills/regia/SKILL.md` in the plugin, triggered by talk about groups, siblings, coordinators, reorganizing,
or stale or lost agents. It covers:

- what regia is, as seen from inside an agent: the tree, groups versus coordinators, the blackboard, that the
  user watches the tree;
- the roles, and that a refusal is final (don't work around it with Bash edits to `layout.json`);
- playbooks: splitting a group (`tree`, decide, `create_group` with agents, a note explaining the split);
  triaging stale agents (`agent` on each, then group by domain, archive the clearly finished ones, and report
  to the user the ones that need a decision instead of guessing); coordinating with siblings through
  addressed notes;
- etiquette: read before writing, make few large moves rather than many small ones, always leave a note
  saying what you changed and why.

The user's `swarm` skill should point to it for briefs.

## Implementation notes

- `cmd/regia/mcp.go` (the command) and a new standalone package, `internal/mcp`, which holds JSON-RPC over
  stdio (`initialize`, `tools/list`, `tools/call`) and depends on nothing in regia. The tools live in
  `internal/group` (`group/tools.go`) next to `Context`, because they need `Tree` and `Position`.
- **Decision: the MCP library.** Conventions say no new modules without asking. The subset needed (stdio, the
  three methods, text and JSON results) is about 200 lines by hand. The official Go SDK
  (`github.com/modelcontextprotocol/go-sdk`) would track protocol changes for us. Proposed: by hand, with a
  test that replays a recorded `initialize` / `tools/list` / `tools/call` exchange.
- `.mcp.json` at the plugin root: `{"mcpServers": {"regia": {"command": "regia", "args": ["mcp"]}}}`.
  If `regia` is not on PATH, the server shows as failed in `/mcp` and the session is otherwise unaffected.
- `regia group off` / `REGIA_GROUP=off`: the server still starts, but every tool answers "group awareness is
  off", the same switch as the hook.
- Tests drive the tools with temp XDG dirs and a fake agent listing, never the real `~/.claude`. Scope rules
  get a table test (role × tool × inside/outside the scope × pinned).
- README: a section "Agents that organize regia" (roles, the grant key, the action log and undo). Add the
  grant key to the help modal. Update `docs/plugins.md` only if plugin scripts get access to the action log.

## Plan

1. `layout.Update` and the TUI's `m.edit`. No behaviour change, and it fixes the two-instance race.
2. `regia mcp` with the read tools and `note`, plus `.mcp.json`. Useful right away, low risk.
3. Scoped write tools, the action log, undo in the TUI, and the auto-notes to affected groups.
4. Orchestrator grants (layout field, TUI key, mark).
5. The skill, the hook's intro line, README and help modal.
6. Sibling messaging beyond notes, after checking `SendMessage`.

## Open questions

- Grant storage and lifetime (see Roles).
- MCP library: by hand or the official SDK (see Implementation notes).
- Should a coordinator be allowed to archive agents it did not start but that the user moved under it?
  (Proposed: yes, archive is reversible and logged.)
- Undo in the TUI: one key that reverts the last agent action, or a list of actions to pick from?
