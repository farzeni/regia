# Keys

regia opens on the list (command keys); `i`, `c` or `C` focus the input, and
`esc` on an empty input goes back to the list. In command keys `esc` clears the
filter or a pending move, and with nothing to clear it quits (`esc esc` from an
empty input exits; `ctrl+c` too). `enter` that sends a task, and attaching to a
session, also leave the input; coming back from a session lands on the list. A
task you start is selected as soon as it shows up in the list, so a second
`enter` attaches to it.

`?` shows the same keys in regia.

## The agent

| Key | Action |
|---|---|
| `enter` | attach to the session (`←` inside it comes back); your plugins' [attach gates](../plugins#gates) are asked first |
| `y` / `Y` | copy the suggested reply / the session id |
| `!` | plugin actions and links for the agent, or for the group (see [Plugins](../guide/plugins)) |
| `,` / `.` | previous / next tab of the agent box: `agent`, `changes`, then one per plugin |
| `D` | the agent box's `changes` tab, with the keys: `j`/`k` move between the files the agent touched, `enter` opens one at the line it last changed, `d` opens the agent's whole diff against where it branched off main/master in your diff tool, `esc` back, `D` again: the agent tab |
| `s` | stop the session (conversation kept) |
| `x` | remove from the list (`claude rm`: worktree deleted if safe, conversation kept; refuses over unpushed commits, or when a plugin's remove gate refuses) |
| `e` | archive the agent, or everything in a group except busy agents (asks first); on an archived agent, bring it back |
| `A` | show / hide archived agents (dim) |
| `a` | hide or show finished sessions you've already seen (shown by default) |
| `f` | pin / unpin the agent (a group: all its agents): pinned ones stay listed when done |
| `v` | select mode: `space` marks a row (a group: all of it), `*` marks everything, then `e` archives or `x` removes them all, `m` picks them all up to move (`p` puts them down), `n` makes a new group of them, `f` pins them (unpins, if all pinned); `esc` leaves |

## Organize

| Key | Action |
|---|---|
| `J` / `K` (`shift+↓` / `shift+↑`) | move down / up among siblings |
| `>` / `<` | nest under the agent above (it becomes the coordinator) / un-nest |
| `m`, then `p` / `P` | move: `p` puts it into the group under the cursor (at the top), or right after the agent; `P` puts it after the group, or under the agent (as its coordinator) |
| `n` / `r` / `d` | new group / rename (group or agent alias) / delete group (its agents move up); the name is typed right in the row (`enter` saves, `esc` cancels) |
| `M` | mute group awareness for a group or coordinator: its agents get nothing, it isn't listed to others, and claims can't reach it (see [Group awareness](../guide/group-awareness)) |
| `O` | let the agent organize part of the tree through regia's MCP tools: its group, then the whole tree, then nothing (marked ✦); on an agent's own claim, takes it back (see [Agents that organize regia](../guide/agents-organize)) |
| `U` | what agents changed in the tree, newest first: `j`/`k` move, `enter` undoes one, `esc` closes |
| `T` | a group's or coordinator's panel shows its timeline / back (on an agent: its group's) |
| `w` | set the directory new agents in the group start in (on an agent: its group's); see [Where a new agent starts](../guide/starting-agents#where-a-new-agent-starts) |

## Navigate

| Key | Action |
|---|---|
| `j`/`k`, `↑`/`↓` | move |
| `[` / `]`, `ctrl+↑` / `ctrl+↓`, `alt+↑` / `alt+↓` | previous / next group (the arrows also while typing; some terminals, e.g. Tilix, keep `alt+↑/↓` for themselves) |
| `g` / `G` | top / bottom |
| `ctrl+d` / `ctrl+u` | half a page down / up |
| `pgup` / `pgdn` | scroll the panel's conversation back / forward (in the input: earlier / later messages you typed) |
| `b` | jump to the next agent whose turn is yours (waiting on a question) |
| `tab` / `shift+tab` | agents → inbox → search / back |
| `Z` | snooze a question (see [Inbox](../guide/inbox)) |
| `/`, `esc` | filter by name, question, path or branch / clear; while typing, `↑/↓` (`ctrl+n/p`, `ctrl+j/k`) move among the matches and `enter` opens the highlighted one (the filter stays); `ctrl+w` drops a word |
| `←` `→` `space` (`h` / `l`) | fold / unfold |
| `z` | fold everything / unfold everything |
| `-` / `+` | list narrower / wider (or drag the divider; remembered) |
| `R` | refresh now |
| `?` | help |

## The conversation

`o`, or `→` on an agent (one with folded agents under it unfolds first), focuses the conversation. See
[Reading the conversation](../guide/agents#reading-the-conversation).

| Key | Action |
|---|---|
| `↑`/`↓` | message to message, over the steps |
| `shift+↑`/`↓` (`j`/`k`) | every block, runs and steps too |
| `ctrl+↑`/`↓` (`alt+↑`/`↓`, `[`/`]`) | your messages |
| `ctrl+d`/`u` | half a screen |
| `enter`/`space` | fold / unfold (or open a step) |
| `c` | copy the block |
| `r` | quote the block, or the text you dragged over, in the input (and copy it) |
| `i` | to the input |
| `alt+enter` | attach to its session |
| `g`/`G` | top / bottom |
| `←`/`esc` | back to the list |

## Search

| Key | Action |
|---|---|
| `ctrl+r` | regex cheat sheet for `/patterns/` (`esc` closes it) |
| `ctrl+t` | also search tool calls and their output |
| `ctrl+g` | search the listed sessions or the whole history (removed sessions too; `enter` resumes one) |
| `ctrl+o` | the selected session's plugin links and actions (the `!` menu; letters type into the query here), e.g. a removed session's handoff |

The rest is in [Search](../guide/search#keys).

## The input

| Key | Action |
|---|---|
| `i` / `c` / `C` | type a new task; `C` starts it inside the selection |
| `enter` | start the task at the selection (on an empty input: attach) |
| `alt+enter`, `ctrl+j`, `\` then `enter` | add a line |
| `/name`, `@path` | skill or command / file (`tab` or `enter` picks) |
| `ctrl+v` | paste text, an image or files |
| `ctrl+o` | pick the task's directory |
| `ctrl+g` | pick the task's model |
| `pgup` / `pgdn` | earlier / later messages you typed |

The usual readline keys work too (`ctrl+a`/`ctrl+e`, `ctrl+w`, `alt+←/→`, `ctrl+k`/`ctrl+u`, …). See
[Starting agents](../guide/starting-agents).

## Confirmations

Actions that ask first (`x`, `s`, `d`, `e` on a group, and `e`/`x` in select mode) are confirmed by pressing
the same key again; any other key cancels.
