# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What regia is

A terminal control room (Go, Bubble Tea) for Claude Code **background agents**: one screen that shows which
agents are waiting for you, lets you organize them (groups, coordinators with the agents they started), read their
conversation, and jump in and out of any session (`claude attach`) in a keystroke. It is used daily by its author
to run many agents in parallel, often as a swarm (a coordinator agent plus its agents), and regia itself is
developed that way: feature work arrives as `worktree-*` branches merged into `main` with `--no-ff`.

regia only **reads** Claude Code's own data and drives the `claude` CLI; it has no server and no database of its own.
It also ships a **Claude Code plugin** (this repo is its own marketplace) whose hooks call `regia hook …`, which
starts `regia mcp` (`mcpServers` in `.claude-plugin/plugin.json`) in every session and carries the `regia:organize` skill (`skills/`).

## Commands

```bash
go build ./... && go vet ./... && go test ./...   # the check every change must pass
go test -short ./...                             # skip the read-only scan of your real transcripts
go test ./internal/ui -run TestConv              # one package / one test (regex)
go build -o regia ./cmd/regia                    # local binary (a "dev build": never self-updates)
regia --dump                                     # tree + inbox as plain text, no TTY
regia --panel <id> / --timeline <id>             # hidden: one agent's panel / a group's timeline as text
REGIA_BENCH_JOB=<job id> go test ./internal/ui -run x -bench Real   # real-data benchmarks (skipped otherwise)
cd docs && pnpm install && pnpm build            # the docs site; fails on a dead link (pnpm dev: live)
```

Install over the user's binary: `go build -o /tmp/regia ./cmd/regia && install -m755 /tmp/regia ~/.local/bin/regia`.
After changing `hooks/hooks.json` or `.claude-plugin/`: `claude plugin marketplace update regia && claude plugin update regia@regia`
(running sessions pick hooks up after a restart or `/reload-plugins`).

## Big picture

**Data in (read-only)**, all parsed in `internal/claude`:
- `claude agents --json --all`: the session list (re-run on file changes, every 2–10 s, last result cached).
- `~/.claude/jobs/<id>/state.json` + `timeline.jsonl`: question, now-doing, tokens, PR links, state changes.
- `~/.claude/projects/…/<session>.jsonl`: transcripts. `claude.Messages` caches by file size and reads only what
  was appended; `claude.Conversation` turns messages + tool calls/results into the panel's structured steps.
- These are internal Claude Code formats: every field is optional; what can't be read is left out.

**regia's own state** in `~/.config/regia/`: `layout.json` (your tree, archive, pins, seen, grants; written under
a lock and merged with other writers' changes, `layout/sync.go`), `spawns.jsonl` (who
started whom, written by the plugin's hook and backfilled from transcripts), `actions.jsonl` (agents' changes),
`plugins/` (your scripts), `openers.conf`.

**Packages** — dependencies point one way: `ui` / `mcp` → `group` → `layout` → `spawns` → `claude`; `gitinfo`,
`prinfo`, `attach`, `update`, `plugins` are standalone.
- `layout`: places each new agent (fork parent → spawner → project group) and flattens the tree into rows.
- `spawns`: the `regia hook spawn` handler (PostToolUse on Bash) and the transcript backfill.
- `group`: group awareness for agents — `regia context` and `regia hook group` (PostToolUse,
  UserPromptSubmit, SessionStart), which must stay quiet and fast in every session; and the tree tools
  (`tools.go`: roles worker / coordinator / orchestrator, scope checks) with their undo log (`actions.go`).
- `mcp`: `regia mcp`, a stdio MCP server per session that exposes the `group` tools; it knows the caller from
  the session's environment (`group.SelfFromEnv`).
- `attach`: `claude attach` behind a pty proxy so `←` returns to regia in ~80 ms.
- `ui`: the TUI, one model (`model.go`), `update.go` (keys, messages), `view.go`; a 1 s tick re-reads job state
  (`refresh` → `rebuild` rows), a 125 ms pulse animates busy agents and redraws the panel. Views: tree, inbox,
  search. The right panel: context box with tabs (agent, changes, plugins) on top, the ask, the conversation
  (`conv.go`: messages, folded runs of tool steps, a focusable block cursor; per-tool rendering and enter
  actions in `steps.go` via `stepKinds`), a status foot.
- Opening things (file at line, image, URL, pager, diff) always goes through the **openers** (`open.go`,
  `openers.go`, presets embedded from `presets/openers/`); commands are never run through a shell.

## Read before changing

- [docs/conventions.md](docs/conventions.md) — code style, comments, UI styles, tests, commits, dependencies.
  Read it before writing code here.
- The docs site, `docs/` (VitePress): user-facing behaviour in `docs/guide/`, every key in
  `docs/reference/keys.md`, commands in `docs/reference/commands.md`. Update the page (and the help modal,
  `internal/ui/modal.go`) whenever a key or behaviour changes; a new page goes in the sidebar
  (`docs/.vitepress/config.mts`). `README.md` stays short: what regia is, install, links, development.
- [docs/plugins.md](docs/plugins.md) — the plugin protocol; keep it in step with `internal/plugins`.

## Rules that are easy to break

- Never block `View` or key handling on I/O or external programs (git, gh, plugins): run them in a `tea.Cmd`.
  The panel redraws 8 times a second while an agent works; measure render cost on a large real transcript when
  you touch the conversation.
- Never write to `~/.claude` (jobs, transcripts) and don't run regia against the user's live session in tests:
  tests drive the model (`testModel(t)` with temp XDG dirs).
- `regia hook …` runs inside every Claude Code session: no output unless needed, no slow work, never fail the hook.
