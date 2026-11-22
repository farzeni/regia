# Organizing the tree

## How agents are placed

When regia first sees a session it places it automatically:

1. under the session it was **forked** from;
2. else under the session that **started it** with `claude --bg` (learned by the
   plugin's hook, or by the backfill);
3. else in a **group named after its project** (`~/Projects/<name>/…`;
   worktrees under `.claude/worktrees/` count as their repo).

If the spawn link shows up after the agent was placed in its project group,
the agent moves under its coordinator. As soon as you move an agent yourself
(`>`, `<`, `m`, or by starting it from regia's input), it is **pinned** and
regia never moves it again. When a coordinator is removed from the list, its
agents move up a level instead of disappearing.

Your organization is saved in `~/.config/regia/layout.json`; spawn links in
`~/.config/regia/spawns.jsonl`.

### How spawn tracking works

Claude Code doesn't record which session started a `claude --bg` session, so
the plugin adds one `PostToolUse` hook on `Bash` to every session. After each
Bash command it runs `regia hook spawn`, which looks for the
`backgrounded · <id>` line that `claude --bg` prints and appends
`{parent, child}` to `spawns.jsonl`. It takes a few milliseconds, never fails
loudly, and does nothing if `regia` isn't on `PATH`. Coordinators don't need to
know regia exists.

## Moving things yourself

| Key | Action |
|---|---|
| `J` / `K` | move down / up among siblings |
| `>` / `<` | nest under the agent above (it becomes the coordinator) / un-nest |
| `m`, then `p` / `P` | move: `p` puts it into the group under the cursor (at the top), or right after the agent; `P` puts it after the group, or under the agent (as its coordinator) |
| `n` / `r` / `d` | new group / rename (group or agent alias) / delete group (its agents move up); the name is typed right in the row (`enter` saves, `esc` cancels) |
| `w` | set the directory new agents in the group start in (see [Where a new agent starts](./starting-agents#where-a-new-agent-starts)) |

## Cleaning up

Agents pile up; three ways to keep the tree short:

- **Archive** (`e`) hides an agent from regia only: Claude Code still has it,
  `A` shows the archive, and `e` there brings one back. An archived agent that
  starts working again (you resumed it) comes back by itself. Archived agents
  don't count in the header and aren't in the inbox. Stored in `layout.json`.
- **Remove** (`x`) runs `claude rm`: the session leaves `claude agents` too,
  its worktree is deleted if that's safe, and the conversation stays
  resumable. It refuses over unpushed commits.
- **Select mode** (`v`) does either in batches: mark rows with `space` (a
  group marks all its agents, `*` marks everything shown), then `e` or `x`
  (one confirmation; refusals are listed, the rest go through). In select mode
  `m` picks them all up to move, `n` makes a new group of them and `f` pins
  them.

Actions that ask first (`x`, `s`, `d`, `e` on a group, and `e`/`x` in select
mode) are confirmed by pressing the same key again; any other key cancels.
