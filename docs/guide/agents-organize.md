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
- end sessions: `stop` and `remove` (see below);
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

## Stopping and removing sessions

Two tools reach into Claude Code itself, with the same scope as the others (a
coordinator acts on the agents under it, anything else needs a claim, muted
parts are out of reach, never the agent itself). Both need a reason, which
you see in the status line and in `U`, and neither can be undone there.

- `stop` runs `claude stop`: the session stays listed and resumable, and
  starts again when someone messages it. It may stop a working agent: that is
  what it is for.
- `remove` runs `claude rm`, as `x` does: the session leaves regia and
  `claude agents`, its worktree is deleted when that's safe, and the
  conversation stays findable in search's whole history (`ctrl+g`), where
  `enter` resumes it. It refuses an agent that is working or waiting on you,
  and a coordinator with agents still listed under it (named in the same call,
  they are removed first, so nothing moves up a level). `claude rm`'s own
  refusals (unpushed commits) come back as the reason.

Your [plugins](./plugins) can hold a removal back, from `x` and from agents
alike: a plugin with `"gates": ["remove"]` is asked first and may refuse
("no handoff written yet"). This is how a team rule such as "a session is
removed only after it wrote its handoff" is kept in your own script.

A typical cleanup: a coordinator's agents each finish by writing a handoff
file; the coordinator then calls `remove` on the finished ones with the reason
"done, handoff written". Removed sessions don't get woken up later with a cold
cache, and a plugin listing `"removed"` in its states shows the handoff next to
the old session in the whole history.

## Seeing and undoing changes

Every change appears in the status line as it happens ("orchestrator: moved 2
items into backend · U to undo"). `U` lists all the agents' changes, newest
first; `enter` undoes the selected one, unless what it changed was changed
again since (regia says so and leaves it). Stops and removals are listed too,
marked "can't be undone". The log is
`~/.config/regia/actions.jsonl`. Changes from agents and from you are merged,
so neither overwrites the other.

`REGIA_GROUP=off` (or `regia group off`) turns the tools off along with
[group awareness](./group-awareness).

How it was designed, and why there is no daemon: [the design](../design/agents-mcp).
