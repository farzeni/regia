# The agents view

Every agent, organized as a tree. Each row shows state (● waiting for you,
✓ ready, ◆ busy, ✗ failed; see [What the states mean](#what-the-states-mean)), name and age; folded groups
show how many agents are inside and how many are your turn. How the tree is
built and how to change it is in [Organizing the tree](./organizing).

## The panel

The right panel shows the selected agent in three parts: a card pinned at the
top (task, git status, what it is asking), the conversation in the middle, and
a foot pinned at the bottom with its state, name, directory and tokens, which
never scrolls.

The conversation is your messages (right-aligned, in your tint, in a box) and
the agent's replies in full, its markdown rendered, newest last: the panel
opens at its end, the latest reply right above the foot (`pgup`/`pgdn` scroll
back and forth). The tool calls between two messages are one folded line,
`▸ ⚙ 14 steps · edited 3 files · ran 5 commands · 1 error · 2m10s` (how long
they took), with when the reply that follows came at its right end (`just
now`, `3 minutes ago`, `2 hours ago`, `yesterday`, `Mon 5 Aug`,
`Wed 8 Jan 2025`; your messages say it the same way); the reply comes right
under it. Unfolded, the steps show in a box.

## Reading the conversation

`o` focuses the conversation (the divider lights up):

- `↑`/`↓` move message to message, `shift+↑`/`↓` (`j`/`k`) through every block,
  runs and steps too;
- `enter`/`space` unfold a run into one line per step (`✗` failed) and a step
  into its detail;
- `c` copies the selected block, `r` quotes it (or the text you last dragged
  over) as `> ` lines in the input box, also on the clipboard, with the cursor
  two lines below and the input focused;
- `i` goes to the input, `g`/`G` go to the top / bottom, `←`/`esc` give the
  keys back to the list.

What's unfolded is remembered per agent until regia quits. Each kind of step
reads its own way; on a step `space` unfolds its detail and `enter` does its
action (on steps without one, `enter` unfolds too). What `enter` opens goes
through your [openers](./openers).

| Step | Folded | Unfolded | `enter` |
|---|---|---|---|
| Edit / Write | `✎ path:line +8 −3` (`new` for a created file) | the diff with line numbers, first 20 lines | opens the file at the first changed line |
| Bash | `$ cmd` (`✗` failed, `⏹` interrupted) | the whole command, the last 15 lines of output (stderr tinted) | the whole output in the pager |
| `claude --bg` | `⇢ spawned fb-inbox (working)`, its live state | the command | selects that agent in the list |
| Read / Grep / Glob | `◦ read x.go 1-200`, `◦ grep "p" 12 hits`, `◦ glob "*.go" 8 files` | the file and its lines, or what was found | opens the file (a search: its first hit) |
| Images (screenshots, Read of an image) | `▣ screenshot 1280×720` | type, size | opens the image (decoded to a temp file) |
| Agent / Task | `⇢ subagent: review panel (done)` | its report | its conversation in the pager |
| WebFetch / WebSearch | `◦ fetch <url>`, `◦ search "query"` | the first lines of the result | opens the URL (a search: its first link) |
| TodoWrite | `☐ todos 3/7` | the list then: ✓ done, ▸ doing, ◻ to do | — |
| Thinking | `… thought` | the thought, when the transcript has it | — |
| Other tools | `• Tool input` (MCP tools as `server:tool`) | the input and the head of the result | — |

The steps line and the agent's reply right after it are one item: `enter`
/`space` unfold its steps, `c`/`r` copy / quote the reply. A click selects
(and focuses the conversation); on a steps line or a step it also unfolds /
folds it, a click on the reply's text only selects, and a click on the
selected step opens it. The wheel scrolls, and in focus the cursor stays on
what's shown.

## The changes tab

The agent box's **changes** tab (`D`) lists the files the agent edited or
wrote, newest first, each with the lines it added and removed (`+8 −3`) and
the line it last changed; failed edits don't count. `D` gives the tab the
keys: `j`/`k` move, `enter` (or a click on the selected file) opens the file
at that line, `d` opens the agent's whole diff (from where its branch left
main/master, uncommitted changes included) in your diff tool, `esc` gives
the keys back, `D` again goes back to the agent tab.

## Pull requests

When the agent has a **pull request** (a PR link in its job state, or else a
PR for its branch), the card has a `pr` line: number, draft/open, CI checks
(✓ passing, ✗ failing, … pending) and review (review requested, approved,
changes requested), or **✓ merged** / closed, so "is the work landed?" is
answered at a glance. It's read with `gh` in the background: open PRs at most
once a minute, merged and closed ones never again; without `gh`, or not logged
in, the line is simply not there.

## What the states mean

Claude Code marks a session *done* whenever a turn ends with a result, which
for a conversation is after every reply, not when the work is over. So regia
shows a session that finished a turn you haven't looked at yet as **✓ ready**
(bright green): it stays in the tree, counted as "ready" in the header (not in
"need you", and not in the inbox, which is for questions) until you look at
it, by keeping it selected for 2 seconds or attaching to it. Then it's plain
*done*, hidden unless you press `a` (the one you're reading stays until you
move away). A question from before today (4am) is **stale** (grey ●): still
listed, but not counted in "need you"; done and failed sessions from before
today go grey the same way. What you've seen is remembered in `layout.json`.

## Stuck agents

A busy agent that **looks stuck** gets a dim ⚠ after its name, a `stuck?`
line on its card saying why, and an inbox item after the questions (not
counted in "need you"). Two guesses, checked every second for busy agents
only: nothing was written for 10 minutes (job state, timeline, transcript or
a subagent's transcript: `no activity for 23m`), or the last 3 or more tool
results were the same error from the same tool (`same Bash error 5×: …`).
It's a guess: a long build is quiet too. It only stats files and reads what
was appended to the transcript since the last check.

## Pinned agents

To keep a session in sight whatever its state, **pin** it with `f` (⚑ before
its name): a pinned session stays in the tree after it's done and seen, until
you press `f` again. `f` on a group pins all its agents (or unpins them, when
they're all pinned already). The filter still applies to pinned sessions;
archiving one drops its pin, and pinning an archived one brings it back.

## The mouse

The wheel scrolls the list or the panel's conversation (whichever is under the
pointer), a click selects a row, a tab of the agent box, a file of the changes
tab or a block of the conversation (a second click on the selected one opens
it, or folds / unfolds it), and dragging the divider resizes the list.
Dragging over the panel selects its text (the list stays out of it) and copies
it when you let go, focusing the conversation: `r` right then quotes just that
text in the input; a click, any other key or moving to another agent clears
it. Hold `shift` while dragging to select with the terminal instead, across
the screen.
