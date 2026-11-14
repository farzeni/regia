# regia

A fast terminal control room for Claude Code background agents: see who needs
you, organize agents your way (groups, coordinators with their agents, manual
order), and jump in and out of sessions instantly.

```
go build -o regia . && install -m755 regia ~/.local/bin/
regia setup      # once per machine: Claude Code plugin + backfill (see below)
regia            # ? shows all keys
regia --dump     # plain-text tree + inbox (no TTY needed)
```

`regia setup` runs `claude plugin install regia --marketplace
https://github.com/farzeni/regia.git` (this repo is its own marketplace;
`--source` overrides it, `--no-plugin` / `--no-backfill` skip a step) and scans
existing transcripts for past spawns. The plugin adds one `PostToolUse` hook on
`Bash` to every session: `regia hook spawn` reads the payload and, when the
command was a `claude --bg` that printed `backgrounded · <id>`, appends
`{parent, child}` to `~/.config/regia/spawns.jsonl`. It costs a few ms per Bash
call and does nothing if `regia` isn't on PATH. Coordinators need to know
nothing about regia. Update with `claude plugin update regia@regia`, remove with
`claude plugin uninstall regia@regia`.

## How it works

| Need | Source |
|---|---|
| list of sessions | `claude agents --json --all` (supported CLI, ~0.2s, async every 2s; last result cached so the first frame is instant) |
| question, suggested reply, now-doing, tokens, PRs, timeline | `~/.claude/jobs/<id>/state.json` and `timeline.jsonl`, read-only, every 1s. Internal format: every field is optional |
| jump in | `claude attach <id>` runs in the terminal; `←` inside the session returns to regia |
| stop / new agent / terminal output | `claude stop`, `claude --bg "<prompt>"`, `claude logs` |
| your organization | `~/.config/regia/layout.json` (groups, parents, order, aliases, folds) |

New agents are placed automatically: under the session they were forked from,
else under the session that started them with `claude --bg` (from
`spawns.jsonl`), otherwise in a group named after their project
(`~/Projects/<name>`, worktrees fold into their repo). An agent still sitting in
its project group moves under its spawner as soon as the link is known (the hook
can land a moment after the agent appears; a backfill finds old ones). Once you
move an agent by hand (`>` `<` `m`) it's pinned and never moved again. When a
coordinator is removed, its agents move up into its parent.

## Keys

The bottom line is an always-on input, focused at start (like `claude agents`):
type a task and press `enter` to start it next to the selected agent (inside
it if it's a group). `↑/↓` move the selection while you type, `enter` on an
empty input attaches, `esc` clears the text and, on an empty input, switches
to the command keys below; `i`, `c` or `C` (aim inside) go back to typing.
The input takes the usual readline keys: `alt+backspace`/`ctrl+w`, `alt+d`,
`ctrl+a`/`ctrl+e`, `alt+←/→`, `ctrl+k`/`ctrl+u`, `delete`. `←/→`, `home/end` and
`ctrl+d` edit the text when there is some, and act on the list when it's empty;
`ctrl+f` is the filter (use `→` to move forward).

Multi-line: `alt+enter`, `ctrl+j`, or `\` at the end then `enter` add a line;
the box grows up to 8 rows, and with several lines `↑/↓` move in the text.
A paste longer than 3 lines or 800 characters shows as `[Pasted text #1 +N
lines]` and is expanded when the agent starts (`ctrl+v` reads the clipboard
the same way). The task goes to `claude --bg` as one argument, so it's capped
at 120 KiB — beyond that, put it in a file and point the agent to it.

| | |
|---|---|
| `enter` | attach (`←` in the session comes back) |
| `tab` | tree ⇄ inbox (everyone waiting for you, longest wait first) |
| `b` | jump to the next agent waiting for you |
| `y` / `Y` | copy suggested reply / session id |
| `o` | show the session's terminal output |
| `i` / `c` / `C` | back to the input; `C` aims the new agent inside the selection (under a coordinator) |
| `s` | stop (conversation kept) |
| `x` | remove from the list (`claude rm`: worktree deleted if safe, conversation kept; refuses over unpushed commits) |
| `J` / `K` | move down / up among siblings |
| `>` / `<` | nest under the agent above (make it the coordinator) / un-nest |
| `m` then `p` / `P` | move: put below / inside the target |
| `n` `r` `d` | new group, rename (alias), remove group |
| `←/→` `space` | fold |
| `/` `esc` | filter (name, question, path, branch) / clear |
| `a` | show finished sessions |

## Limits (v1)

- Replying without attaching isn't possible: the CLI has no "send" command, and
  the peer socket protocol is private. `y` + `enter` + paste is the fast path.
- Interactive sessions are listed but can't be attached from here.
- `~/.claude/jobs` is an internal format and may change between Claude Code versions.
