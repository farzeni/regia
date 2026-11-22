# Getting started

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
   (the regia repo is its own marketplace), and updates it if it was already there.
   The plugin gives every session hooks (so regia knows which session started
   which: that's how agents nest under their coordinator automatically, and so
   agents in a group know about it), the **regia MCP server** and the
   **`regia:organize` skill** (see [Agents that organize regia](./agents-organize)).
   Sessions already running pick it up after a restart or `/reload-plugins`.
2. **Backfills** those links from your existing transcripts, so sessions you
   already have are nested too.

Flags: `--source <url|owner/repo|path>` installs the plugin from elsewhere,
`--no-plugin` and `--no-backfill` skip a step. Running it again is safe.

## Updates

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

## A first look

```bash
regia            # open the UI; ? shows every key
regia --dump     # print the tree and the inbox as plain text (no TTY needed)
```

The screen has three views: `tab` goes agents → inbox → search, `shift+tab` goes back.

- **[agents](./agents)**: every agent, organized as a tree, with the selected
  agent's card, conversation and changes on the right;
- **[inbox](./inbox)**: what is waiting for you today;
- **[search](./search)**: inside every session's conversation.

At the bottom is an always-on input (`i` focuses it): type a task and press
`enter` to [start a new background agent](./starting-agents). `enter` on an
empty input attaches to the selected session; inside it, `←` (or `ctrl+z`)
brings you back to regia in about 80 ms. Sending a task or attaching lets go
of the input, so you land back on the list: `i` → type → `enter`, and `i`
again for the next one.

Every key is listed in [Keys](../reference/keys), and `?` shows them in regia.

## Limits

- You can't reply without attaching: the CLI has no "send" command and the
  session socket protocol is private. `y`, `enter`, paste is the fast path.
- Interactive (foreground) sessions are listed but can't be attached from here.
- `~/.claude/jobs` and transcripts are internal Claude Code formats and may
  change between versions; regia treats every field as optional.
