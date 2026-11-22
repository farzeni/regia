# Code conventions

How code is written in regia. CLAUDE.md links here; agent briefs quote the rules that matter for their task.
Update this file when a rule changes.

## Build and test
- Go 1.26, gofmt. Bubble Tea v1 (bubbletea 1.3), bubbles, lipgloss, glamour (markdown), x/ansi.
- `go build ./... && go vet ./... && go test ./...` must pass. `go test -short ./...` skips the read-only scan of
  your real transcripts.
- Real-data benchmarks are skipped unless an env var names a job:
  `REGIA_BENCH_JOB=<id> go test ./internal/ui -run x -bench Real` (internal/ui/bench_test.go; also
  internal/claude/bench_test.go). `REGIA_BENCH_LINKS=1` measures with OSC 8 links on.

## Packages
- Dependencies point one way: `ui` → `group` → `layout` → `spawns` → `claude`. `gitinfo`, `prinfo`, `attach`,
  `update`, `plugins` are standalone. Never import `ui` (or anything above you) from below.
- Claude Code's data is read only in `internal/claude` (agents listing, job state, transcripts). The UI never
  parses JSONL itself; it asks `claude.Messages`/`claude.Conversation` and friends.
- In `internal/ui`, one concern per file (panel.go, conv.go, steps.go, mouse.go, openers.go…). A new feature gets
  its own file and a `<name>_test.go` next to it.

## Code style
- Small functions, early returns, no interfaces or frameworks "for later". When behaviour varies by kind, a map
  of funcs is enough (see `stepKinds` in internal/ui/steps.go).
- Comments are short, plain English, and say what the user sees and why, from the user's point of view ("your
  messages", "the agent's replies"). A declaration's comment starts with its name:
  `// messageHead is "▸ agent 14:05" (bold for the message that matters).`
- Caches are small package-level structs keyed by whatever invalidates them, with a comment saying why the cache
  exists ("the panel is drawn 8 times a second while an agent works").
- Background work (git, gh, plugins, transcript scans) runs in a `tea.Cmd` and comes back as a message; `View`
  and key handling never block on I/O or external programs.
- Claude Code's files are internal formats: treat every field as optional, and leave out what can't be read
  rather than failing.

## UI
- Colors are `c*` vars and styles `st*` vars in internal/ui/view.go (`cAccent` 141, `cDim` 241, `cBusy` 42,
  `cFailed` 196, `stYou` 183 for your messages, `stSel` background 237 for the selection); a feature's own styles
  sit at the top of its file (e.g. `stStderr` in steps.go). Reuse before adding.
- Width math with the package's helpers (`wrap`, `trunc`, `ansi.StringWidth`), never `len()` on rendered strings.
- Every key is documented twice: the help modal (internal/ui/modal.go) and the docs site (docs/reference/keys.md,
  and the guide page of the feature in docs/guide/).
  Change both in the same commit.
- Mouse capture is on (`tea.WithMouseCellMotion`, run.go); mouse handling lives in mouse.go. Shift+drag selects text.
- Side effects go through package-level `var` funcs so tests can replace them: `spawnAgent`, `copyText`
  (clipboard, OSC 52 fallback), `openThing` (openers), `linkify` (OSC 8).
- Opening files, images, URLs, pagers and diffs always goes through the openers (open.go, openers.go;
  `~/.config/regia/openers.conf`, presets in presets/openers). Commands are split like a shell but never run
  through one.

## Tests
- Drive the model, don't test internals: `m := testModel(t)` (input_test.go: temp XDG dirs, two fake agents,
  140×30), keys with `m.Update(tkey("o"))`, assertions on `ansi.Strip(m.View())` or on model state.
- Transcripts in tests are built with helpers (`giveTranscript` in panel_test.go) into `t.TempDir()`.
- Fixtures from real transcripts are trimmed to the lines needed and anonymized (no paths under a home dir, no
  personal text, no secrets), in a `testdata/` dir next to the test (e.g. internal/claude/testdata/steps.jsonl).
- Replace side effects through their package var and restore it with `defer`.
- Failure messages print the rendered screen: `t.Fatalf("…:\n%s", all)`.

## Commits
- English. Summary: one imperative line describing what the user gets ("Move the inbox cursor to the next item
  after you answer an agent"), not the code change.
- Body: prose (or a few bullets) on what the user sees now and what was wrong before, keys named exactly.
- Feature work happens on a worktree branch; merges into main are `--no-ff` with a "Merge <branch>: <what the
  user gets>" summary.

## Dependencies
- No new modules without asking. Already available: bubbles, lipgloss, glamour, chroma (via glamour), x/ansi,
  termenv, creack/pty, go-runewidth.
