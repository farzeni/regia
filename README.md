# regia

A fast terminal control room for Claude Code background agents: see who needs
you, organize agents your way (groups, coordinators with their agents, manual
order), and jump in and out of sessions instantly.

```
go build -o regia . && install -m755 regia ~/.local/bin/
regia            # ? shows all keys
regia --dump     # plain-text tree + inbox (no TTY needed)
```

## How it works

| Need | Source |
|---|---|
| list of sessions | `claude agents --json --all` (supported CLI, ~0.2s, async every 2s; last result cached so the first frame is instant) |
| question, suggested reply, now-doing, tokens, PRs, timeline | `~/.claude/jobs/<id>/state.json` and `timeline.jsonl`, read-only, every 1s. Internal format: every field is optional |
| jump in | `claude attach <id>` runs in the terminal; `←` inside the session returns to regia |
| stop / new agent / terminal output | `claude stop`, `claude --bg "<prompt>"`, `claude logs` |
| your organization | `~/.config/regia/layout.json` (groups, parents, order, aliases, folds) |

New agents are placed automatically: under the session they were forked from,
otherwise in a group named after their project (`~/Projects/<name>`, worktrees
fold into their repo). After that the place is yours to change.

## Keys

The bottom line is an always-on input, focused at start (like `claude agents`):
type a task and press `enter` to start it next to the selected agent (inside
it if it's a group). `↑/↓` move the selection while you type, `enter` on an
empty input attaches, `esc` clears the text and, on an empty input, switches
to the command keys below; `i`, `c` or `C` (aim inside) go back to typing.

| | |
|---|---|
| `enter` | attach (`←` in the session comes back) |
| `tab` | tree ⇄ inbox (everyone waiting for you, longest wait first) |
| `b` | jump to the next agent waiting for you |
| `y` / `Y` | copy suggested reply / session id |
| `o` | show the session's terminal output |
| `i` / `c` / `C` | back to the input; `C` aims the new agent inside the selection (under a coordinator) |
| `s` | stop (conversation kept) |
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
