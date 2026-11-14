# regia

A fast terminal control room for Claude Code background agents. See at a
glance who is waiting for you, organize agents your way (projects, groups,
coordinators with their agents underneath), and jump in and out of any session
in a keystroke.

## Install

You need **Go 1.26+** and **Claude Code 2.1.292+** (`claude --version`).

```bash
go install github.com/farzeni/regia/cmd/regia@latest   # installs to $(go env GOPATH)/bin
regia setup                                            # once per machine
regia
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`: the Claude Code hook calls
`regia` by name. To build from a clone instead:

```bash
git clone https://github.com/farzeni/regia.git && cd regia
go build -o regia ./cmd/regia && install -m755 regia ~/.local/bin/
regia setup
```

`regia setup` does two things:

1. **Installs the regia Claude Code plugin** with
   `claude plugin install regia --marketplace https://github.com/farzeni/regia.git`
   (this repo is its own marketplace). The plugin adds a small hook so regia
   knows which session started which: that's how agents nest under their
   coordinator automatically. Sessions already running pick it up after a
   restart or `/reload-plugins`.
2. **Backfills** those links from your existing transcripts, so sessions you
   already have are nested too.

Flags: `--source <url|owner/repo|path>` installs the plugin from elsewhere,
`--no-plugin` and `--no-backfill` skip a step. Running it again is safe.

**Update:** `go install github.com/farzeni/regia/cmd/regia@latest` (or `git pull` and
rebuild) and `claude plugin update regia@regia`.
**Uninstall:** `claude plugin uninstall regia@regia`, delete the binary, and
optionally `~/.config/regia/`.

## Usage

```bash
regia            # open the UI; ? shows every key
regia --dump     # print the tree and the inbox as plain text (no TTY needed)
regia setup      # install the plugin and backfill links (see above)
```

The screen has two views, switched with `tab`:

- **tree** — every agent, organized. Each row shows state (● waiting for you,
  ◆ busy, ✓ done, ✗ failed), name and age; folded groups show how many agents
  are inside and how many need you. The right panel shows the selected agent
  and reads bottom-up, like a chat: its state and what it is asking sit at the
  bottom, above them the task and git status, and above those its history
  grouped by your messages, newest last (`pgup`/`pgdn` scroll back).
- **inbox** — only the agents waiting for you, longest wait first.

At the bottom is an always-on input (`i` focuses it): type a task and press `enter` to start a
new background agent next to the selected one (inside it if it's a group, or
with `C` to put it under a coordinator). `enter` on an empty input attaches to
the selected session; inside it, `←` (or `ctrl+z`) brings you back to regia in
about 80 ms.

## How agents are organized

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

## Keys

regia opens on the list (command keys); `i`, `c` or `C` focus the input, and
`esc` on an empty input goes back to the list. In command keys `esc` clears the filter or a pending
move, and with nothing to clear it quits (`esc esc` from an empty input exits;
`ctrl+c` too). A task you start is selected as soon as it shows up in the list,
so a second `enter` attaches to it.

| Key | Action |
|---|---|
| `enter` | attach to the session (`←` inside it comes back) |
| `tab` | tree ⇄ inbox |
| `alt+↑` / `alt+↓` | previous / next group (also while typing) |
| `pgup` / `pgdn` | scroll the panel's history back / forward |
| `b` | jump to the next agent waiting for you |
| `y` / `Y` | copy the suggested reply / the session id |
| `o` | show the session's terminal output |
| `i` / `c` / `C` | type a new task; `C` starts it inside the selection |
| `s` | stop the session (conversation kept) |
| `x` | remove from the list (`claude rm`: worktree deleted if safe, conversation kept; refuses over unpushed commits) |
| `J` / `K` | move down / up among siblings |
| `>` / `<` | nest under the agent above (it becomes the coordinator) / un-nest |
| `m`, then `p` / `P` | move: put below / inside the target |
| `n` / `r` / `d` | new group / rename (group or agent alias) / delete group (its agents move up) |
| `←` `→` `space` | fold / unfold |
| `z` | fold everything / unfold everything |
| `/`, `esc` | filter by name, question, path or branch / clear |
| `a` | show finished sessions |
| `?` | help |

The input takes the usual readline keys (`ctrl+a`/`ctrl+e`, `ctrl+w`,
`alt+←/→`, `ctrl+k`/`ctrl+u`, …). `alt+enter`, `ctrl+j`, or `\` then `enter`
add a line. A paste longer than 3 lines or 800 characters folds to
`[Pasted text #1 +N lines]` and is expanded when the agent starts; `ctrl+v`
pastes from the clipboard the same way. A task is capped at 120 KiB; beyond
that, put it in a file and point the agent to it.

## Where the data comes from

| What | Source |
|---|---|
| list of sessions | `claude agents --json --all` (refreshed every 2 s; the last result is cached so the first frame is instant) |
| question, suggested reply, now-doing, tokens, PRs | `~/.claude/jobs/<id>/state.json` and `timeline.jsonl`, read-only, every 1 s |
| history | the session transcript (`~/.claude/projects/…/<session>.jsonl`), read-only |
| attach / stop / start / output / remove | `claude attach`, `claude stop`, `claude --bg`, `claude logs`, `claude rm` |
| your organization | `~/.config/regia/layout.json` |
| who started whom | `~/.config/regia/spawns.jsonl` |

## Limits

- You can't reply without attaching: the CLI has no "send" command and the
  session socket protocol is private. `y`, `enter`, paste is the fast path.
- Interactive (foreground) sessions are listed but can't be attached from here.
- `~/.claude/jobs` and transcripts are internal Claude Code formats and may
  change between versions; regia treats every field as optional.

## Development

```bash
go test ./...                  # includes a read-only scan of your real transcripts
go test -short ./...           # skip it
go build -o regia ./cmd/regia
```

```
cmd/regia/          the binary: argument parsing, `regia setup`, `regia hook spawn`
internal/claude/    Claude Code data: `claude agents` listing, job state files, transcript history
internal/spawns/    spawn links: the hook handler, transcript backfill, spawns.jsonl
internal/layout/    your organization: tree, automatic placement, moves, layout.json
internal/gitinfo/   git state of an agent's directory, cached
internal/attach/    `claude attach` behind a pty proxy (fast return to regia)
internal/ui/        the terminal interface (bubbletea): model, update, view, panel, input
.claude-plugin/     plugin manifest + marketplace (this repo is its own marketplace)
hooks/hooks.json    the PostToolUse hook the plugin installs
```

Dependencies point one way: `ui` → `layout` → `spawns` → `claude`; `gitinfo`
and `attach` are standalone.
