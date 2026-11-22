# regia

A fast terminal control room for Claude Code background agents. See at a
glance who is waiting for you, organize agents your way (projects, groups,
coordinators with their agents underneath), and jump in and out of any session
in a keystroke.

Docs: the site is built from [`docs/`](docs/) (not published yet; until then,
read them there or run them locally, see Development).

## Install

You need **Go 1.26+** and **Claude Code 2.1.292+** (`claude --version`).

```bash
go install github.com/farzeni/regia/cmd/regia@main   # installs to $(go env GOPATH)/bin
regia setup                                            # once per machine: the plugin, and links from your transcripts
regia                                                  # ? shows every key
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`: the Claude Code hook calls
`regia` by name. Then follow [Getting started](docs/guide/getting-started.md).

## Docs

- Guide: [getting started](docs/guide/getting-started.md),
  [the agents view](docs/guide/agents.md), [inbox](docs/guide/inbox.md),
  [search](docs/guide/search.md), [starting agents](docs/guide/starting-agents.md),
  [organizing the tree](docs/guide/organizing.md),
  [group awareness](docs/guide/group-awareness.md),
  [agents that organize regia](docs/guide/agents-organize.md),
  [openers](docs/guide/openers.md), [plugins](docs/guide/plugins.md)
- Reference: [keys](docs/reference/keys.md), [commands](docs/reference/commands.md),
  [plugin protocol](docs/plugins.md), [data and files](docs/reference/data.md)
- Design: [agents that organize regia](docs/design/agents-mcp.md)

## Development

```bash
go build ./... && go vet ./... && go test ./...   # every change must pass
go test -short ./...                             # skip the read-only scan of your real transcripts
go build -o regia ./cmd/regia                    # a dev build: never updates itself
cd docs && pnpm install && pnpm dev              # the docs site, live (pnpm build fails on a dead link)
```

```
cmd/regia/          the binary: argument parsing, `regia setup`, `update`, `uninstall`, `hook spawn`
internal/claude/    Claude Code data: `claude agents` listing, job state files, transcript history
internal/spawns/    spawn links: the hook handler, transcript backfill, spawns.jsonl
internal/layout/    your organization: tree, automatic placement, moves, layout.json
internal/group/     group awareness (`regia context`, `regia hook group`); the tree tools, roles and undo log
internal/mcp/       `regia mcp`: the MCP server the plugin starts per session
internal/plugins/   your plugin scripts: run per agent in the background, cached
internal/gitinfo/   git state of an agent's directory, cached
internal/prinfo/    PR status of an agent (state, CI, review) via `gh`, cached
internal/attach/    `claude attach` behind a pty proxy (fast return to regia)
internal/update/    self-update: version check on the Go module proxy, `go install` in place
internal/ui/        the terminal interface (bubbletea): model, update, view, panel, input
.claude-plugin/     plugin manifest (with the MCP server it starts, regia mcp) + marketplace (this repo is its own marketplace)
hooks/hooks.json    the hooks the plugin installs (spawn links, group awareness)
skills/organize/    the regia:organize skill
presets/openers/    opener presets, embedded in the binary
docs/               the docs site (VitePress): guide, reference, plugin protocol, design; code conventions
```

Dependencies point one way: `ui` / `mcp` → `group` → `layout` → `spawns` → `claude`;
`gitinfo`, `prinfo`, `attach`, `update` and `plugins` are standalone.
Before contributing, read [docs/conventions.md](docs/conventions.md);
[CLAUDE.md](CLAUDE.md) is the short version for Claude Code.
