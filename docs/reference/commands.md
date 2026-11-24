# Commands

```bash
regia            # open the UI; ? shows every key
regia --dump     # print the tree and the inbox as plain text (no TTY needed)
regia setup      # install the Claude Code plugin and backfill spawn links
regia update     # install the latest version and update the plugin (also daily, automatically)
regia version    # print this build's version
regia uninstall  # remove the plugin and the binary (--purge: also your layout)
regia plugin     # list | new | input | run: your own scripts on the agent panel
regia openers    # list | show | test: what opens files, images, URLs and diffs
regia swap       # <old> <new> --reason … [--note …]: a fresh session takes an old one's place
```

Group awareness, for agents in a hand-made group or under a coordinator:

```bash
regia context    # run by an agent: its group, coordinator and siblings with what they are doing
regia mcp        # the MCP server the plugin starts in every session: agents read and organize the tree
regia group      # on | off | status: turn it on or off for every session (REGIA_GROUP=off: one session)
```

## setup

Installs the regia plugin (`claude plugin install regia --marketplace https://github.com/farzeni/regia.git`),
or updates it, and backfills spawn links from your existing transcripts. See
[Getting started](../guide/getting-started#install).

| Flag | Does |
|---|---|
| `--source <url\|owner/repo\|path>` | install the plugin from elsewhere |
| `--no-plugin` | skip the plugin |
| `--no-backfill` | skip the backfill |

Running it again is safe.

## update

| Command | Does |
|---|---|
| `regia update` | update now (binary + plugin) |
| `regia update --check` | only say whether there's a newer version |
| `regia update --auto off` / `on` | turn the daily automatic update off / on (`REGIA_NO_AUTOUPDATE=1` also turns it off) |
| `regia update --force` | replace a dev build (one you built from a checkout) with the published version |

## uninstall

Removes the plugin and the binary after a y/N. `--purge` also deletes your layout (`~/.config/regia/`) and
the update cache.

## plugin

```bash
regia plugin new jira                # a working skeleton to start from
regia plugin run jira <agent-id>     # run it once and check its output (or a group id, g:…)
regia plugin list
```

See [Plugins](../guide/plugins) and the [plugin protocol](../plugins).

## openers

```bash
regia openers list                 # the presets
regia openers show                 # what's in effect, where each line comes from, mistakes
regia openers test file main.go:12 # open it now (also: image <p>, url <u>, pager, diff [dir])
```

See [Openers](../guide/openers).

## swap

```bash
regia swap <old-id> <new-id> --reason "context stale" [--note epics/E1/journal.md]
```

The new session, started beforehand (`claude --bg`), takes the old one's name, place in the tree, pin and
the agents under it; the old one is removed with `claude rm` (if that refuses, it stays archived under the
new one with the reason). The note is kept on the row with the whole chain of swaps. Plugins with
`"gates": ["swap"]` may refuse. It runs as you (no scope), is logged in `U` and can't be undone. Meant for
scripts that rotate sessions; agents use the `swap` tool. See
[Swapping a session](../guide/agents-organize#swapping-a-session-for-a-fresh-one).

## context, mcp, group

`regia context` prints what an agent sees of its group; `regia context <id>` prints it for another agent.
`regia mcp` is started by the plugin in every session; you don't run it yourself. `regia group off` turns
group awareness and the MCP tools off for every session, `regia group on` brings them back. See
[Group awareness](../guide/group-awareness) and [Agents that organize regia](../guide/agents-organize).

## Used by the plugin

`regia hook spawn`, `regia hook group` and `regia hook stop` are called by the plugin's hooks in every
session. They print nothing unless needed, take a few milliseconds and never fail the hook. `regia hook stop`
(the `Stop` hook) starts `regia retire <id>` in the background when the session swapped itself out: it
removes the session once its turn has ended.
