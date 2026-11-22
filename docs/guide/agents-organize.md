# Agents that organize regia

Agents can read regia's tree and, when allowed, organize it: "split the agents
on the shop into frontend and backend", "these five look stale, sort them
out", "check what your siblings are doing". The plugin starts `regia mcp`, a
small MCP server, in every session (no daemon: it lives and dies with the
session), and ships the `regia:organize` skill that tells agents how to use it.

## Tools

- read: `whoami`, `tree` (`brief` for the shape, `under` for one group),
  `agent`; `inbox` (who is waiting, stuck, has a PR that fails CI or has
  changes requested, or failed), `timeline` (what happened in a group in the
  last hours: the same feed as `T`), `changes` (the files an agent edited, as
  in the changes tab);
- change: `create_group`, `rename_group`, `delete_group`, `move`, `archive`,
  `unarchive`, `rename_agent` (the name regia shows), `pin`, `unpin`;
- `claim` / `release`: take or give back a part of the tree to organize.

## Who may do what

What an agent may do follows from where it is (the server knows the calling
session) and what it claimed:

| Role | Who | Reads | Organizes |
|---|---|---|---|
| worker | any agent | its group | nothing, until it claims |
| coordinator | an agent with agents under it | the agents under it | the agents under it (itself only to rename or pin) |
| orchestrator | an agent that claimed its group or the whole tree, or that you pressed `O` on | what it claimed or was granted | the same |

An agent asked to organize more than its own agents first **claims** its group
or the whole tree, with a reason. Nothing stops it from claiming: the point is
that you see it. The agent is marked ✦ in the tree, its box says what it claimed
and why ("claims ✦ the whole tree: morning triage"), the claim shows in the
status line and in `U`. A claim ends when the agent releases it, when it is
done, or when you press `O` on it.

You can also grant an agent part of the tree yourself with `O` (its group, then
the whole tree, then nothing); a grant lasts until you take it back.

Muted groups (`M`) are out of reach to claims: mute what agents must leave
alone. Only a whole-tree grant you gave reaches them. `archive` only hides an
agent in regia; it never stops one.

## Seeing and undoing changes

Every change appears in the status line as it happens ("orchestrator: moved 2
items into backend · U to undo"). `U` lists all the agents' changes, newest
first; `enter` undoes the selected one, unless what it changed was changed
again since (regia says so and leaves it). The log is
`~/.config/regia/actions.jsonl`. Changes from agents and from you are merged,
so neither overwrites the other.

`REGIA_GROUP=off` (or `regia group off`) turns the tools off along with
[group awareness](./group-awareness).

How it was designed, and why there is no daemon: [the design](../design/agents-mcp).
