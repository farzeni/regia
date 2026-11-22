# Starting agents

At the bottom is an always-on input (`i` focuses it): type a task and press
`enter` to start a new background agent next to the selected one (inside it if
it's a group, or with `C` to put it under a coordinator). regia runs
`claude --bg` for it. A task you start is selected as soon as it shows up in
the list, so a second `enter` attaches to it.

## Typing a task

The input takes the usual readline keys (`ctrl+a`/`ctrl+e`, `ctrl+w`,
`alt+←/→`, `ctrl+k`/`ctrl+u`, …), and so do names edited in place.
`alt+enter`, `ctrl+j`, or `\` then `enter` add a line. A paste longer than 3
lines or 800 characters folds to `[Pasted text #1 +N lines]` and is expanded
when the agent starts; `ctrl+v` pastes from the clipboard the same way. A task
is capped at 120 KiB; beyond that, put it in a file and point the agent to it.

A word starting with `/` anywhere in the draft lists skills and commands (your
own, the installed plugins' as `plugin:name`, the project's, and the
built-ins); a word starting with `@` lists the files of the directory the agent
will start in (git's files, or a short walk outside a repository). `↑/↓` move,
`tab` or `enter` picks, `esc` closes the list.

An image on the clipboard (`ctrl+v`), files copied in a file manager, or image
paths dropped on the terminal become `[Image #n]`: the image is saved under
`~/.cache/regia/pastes` and the token turns into its path when the agent
starts, so the agent reads it. The clipboard is read with `wl-paste` or
`xclip` on Linux and with the built-in `osascript` on macOS.

## Nothing you type is lost

`pgup` in the input brings back earlier messages (newest first, with their
pasted text and images); `pgdown` goes forward again and, past the newest,
back to what you were typing. Edits you make while moving through them are
kept until you send. What you send, what `esc` or `ctrl+c` clears, and what is
in the input when regia quits all go into the history
(`~/.config/regia/history.jsonl`, shared by every regia). The draft is also
written to disk every second, so if regia is killed, the next one puts it back
in the input.

## Where a new agent starts

The input box's bottom border shows the directory the task will run in
(`╰── ~/Projects/regia · ctrl+o ─╯`; `ctrl+o` also says why). It follows the
selection; regia walks up the tree from the selected row and takes the first of:

1. an **agent's directory**, unless it is your home directory (an agent
   running in `~` was usually started from a terminal there);
2. a **group's saved directory**, set with `w`;

and, when the walk finds neither, a guess for the nearest group:

3. the directory **most of its agents** work in (a tie goes to the most
   recently active);
4. `~/Projects/<name>` or `~/Git/<name>`, for the group's project or title;

and finally your home directory. A worktree (`…/.claude/worktrees/x`) counts
as its repository, so a new task never starts inside another agent's
worktree. A group made from a selection (`v` … `n`) saves its directory when
all its agents work in the same one.

`ctrl+o` picks a directory for this task only: the list holds where it would
start now, the groups' saved directories, the directories your agents work in
(most recent first) and the folders in `~/Projects` and `~/Git`. Type to
filter, or type a path (`~/…`, `/…`) to browse; `tab` goes into the selected
folder, `enter` picks, `ctrl+s` picks and also saves it as the group's
directory, `esc` cancels. The pick lasts until the task is sent or the draft
is cleared, even if you move the selection to place the task elsewhere. `w`
opens the same picker on the group: `enter` saves, and the first entry,
`(no saved dir)`, forgets it. If the directory no longer exists, regia doesn't
start the task: pick another one.

## Which model a new agent runs

Left of the directory, the border shows the model
(`╰── default · ctrl+g ── ~/Projects/regia · ctrl+o ─╯`). `default` lets
claude choose; `ctrl+g` picks one for this task: `opus`, `sonnet`, `haiku`,
`fable`, or type a full model id (`claude-opus-5-5`) and `enter`. regia then
starts the agent with `claude --bg --model <model>`. Like the directory, the
pick lasts until the task is sent or the draft is cleared.
