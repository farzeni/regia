# Search

Search looks inside every session's conversation, not just names: what you
type is matched against your messages and the agents' replies in every listed
session (archived and finished ones too). The list shows each session with a
match, newest first, followed by its matching messages (who wrote it, when,
and the line around the match, highlighted); the right panel shows the
selected message in context with the messages around it.

## What you can type

- Several words must all appear in the same message, in any case.
- `"quoted words"` match as a phrase.
- `/a regex/` matches as a (case-insensitive) Go regular expression, e.g.
  `deploy /retr(y|ies)/`. A closing `/` needs a space or the end after it, so
  a path like `/home/me` stays a word; a pattern that doesn't compile says why.

`ctrl+r` shows a cheat sheet of the regex syntax worth knowing (classes,
repeats, `\b` word boundaries, anchors, `(?-i)` to match case, ready-made
patterns for ids, URLs, `file.go:12`); `esc` closes it and keeps your query.
`ctrl+t` also searches tool calls and their output.

## Sessions or the whole history

`ctrl+g` picks what to search, like the model picker in the input:

- **sessions** (the default): what `claude agents --all` lists;
- **history**: every conversation on disk (`~/.claude/projects`), sessions you
  removed with `x` and old ones too. Those show dimmed, named by their title
  (or the first thing you wrote), and `enter` on one resumes it here
  (`claude --resume`, in the directory it ran in) instead of attaching. The
  first history search reads every transcript (about a second for a few
  hundred MB); after that it's instant too.

A [plugin](./plugins) that lists `"removed"` in its `states` runs for the
removed session you select there, and its lines show under the session's name:
for example where its handoff file is, so you read that instead of resuming an
old, expensive session. `ctrl+o` opens its links and actions.

## Keys

There is no new-task box here: keys type into the search, a one-line input
with the usual editing keys (`←/→`, `home/end`, `alt+←/→`, `ctrl+w`,
`ctrl+u`, `delete`…).

| Key | Action |
|---|---|
| `↑/↓` (`ctrl+n/p`, `ctrl+j/k`) | move while you type |
| `ctrl+↑/↓` | jump between sessions |
| `ctrl+home/end` | first or last match |
| `shift+←/→` | fold a session's matches |
| `enter` | attach (a history session: resume it) |
| `ctrl+o` | the session's plugin links and actions (the `!` menu) |
| `pgup/pgdn` | scroll the panel |
| `esc` | clear the search; again, back to the tree on that session |

The transcripts are read once (a few hundred ms) and then only what was
appended, so searching is instant after that.
