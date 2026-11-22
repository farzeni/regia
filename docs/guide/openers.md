# Openers

Enter on a step in the conversation (or in the changes tab) opens it in your
tools: a file at its line in your editor, an image in your viewer, a URL in
your browser, long output in your pager, the agent's diff in git. What runs
is set in `~/.config/regia/openers.conf` (no file: the `default` preset).
Changes take effect when regia starts.

```ini
preset = vscode               # start from a preset (regia openers list)
image  = term: chafa {path}   # then change any kind you want
url    = firefox --new-tab {url}
osc8   = auto                 # clickable links: on, off, auto
```

- Keys: `preset`, `file`, `image`, `url`, `pager`, `diff`, `link`, `osc8`.
  Your lines win over the preset's, and the preset's over `default`.
- A value is a command. Placeholders: `{path}` (absolute; relative paths are
  in the agent's worktree), `{line}` (1 when unknown), `{url}`, `{dir}` (the
  agent's worktree), `{base}` (the commit the agent's changes start from; an
  argument that is only `{base}` is left out when there is none), `{host}`.
- It is split like a shell splits it (`'…'`, `"…"`, `\ `) but **never run
  through a shell**: a path from a transcript is always one argument, so
  pipes, `;` and `$(…)` do nothing. To use them, point to a script.
- `term:` before the command runs it in this terminal: regia steps aside
  until it quits (pagers, `nvim`, `chafa`). Without it the program starts on
  its own, detached (GUI editors, `xdg-open`, `tmux new-window`).
- `pager` gets the text on its stdin; `$PAGER` is used unless you set
  `pager`. `diff` by default is `term: git -C {dir} diff {base}`, through
  your git pager (delta works); new files the agent hasn't added to git
  aren't in it.
- `link` is where file links in the conversation point (below).

```bash
regia openers list                 # the presets
regia openers show                 # what's in effect, where each line comes from, mistakes
regia openers test file main.go:12 # open it now (also: image <p>, url <u>, pager, diff [dir])
```

A mistake in the file is flashed when regia starts and regia uses the
defaults; `regia openers show` says which line.

## Presets

| preset | opens files with | links go to |
|---|---|---|
| `default` | `xdg-open` (`open` on macOS) | `file://` |
| `vscode` | `code <agent folder> -g path:line`: the window of the agent's worktree | `vscode://file…` |
| `cursor` | `cursor <agent folder> -g path:line`: the window of the agent's worktree | `cursor://file…` |
| `zed` | `zed path:line` | `zed://file…` |
| `idea` | `idea --line N path` (JetBrains) | `idea://open?file=…` |
| `nvim-tmux` | `nvim +N path` in a new tmux window | `file://` |

## Links in the conversation (OSC 8)

With `osc8` on, URLs and file paths in the agent's replies (`/abs/x.go:12`,
`internal/ui/open.go`, `x.go:7`) are links: ctrl+click (a plain click in
some terminals) opens URLs in the browser and files where the preset's `link`
points. regia acts on a click when the button comes up, so a terminal that
opens the link then (Ghostty) gets it before regia selects or moves anything.
`auto` turns links on when the terminal says it supports them:

| terminal | links |
|---|---|
| Tilix, GNOME Terminal, Ptyxis (VTE ≥ 0.50) | yes, ctrl+click |
| Ghostty, Kitty, WezTerm | yes (Ghostty: ctrl+click, cmd+click on macOS; Kitty: plain click, asks first) |
| iTerm2 | yes, cmd+click |
| VS Code terminal, Windows Terminal | yes |
| xterm, the Linux console, others | off (they'd ignore them anyway) |
| inside tmux | off in `auto`; with tmux ≥ 3.4 add `set -as terminal-features ",*:hyperlinks"` to `~/.tmux.conf` and set `osc8 = on` |

## Write a preset and share it

A preset is a file in
[presets/openers](https://github.com/farzeni/regia/tree/main/presets/openers):
a first line `# what it is`, then any keys but `preset`. Try yours as your own
`openers.conf`, then send a PR with the file. You can ask Claude for it in
the regia repo:

> Write a regia opener preset `presets/openers/<name>.conf` for <my editor /
> viewer / terminal>. Read docs/guide/openers.md and two presets in that
> folder. Find the command that opens a file at a line and, if it has one, its
> URL scheme for `link`. Keep it to the keys that differ from default, start
> with a `# …` line, then run `go test ./internal/ui -run Preset` and
> `regia openers test file README.md:10` with it as my openers.conf.
