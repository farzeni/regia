# Data and files

regia only reads Claude Code's own data and drives the `claude` CLI; it has no server and no database of its
own.

## Where the data comes from

| What | Source |
|---|---|
| list of sessions | `claude agents --json --all` (run again when a job or session file changes, at most every 2 s and at least every 10 s; the last result is cached so the first frame is instant) |
| question, suggested reply, now-doing, tokens, PRs | `~/.claude/jobs/<id>/state.json` and `timeline.jsonl`, read-only, every 1 s |
| PR status (state, CI, review) | `gh pr view <PR link or branch> --json …` in the agent's directory, in the background: open PRs every 60 s at most, a branch without a PR every 10 min, merged/closed never again (the selected agent, plus finished agents from today or with a PR link, up to 4 new calls per second); skipped when `gh` is missing or not logged in |
| group timeline (`T`) | the agents' `timeline.jsonl` (parsed again only when it changes), `git log` per directory and branch (background, cached 30 s) |
| stuck (⚠) | mtimes of the job files, the transcript and its subagents' transcripts; the transcript's tail, read incrementally |
| history | the session transcript (`~/.claude/projects/…/<session>.jsonl`), read-only |
| attach / stop / start / output / remove | `claude attach`, `claude stop`, `claude --bg`, `claude logs`, `claude rm` |
| plugins | your scripts in `~/.config/regia/plugins/`, in the background: per agent at most every `every` (2 min by default), up to 4 runs per second, backing off when one fails |

`~/.claude/jobs` and transcripts are internal Claude Code formats and may change between versions; regia
treats every field as optional and leaves out what it can't read.

## regia's own files

All in `~/.config/regia/` unless noted.

| File | Holds |
|---|---|
| `layout.json` | your organization: the tree, groups, archive, pins, what you've seen, grants and claims |
| `spawns.jsonl` | who started whom, written by the plugin's hook and backfilled from transcripts |
| `actions.jsonl` | what agents changed in the tree through `regia mcp` (`U`) |
| `history.jsonl` | everything you typed in the input |
| `openers.conf` | what opens files, images, URLs and diffs |
| `plugins/` | your plugin scripts |
| `config.json` | `regia group on/off` |
| `~/.cache/regia/pastes` | images you pasted into the input |

`layout.json` is written under a lock and merged with other writers' changes, so two regia instances and
the agents' tools never overwrite each other.
