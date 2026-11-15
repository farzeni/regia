# regia

A fast terminal control room for Claude Code background agents. See at a
glance who is waiting for you, organize agents your way (projects, groups,
coordinators with their agents underneath), and jump in and out of any session
in a keystroke.

## Install

You need **Go 1.26+** and **Claude Code 2.1.292+** (`claude --version`).

```bash
go install github.com/farzeni/regia/cmd/regia@main   # installs to $(go env GOPATH)/bin
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

### Updates

regia keeps itself current: once a day, when you open it, it checks for a newer
version in the background, installs it over the running binary and updates the
plugin; the header then says `⬆ updated to … · restart regia`. Nothing is
interrupted, and the new version runs the next time you start regia.

```bash
regia update             # update now (binary + plugin)
regia update --check     # only say whether there's a newer version
regia update --auto off  # no automatic updates (or REGIA_NO_AUTOUPDATE=1); --auto on brings them back
regia version
```

A binary you built yourself from a checkout is a dev build: it may be ahead of
what's published, so regia never updates it automatically and `regia update`
asks for `--force` before replacing it with the published version.

**Uninstall:** `regia uninstall` removes the plugin and the binary after a y/N;
`--purge` also deletes your layout (`~/.config/regia/`) and the update cache.

## Usage

```bash
regia            # open the UI; ? shows every key
regia --dump     # print the tree and the inbox as plain text (no TTY needed)
regia setup      # install the plugin and backfill links (see above)
regia update     # update now; it also happens on its own once a day
regia uninstall  # remove the plugin and the binary
```

The screen has two views, switched with `tab`:

- **tree** — every agent, organized. Each row shows state (● waiting for you,
  ✓ ready, ◆ busy, ✗ failed; see below), name and age; folded groups show how
  many agents are inside and how many are your turn. The right panel shows the selected agent
  and reads bottom-up, like a chat: its state and what it is asking sit at the
  bottom, above them the task and git status, and above those its history
  grouped by your messages, newest last (`pgup`/`pgdn` scroll back).
- **inbox** — only the agents whose turn it is yours: questions first, then
  results to read, then stale questions; most recent first in each. Each item
  takes two lines: the agent, then the group (and coordinator) it sits under
  and what it is asking.

What the states mean. Claude Code marks a session *done* whenever a turn ends
with a result, which for a conversation is after every reply, not when the
work is over. So regia shows a session that finished a turn you haven't
looked at yet as **✓ ready** (bright green): it stays in the tree and the
inbox and counts in the header until you look at it, by keeping it selected
for 2 seconds or attaching to it. Then it's plain *done*, hidden unless you
press `a` (the one you're reading stays until you move away). A question left
unanswered for over a day is **stale** (grey ●): still listed, but not
counted in "need you". What you've seen is remembered in `layout.json`.

To keep a session in sight whatever its state, **pin** it with `f` (⚑ before
its name): a pinned session stays in the tree after it's done and seen, until
you press `f` again. `f` on a group pins all its agents (or unpins them, when
they're all pinned already). The filter still applies to pinned sessions;
archiving one drops its pin, and pinning an archived one brings it back.

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
| `[` / `]`, `ctrl+↑` / `ctrl+↓`, `alt+↑` / `alt+↓` | previous / next group (the arrows also while typing; some terminals, e.g. Tilix, keep `alt+↑/↓` for themselves) |
| `pgup` / `pgdn` | scroll the panel's history back / forward |
| `b` | jump to the next agent whose turn is yours (waiting or ready) |
| `y` / `Y` | copy the suggested reply / the session id |
| `o` | show the session's terminal output |
| `i` / `c` / `C` | type a new task; `C` starts it inside the selection |
| `s` | stop the session (conversation kept) |
| `x` | remove from the list (`claude rm`: worktree deleted if safe, conversation kept; refuses over unpushed commits) |
| `J` / `K` | move down / up among siblings |
| `>` / `<` | nest under the agent above (it becomes the coordinator) / un-nest |
| `m`, then `p` / `P` | move: `p` puts it into the group under the cursor (at the top), or right after the agent; `P` puts it after the group, or under the agent (as its coordinator) |
| `n` / `r` / `d` | new group / rename (group or agent alias) / delete group (its agents move up) |
| `w` | set the directory new agents in the group start in (on an agent: its group's); see [Where a new agent starts](#where-a-new-agent-starts) |
| `←` `→` `space` | fold / unfold |
| `z` | fold everything / unfold everything |
| `-` / `+` | list narrower / wider (or drag the divider; remembered) |
| `/`, `esc` | filter by name, question, path or branch / clear |
| `a` | hide or show finished sessions you've already seen (shown by default) |
| `f` | pin / unpin the agent (a group: all its agents): pinned ones stay listed when done |
| `e` | archive the agent, or everything in a group except busy agents (asks first); on an archived agent, bring it back |
| `A` | show / hide archived agents (dim) |
| `v` | select mode: `space` marks a row (a group: all of it), `*` marks everything, then `e` archives or `x` removes them all, `m` picks them all up to move (`p` puts them down), `n` makes a new group of them, `f` pins them (unpins, if all pinned); `esc` leaves |
| `?` | help |

The mouse works too: the wheel scrolls the list or the panel (whichever is
under the pointer), a click selects a row, and dragging the divider resizes
the list. Hold `shift` to select text with the mouse as usual.

The input takes the usual readline keys (`ctrl+a`/`ctrl+e`, `ctrl+w`,
`alt+←/→`, `ctrl+k`/`ctrl+u`, …). `alt+enter`, `ctrl+j`, or `\` then `enter`
add a line. A paste longer than 3 lines or 800 characters folds to
`[Pasted text #1 +N lines]` and is expanded when the agent starts; `ctrl+v`
pastes from the clipboard the same way. A task is capped at 120 KiB; beyond
that, put it in a file and point the agent to it.

A word starting with `/` anywhere in the draft lists skills and commands (your
own, the installed plugins' as `plugin:name`, the project's, and the
built-ins); a word starting with `@` lists the files of the directory the agent
will start in (git's files, or a short walk outside a repository). `↑/↓` move,
`tab` or `enter` picks, `esc` closes the list. An image on the clipboard
(`ctrl+v`), files copied in a file manager, or image paths dropped on the
terminal become `[Image #n]`: the image is saved under `~/.cache/regia/pastes`
and the token turns into its path when the agent starts, so the agent reads it.
The clipboard is read with `wl-paste` or `xclip` on Linux and with the built-in
`osascript` on macOS.

### Where a new agent starts

The input box's bottom border shows the directory the task will run in
(`╰── ~/Projects/regia · ctrl+o ─╯`; `ctrl+o` also says why). It follows the
selection; regia walks up the tree from the selected row and takes the first of:

1. an **agent's directory**, unless it is your home directory (an agent
   running in `~` was usually started from a terminal there);
2. a **group's saved directory**, set with `w`;

and, when the walk finds neither, a guess for the nearest group:

3. the directory **most of its agents** work in (a tie goes to the most
   recently active);
4. `~/Projects/<name>` or `~/Git/<name>`, for the group's project or title;

and finally your home directory. A worktree (`…/.claude/worktrees/x`) counts
as its repository, so a new task never starts inside another agent's
worktree. A group made from a selection (`v` … `n`) saves its directory when
all its agents work in the same one.

`ctrl+o` picks a directory for this task only: the list holds where it would
start now, the groups' saved directories, the directories your agents work in
(most recent first) and the folders in `~/Projects` and `~/Git`. Type to
filter, or type a path (`~/…`, `/…`) to browse; `tab` goes into the selected
folder, `enter` picks, `ctrl+s` picks and also saves it as the group's
directory, `esc` cancels. The pick lasts until the task is sent or the draft
is cleared, even if you move the selection to place the task elsewhere. `w`
opens the same picker on the group: `enter` saves, and the first entry,
`(no saved dir)`, forgets it. If the directory no longer exists, regia doesn't
start the task: pick another one.

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
  (one confirmation; refusals are listed, the rest go through).

Actions that ask first (`x`, `s`, `d`, `e` on a group, and `e`/`x` in select
mode) are confirmed by pressing the same key again; any other key cancels.

## Group awareness

Agents can know about the group they're in. Only groups that were made on
purpose count: groups you created by hand, and a coordinator with the agents it
started (or that you moved under it). Automatic project groups don't, so agents
that only share a directory know nothing about each other.

- **`regia context`** (run by an agent): its group, its coordinator, its
  siblings and its own agents with state and last line, then the latest 10
  shared notes of each level above it (`--all` for every note).
- **`regia note "…"`** (run by an agent): one line for the group: a decision, a
  fact or a warning. It goes to the nearest group or coordinator above the
  agent and reaches everything below that point. Notes live in
  `~/.config/regia/notes/` and are kept; removing a group removes its notes.

In regia, the panel of a group (or of a coordinator, above its card) shows its
blackboard. `N` opens it, from the group, the coordinator or any agent in it:
`↑/↓` pick a note, `n` adds one from you, `e` edits the picked note (yours or
an agent's), `x x` deletes it, `esc` closes. An edit reaches the agents like a
new note does. Outside regia, `regia context <id>` prints what an agent would
see (`--all` for every note).

It is built to stay out of the agents' way. The plugin's hook does nothing for
agents outside a group. A grouped agent gets a short introduction once (what
the two commands are for: a shared blackboard, with no advice on how to use it), then
a single line only when someone else wrote a note it hasn't seen or it was
moved, at most once a minute (or when it gets a message). Notes are never
pasted into the conversation: the agent reads them with `regia context` when
it wants to.

To turn it off: `regia group off` for every session (`regia group on` to
undo), `REGIA_GROUP=off` for one session, or `M` in regia to mute one group or
coordinator (its agents get nothing, and it isn't listed to others).

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
cmd/regia/          the binary: argument parsing, `regia setup`, `update`, `uninstall`, `hook spawn`
internal/claude/    Claude Code data: `claude agents` listing, job state files, transcript history
internal/spawns/    spawn links: the hook handler, transcript backfill, spawns.jsonl
internal/layout/    your organization: tree, automatic placement, moves, layout.json
internal/gitinfo/   git state of an agent's directory, cached
internal/attach/    `claude attach` behind a pty proxy (fast return to regia)
internal/update/    self-update: version check on the Go module proxy, `go install` in place
internal/ui/        the terminal interface (bubbletea): model, update, view, panel, input
.claude-plugin/     plugin manifest + marketplace (this repo is its own marketplace)
hooks/hooks.json    the PostToolUse hook the plugin installs
```

Dependencies point one way: `ui` → `layout` → `spawns` → `claude`; `gitinfo`,
`attach` and `update` are standalone.
