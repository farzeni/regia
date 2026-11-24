# Writing regia plugins

A regia plugin is a script that regia runs for each agent. The script gets
the agent's details as JSON on stdin and prints JSON on stdout. regia shows
what it prints in four places:

- **fields**: lines in the plugin's own tab of the agent's box on the panel
  (`,` and `.` switch tabs; `agent` is regia's own context):

  ```
  ╭─ agent │ gh-pr ✗ │ jira ──────────── ! actions ─╮
  │ ticket   PROJ-482 · In Review                     │
  │ sprint   Sprint 42 · 3 days left                  │
  ╰───────────────────────────────────────────────────╯
  ```
- **badge**: one or two characters on the agent's row in the list, e.g. `J`
- **attention**: a reason to put the agent in the inbox even though it isn't
  waiting on you, e.g. "PR open but ticket still In Progress"
- **actions**: entries in the `!` menu on the agent's panel, e.g. "move ticket
  to In Review". Picking one runs the script again with that action.

A plugin can also be asked before a session is removed or swapped for a
fresh one, and refuse, or before you attach to one, and send you to a fresh
session instead (see [Gates](#gates)). It can run for sessions already removed, found in search's
whole history (`"removed"` in [`states`](#plugin-json)), and for
[groups](#groups) instead of agents. An action (or a refresh, once) can
[start an agent](#starting-an-agent).

Plugins are for checks that depend on your own setup: the tracker you use, CI
workflows, review channels, deploy dashboards. regia handles scheduling,
caching, timeouts and display. The plugin finds the facts and decides what
needs your attention.

This document is the full contract. If you are an agent asked to write a
plugin, read it to the end and use the checklist at the bottom.

A complete working plugin to read before writing your own:
[`examples/plugins/gh-pr`](https://github.com/farzeni/regia/blob/main/examples/plugins/gh-pr/check.py). It shows the
agent's GitHub PR (state, CI progress, reviews, comments) with a badge,
attention and three actions, in about 200 lines of Python using only the
standard library and `gh`.

---

## Quick start

```bash
regia plugin new jira                 # creates ~/.config/regia/plugins/jira/{plugin.json,check.sh}
regia --dump                          # list agents and their ids
regia plugin input jira 3f2a9c1e      # the exact JSON your script will get for that agent
regia plugin run jira 3f2a9c1e        # run it once, see what regia makes of the output
regia plugin run jira 3f2a9c1e --action move-review
regia plugin list                     # what's installed, and any broken manifests
```

A running regia picks up new or edited plugins within about 5 seconds, so you
don't need to restart it. Editing the script takes effect at the next refresh
(`every`). Editing `plugin.json` drops the cached results, so the plugin runs
again right away.

---

## Layout

```
~/.config/regia/plugins/          # $XDG_CONFIG_HOME/regia/plugins; REGIA_PLUGINS_DIR overrides
  jira/
    plugin.json                   # required
    check.sh                      # whatever plugin.json's "command" runs
    lib/…                         # anything else the plugin needs
  ci/
    plugin.json
    check.py
```

The directory name is the plugin's name. Directories without a `plugin.json`,
and directories whose names start with `.`, are ignored.

---

## plugin.json

```json
{
  "command": ["./check.sh"],
  "label": "jira",
  "dirs": ["~/work"],
  "states": ["busy", "blocked", "done"],
  "interactive": false,
  "every": "2m",
  "timeout": "10s",
  "gates": [],
  "targets": ["agent"],
  "disabled": false
}
```

| key | default | meaning |
|---|---|---|
| `command` | required | The program and its arguments, as an argv array (not run through a shell). The command runs in the agent's directory, so paths are resolved for you: the program, if it contains a `/`, is relative to the plugin directory, and a bare name is looked up in the plugin directory first, then on `PATH`. An argument that names a file in the plugin directory (`check.py`, `lib/run.py`) becomes its absolute path; other arguments pass unchanged. So `["./check.sh"]`, `["python3", "check.py"]` and `["/usr/local/bin/x"]` all work. |
| `label` | the directory name | The label for fields that don't set their own. The tab, the `!` menu and the inbox always use the directory name. |
| `dirs` | every agent | Run only for agents whose working directory is one of these or inside one. `~` is expanded. Glob characters are allowed and are matched against the directory and each of its parents, so `"/srv/*/repo"` works. A trailing `/**` means the same as the directory itself. Use this to keep work plugins off personal projects. |
| `states` | every state | Run only for agents in these states: `blocked` (waiting on you), `busy`, `idle`, `done`, `failed`, `ready`, `removed`. `ready` and `done` match each other: `ready` is regia's name for a finished agent you haven't looked at yet. `removed` is a session no longer listed, selected in search's whole history (`ctrl+g`); only plugins that name it run for those, even when `states` is otherwise empty. |
| `interactive` | `false` | Also run for interactive sessions, i.e. `claude` running in a terminal rather than as a background job. They have no job state, so `branch`, `intent` and `links` are mostly empty for them. |
| `every` | `"2m"` | How long a result is kept before the plugin runs again for that agent. Minimum `10s`. Output can override it (see `every` below). |
| `timeout` | `"10s"` | How long one run may take. After that the process is killed and the run counts as a failure. |
| `gates` | none | What regia asks the plugin before doing it, so it may refuse: `"remove"`, `"swap"`, `"attach"`. See [Gates](#gates). |
| `targets` | `["agent"]` | What the plugin runs for: `"agent"` (each agent), `"group"` (each group, see [Groups](#groups)), or both. |
| `disabled` | `false` | Turns the plugin off without deleting it. |

Unknown keys make the manifest invalid, so a typo like `"comand"` is caught.
`regia plugin list` shows the error, and the plugin doesn't run until it's fixed.

---

## When it runs

- **The selected agent:** whenever a refresh is due, while you look at it.
- **Other agents in the list:** in the background, a few runs per second
  across all plugins. This is how badges and the inbox stay current for agents
  you aren't looking at. Archived agents, and agents that finished before
  today (unless pinned), run only when you select them.
- **Actions:** when you pick one in the `!` menu.
- **Removed sessions:** only plugins with `removed` in `states`, when you
  select one in search's whole history.
- **Groups:** for plugins with `"group"` in `targets`, the same way, per group
  row: the selected group while you look at it, the others in the background.
- **Gates:** before a removal, a swap or an attach, for plugins that list it in `gates`.

Each run is one process for one agent, from start to exit. Nothing stays
running between runs. Results are cached in memory per plugin and agent, so a
restart of regia runs everything again.

**Failures:** a non-zero exit, a timeout, or stdout that isn't JSON. The
plugin's tab then shows a dim line at its bottom with the last line of
stderr, e.g. `⚠ exit status 1: not logged in`, and keeps showing the last
good fields above it. A plugin that keeps failing is retried less and less often,
doubling the interval each time up to 30 minutes, so a broken plugin doesn't
flood your APIs.

**Where it runs:** the working directory is the agent's directory, so `git`
and `gh` work there directly. If that directory was deleted (a removed
worktree), it is the plugin directory instead.

---

## Input (stdin)

One JSON object per run. Use `regia plugin input <plugin> <agent>` to see a
real one. Example:

```json
{
  "version": 1,
  "event": "refresh",
  "plugin": { "name": "jira", "dir": "/home/me/.config/regia/plugins/jira" },
  "agent": {
    "id": "3f2a9c1e",
    "session_id": "3f2a9c1e-5ea8-476e-b9af-5d8b8c51c01d",
    "name": "fix login redirect",
    "title": "fix login redirect",
    "cwd": "/home/me/work/webapp/.claude/worktrees/login",
    "kind": "background",
    "state": "done",
    "interactive": false,
    "branch": "PROJ-482-login-redirect",
    "intent": "Fix PROJ-482: users are sent to / after login instead of the page they came from",
    "detail": "Fixed and opened PR #918",
    "needs": "",
    "links": [{ "kind": "pr", "id": "918", "url": "https://github.com/acme/webapp/pull/918" }],
    "transcript": "/home/me/.claude/projects/-home-me-work-webapp/3f2a9c1e-….jsonl",
    "started_at": "2026-11-15T09:12:03Z",
    "updated_at": "2026-11-15T10:40:51Z",
    "group": ["webapp", "auth fixes"]
  },
  "git": {
    "branch": "PROJ-482-login-redirect", "upstream": "origin/PROJ-482-login-redirect",
    "ahead": 0, "behind": 0, "changed": 0, "head": "a1b2c3d",
    "subject": "Keep the return URL through login", "worktree": true
  },
  "pr": {
    "number": 918, "url": "https://github.com/acme/webapp/pull/918", "title": "Keep the return URL through login",
    "state": "open", "checks": "failing", "review": "requested"
  },
  "last": null
}
```

| field | meaning |
|---|---|
| `version` | Protocol version, currently `1`. It changes only on a breaking change. Check it if you want to fail loudly. |
| `event` | `"refresh"` (show what you know), `"action"` (do something; see Actions), `"remove"` (may it be removed?) or `"swap"` (may a fresh session take its place?); see Gates. |
| `action` | The action's `id`. Present only when `event` is `"action"`. |
| `by` / `reason` | Only for `"remove"` and `"swap"`: who asks (`"you"`, `"you (regia swap)"`, `"you (regia swap --answer)"`, or the name of the agent calling regia's tool) and the reason given. |
| `to` / `note` | Only for `"swap"`: the session taking the agent's place (the same fields as `agent`) and the note the caller gave, e.g. the handoff file. |
| `plugin.dir` | The plugin's own directory, for config files and caches. |
| `agent.id` | Short id, the one `claude attach` and `regia --dump` use. Stable for the agent's life. Use it as a cache key. |
| `agent.title` | What regia shows for the agent: the user's rename if there is one, else `name`. |
| `agent.cwd` | The agent's directory, often a git worktree. |
| `agent.state` | `blocked` (waiting on the user), `busy`, `idle`, `done`, `failed`. |
| `agent.branch` | The branch checked out in `cwd` now, or the branch from the job file when git info isn't known yet. May be empty. |
| `agent.intent` | The task the agent was started with (the first prompt). Ticket keys often appear here when they aren't in the branch name. |
| `agent.detail` / `agent.needs` | The agent's latest status line, and its question when it's `blocked`. |
| `agent.links` | Links the agent recorded (`kind` is e.g. `"pr"`). Can be empty. |
| `agent.transcript` | Path to the session transcript (JSONL), if you need to dig further. Read it; never write it. |
| `agent.group` | The groups and coordinators the agent sits under in regia, outermost first. |
| `agent.swapped_from` | The sessions this one took the place of through a [swap](./guide/agents-organize#swapping-a-session-for-a-fresh-one), oldest first: `id`, `session_id`, `title`, `at`, `reason`, `note` (e.g. the handoff file). Absent when there are none. |
| `git` | `null` when `cwd` isn't a git checkout or regia hasn't read it yet. `changed` counts modified plus untracked files. |
| `pr` | `null` when regia knows no PR for the agent. regia gets it from `gh` (the agent's recorded PR link, else the PR for its branch). `state`: `draft`, `open`, `merged`, `closed`. `checks`: `passing`, `failing`, `pending`, or `""`. `review`: `approved`, `changes`, `requested`, or `""`. Use `pr.url` to look the PR up elsewhere (Slack, Jira) instead of computing it yourself. |
| `last` | Your previous successful output for this agent (`null` on the first run, or after a manifest edit). Use it to skip work, e.g. once a ticket is Done. |

Fields can be empty strings. Treat anything missing as unknown, not as an
error. The same values are also in the environment:

| variable | value |
|---|---|
| `REGIA_PLUGIN` | plugin name |
| `REGIA_PLUGIN_DIR` | plugin directory |
| `REGIA_EVENT` | `refresh` or `action` |
| `REGIA_ACTION` | action id (empty on refresh) |
| `REGIA_AGENT_ID` | `agent.id` |
| `REGIA_AGENT_CWD` | `agent.cwd` |
| `REGIA_GROUP_ID` | `group.id` (empty unless the run is for a [group](#groups)) |

The rest of your environment is passed through unchanged, so tokens you
export in your shell profile (e.g. `JIRA_API_TOKEN`, `SLACK_TOKEN`) are
available. regia runs plugins with the environment it was started with.

---

## Output (stdout)

Print one JSON object and exit 0. Every key is optional. `{}` or empty
output means "nothing to show for this agent", which is valid and common.

```json
{
  "fields": [
    { "label": "jira", "text": "PROJ-482 · In Progress · @me", "status": "warn",
      "url": "https://acme.atlassian.net/browse/PROJ-482" }
  ],
  "badge": { "text": "J", "status": "warn" },
  "attention": "PR #918 open but PROJ-482 still In Progress",
  "actions": [
    { "id": "move-review", "label": "move PROJ-482 to In Review", "confirm": true },
    { "id": "copy-key", "label": "copy ticket key" }
  ],
  "every": "5m"
}
```

### fields

Lines in the plugin's tab, in the order you give them. At most 8 per plugin;
the rest are dropped. The agent's box on the panel has one tab per plugin,
after `agent` (regia's task, status, git and PR lines), named after the
plugin's directory. The user switches with `,` and `.` or a click, and the
tab they pick stays as they move between agents. Your tab's name turns
amber or red when a field is `warn` or `error`, and shows your badge, so
problems are visible while another tab is open. `! actions` shows on the
box's top-right when any plugin offers actions. A plugin with no fields for
an agent gets no tab. The box is as tall as its tallest tab, so a long tab
pushes the history down for every tab: keep the field count small.

| key | meaning |
|---|---|
| `text` | The line. Wrapped to at most 2 lines, then cut. Keep it short: lead with what matters, put details after ` · `. |
| `label` | The dim label on the left. Cut to 10 characters; the label column is as wide as the tab's longest label. Defaults to the manifest's `label`. |
| `status` | Color: `ok` (green), `warn` (amber), `error` (red), `dim` (gray), or omitted (plain). Use `error` only for things that need action. |
| `url` | Shown after the text, and offered as "open …" in the `!` menu (opens in the browser). If it is the agent's PR URL, regia hides its own `pr` line on the `agent` tab, since your tab already covers the PR. |

### badge

One or two characters after the agent's title in the list, colored by
`status`. Either `"J"` or `{"text": "J", "status": "warn"}`. Badges from
several plugins sit side by side, so use one only when it's worth seeing from
the list. Omit it when everything is normal.

### attention

A short reason (one line) to list the agent in the inbox's "needs a look"
group, even though it isn't waiting on the user. The inbox shows it as
`⚠ <plugin>: <attention>` (the directory name, as on the tab). It's the strongest thing a plugin can do: set it
only when the user should act, and clear it (omit the key) as soon as they
don't need to. For an agent that is already waiting on the user (`blocked`),
the inbox shows its question rather than your reason.

### actions

Entries in the `!` menu for this agent, at most 9 per plugin. `id` is
required, `label` defaults to the id. `confirm: true` makes the user press
the menu key a second time before the action runs. Use it for anything that
changes something outside the machine (moving tickets, posting to Slack,
re-running CI).

Actions are offered only after a refresh has listed them, so list an action
only when it makes sense for this agent's current state.

### every

Overrides the manifest's `every` for this agent until the next run. Use a
Go duration (`"30s"`, `"10m"`, `"1h"`, minimum 10s), or `"never"` to keep
this result until regia restarts or the manifest changes. For example, poll
faster while CI is pending, and use `never` once the PR is merged and the
ticket is Done.

---

## Actions

When the user picks an action, regia runs the same command with
`"event": "action"` and `"action": "<id>"`. The input is otherwise the same
as for a refresh, but `last` is `null`. The action runs in the background
with the manifest's timeout.

An action's output may contain:

| key | effect |
|---|---|
| `message` | Shown in the status line, e.g. `"PROJ-482 → In Review"`. Without it, regia shows `<action label> done`. |
| `copy` | Put on the clipboard (with `copied` added to the message). Use it for anything the user will paste into the agent: a failing job's log excerpt, review comments, a ticket's acceptance criteria. |
| `open` | A URL to open in the browser. |
| `remove` | `true` removes the agent the way `x` does (`claude rm`, after every [gate](#gates), put back with the reason if refused). Give the action `confirm: true`: that is the only question asked. |
| `fields`, `badge`, `attention`, `actions` | If any are present, they replace the tab's result for this agent right away. |

| `start` | Start an agent: see [Starting an agent](#starting-an-agent). |

If the output has none of the display keys, the plugin is refreshed at the
next tick, so the tab shows the new state. You don't need to return a
refresh from the action.

A non-zero exit shows `<plugin>: <action> failed: <last stderr line>` in the
status line. Actions should be idempotent where possible: the user might run
one twice.

---

## Groups

With `"targets": ["group"]` the plugin runs for each group (a row you made
with `g`, or an agent's own row with agents under it is still an agent), not
for agents; `["agent", "group"]` runs it for both. Everything works as for an
agent: its `badge` goes on the group's row, its `fields` in a tab of the
group's box on the panel (`,` and `.` switch tabs), its `actions` under `!`
with the group selected. `attention` doesn't apply to groups: the inbox
lists agents.

The input has `group` in place of `agent`:

```json
{
  "version": 1,
  "event": "refresh",
  "plugin": { "name": "epic", "dir": "/home/me/.config/regia/plugins/epic" },
  "group": {
    "id": "g:3",
    "name": "E1 auth",
    "path": ["webapp", "E1 auth"],
    "dir": "/home/me/work/webapp",
    "coordinator": { "id": "9b1d0c22", "title": "E1 coordinator", "...": "the same fields as agent" },
    "agents": [ { "id": "9b1d0c22", "...": "…" }, { "id": "3f2a9c1e", "...": "…" } ]
  },
  "last": null
}
```

| field | meaning |
|---|---|
| `group.id` | The group's id (`g:…`), stable for its life. Use it as a cache key; `regia plugin input <plugin> g:3` shows a real input. |
| `group.name` / `group.path` | Its name, and the names of the groups it sits in, outermost first, then its own. |
| `group.dir` | Where a new agent in it starts: the directory you set with `w`, or the one guessed from its agents. The plugin runs there. |
| `group.coordinator` | Its first agent with agents under it, or `null`. |
| `group.agents` | Every listed agent in it, at any depth, in tree order, with the same fields as `agent`. |

`git` and `pr` are `null`, and `dirs` is matched against `group.dir`;
`states` and `interactive` don't apply to groups.

---

## Starting an agent

An action's output (or a refresh's, see below) may ask regia to start an
agent:

```json
{ "start": { "prompt": "Go on with E1 from journal.md", "as": "coordinator",
             "dir": "~/work/webapp", "model": "opus" } }
```

| key | meaning |
|---|---|
| `prompt` | Required: the task, as you'd type it. |
| `dir` | Where it starts. Default: for `swap` the agent's own directory, else where typing a task with the row selected would start it. |
| `model` | A model, as `claude --model` takes it. Default: claude's. |
| `as` | `agent` (default): a new agent next to the row's agent, or in the group. `coordinator`: the same, and once it runs, the group's agents (or the row's agent) move under it. `swap`: a fresh session that takes the agent's place, as [`regia swap`](./guide/agents-organize#swapping-a-session-for-a-fresh-one) does, with its [gates](#gates); only on an agent's row. |
| `reason` / `note` | For `swap`: the reason and note recorded with it (and given to the swap gates). |
| `key` | Required in a refresh's output, see below. |

regia starts it as typing a task does, selects it, and waits (up to 20 s)
for `claude agents` to list it before moving agents or swapping. When the
start came from the [attach gate](#gates), regia then attaches to the new
session.

**From a refresh:** a refresh may start an agent too, e.g. each morning,
but only once per `key`: regia records each plugin's keys (for 30 days, in
`~/.config/regia/plugin-starts.json`) and never starts the same key twice.
A refresh `start` without `key` is ignored, with a warning on the tab. Use a
key that names the occasion, like `"standup-2026-11-24"`.

---

## Gates

A gate lets a plugin hold something back. There are three, `remove`,
`swap` and `attach`.

**`remove`:** before a session is removed (`claude rm`), from `x`, from select mode, from a
plugin action answering `remove`, or from an agent calling regia's `remove`
tool, regia runs every plugin with `"gates": ["remove"]` whose `dirs` take in
the agent, one after the other, with `"event": "remove"`. The input is the
same as for a refresh (`last` is `null`; when an agent asks, `pr` is `null`),
plus `by` and `reason`.

Print `{"refuse": "<why>"}` to hold the removal back; the user (or the agent
that asked) sees `<plugin>: <why>`, and the session stays. Anything else
(`{}`, empty output) lets it go. `states` doesn't apply to gates.

A gate that fails (non-zero exit, timeout, output that isn't JSON) refuses
too, with its last stderr line: a rule you set up is never skipped silently.
Keep gates fast; `x` waits for them.

**`swap`:** before a [session swap](./guide/agents-organize#swapping-a-session-for-a-fresh-one)
(`regia swap`, or an agent's `swap` tool), regia runs the plugins with
`"gates": ["swap"]` with `"event": "swap"`: `agent` is the session being
replaced, `to` the fresh one taking its place, plus `by`, `reason` and `note`.
The `remove` gates are not asked as well: list both if one rule should decide
both. Refusing and failing work as for `remove`.

For example, a team that has each session write a handoff before it goes:

```bash
input=$(cat)
id=$(jq -r .agent.session_id <<<"$input")
if [ "$(jq -r .event <<<"$input")" != refresh ] && [ ! -s "$HOME/handoffs/$id.md" ]; then
  echo '{"refuse": "no handoff yet (~/handoffs/'"$id"'.md)"}'
  exit 0
fi
echo '{}'
```

**`attach`:** before you attach to an agent (`enter` in the tree, in search,
`alt+enter` in the conversation), regia runs the plugins with
`"gates": ["attach"]` whose `dirs` take in the agent, with `"event": "attach"`
and the same input as for a refresh. Answer:

- `{}` (or nothing): attach.
- `{"refuse": "<why>"}`: don't attach; you see `<plugin>: <why>`.
- `{"run": "<action id>"}`: run that action of yours first, as if picked
  from `!`, then attach. If the action [starts an agent](#starting-an-agent),
  regia attaches to the new session once it's listed instead. The typical
  use: a session that has grown too large gets a fresh one from its handoff
  (`"as": "swap"`), and `enter` lands you there.

Unlike the other gates, `attach` fails open: a gate that fails, or takes
more than 2 s in all, lets you through with a warning, since attaching
destroys nothing. Keep it fast: `enter` waits for it.

With `"gates": ["remove", "swap"]` it holds back both. The same plugin, with `"states": ["done", "removed"]`, can show the handoff
as a field with its path as `url` (`file:///…`): next to a finished agent, and next to the
removed session in search's whole history, where `ctrl+o` opens it.

---

## Rules for plugins

1. **stdout is only for the JSON.** Send logs, progress and debug output to
   stderr. regia shows the last stderr line when a run fails, so make that
   line say what's wrong (`not logged in to jira: run jira init`).
2. **Be fast.** Aim for under 2 s. Every run is one process per agent, and
   with 20 agents that adds up. Make one API call rather than five, and cache
   in `$REGIA_PLUGIN_DIR/cache/` if an answer rarely changes (e.g. ticket id
   → project config).
3. **Nothing to say is not an error.** No ticket key found, not a repo you
   handle, no PR yet: print `{}` and exit 0. Exit non-zero only for real
   failures (auth broken, API down), so the user sees them.
4. **Refreshes must not change anything.** A refresh only reads. Anything
   that changes outside state is an action, and usually `confirm: true`.
5. **Never print secrets.** No tokens in fields, messages or stderr. Read
   credentials from the environment or from the CLI's own config (`gh`,
   `jira`, `acli`), not from `plugin.json`.
6. **Use what regia already gives you.** Use `pr.url`, `pr.checks`,
   `git.branch` and `agent.intent` instead of asking `gh` or `git` again.
7. **Handle partial input.** `git`, `pr` and `last` can be `null`, and any
   string can be empty. Interactive sessions have most job fields empty.
8. **Keep text terse.** The panel is often 60–90 columns wide; the box's
   border and your label column take about 15 of them. Short labels (`ci`,
   `ticket`) leave more room. Use fragments, not sentences.
9. **Use `attention` sparingly.** If everything is attention, the inbox is
   useless. Set it only when the user should act now.

---

## Patterns

These are starting points; adapt them to your team's setup.

**Find a ticket key** from the branch, then the task, then the PR title:

```bash
key=$(jq -r '[.agent.branch, .agent.intent, (.pr.title // "")] | join(" ")' <<<"$input" \
      | grep -oE '[A-Z][A-Z0-9]+-[0-9]+' | head -1 || true)
[ -z "$key" ] && { echo '{}'; exit 0; }
```

**Check specific CI workflows** (not every check) for the branch, with `gh`
from the agent's directory:

```bash
gh run list --branch "$branch" --limit 20 --json workflowName,status,conclusion,url \
  | jq '[.[] | select(.workflowName == "e2e" or .workflowName == "deploy-preview")] | unique_by(.workflowName)'
```

**Find the review post for a PR in Slack**, using the PR URL regia passes:

```bash
url=$(jq -r '.pr.url // empty' <<<"$input"); [ -z "$url" ] && { echo '{}'; exit 0; }
curl -s -H "Authorization: Bearer $SLACK_TOKEN" -G https://slack.com/api/search.messages \
  --data-urlencode "query=$url in:#reviews" --data-urlencode count=1
```

**Compare across systems:** this is often where a plugin is most useful. For
example, set `attention` when the PR is open but the ticket isn't In Review,
when the PR is merged but the ticket isn't Done, or when a PR has no review
post after 30 minutes.

**Handle both events in one script:**

```bash
input=$(cat)
case "$(jq -r .event <<<"$input")" in
  refresh) refresh ;;
  action)  case "$(jq -r .action <<<"$input")" in
             move-review) move_review ;;
             *) echo "unknown action" >&2; exit 2 ;;
           esac ;;
esac
```

You can write a plugin in any language. Python's standard library (`json`,
`urllib`, `subprocess`) is enough for most plugins, without installing
dependencies.

---

## Checklist

- [ ] `regia plugin new <name>`, then edit `plugin.json`: set `dirs` to the
      directories it applies to and `states` if it only matters in some
      states, and pick an `every` that fits the API's rate limits.
- [ ] `regia plugin input <name> <agent>` on two or three real agents (one
      with a PR, one without, one outside `dirs`) to see the real input.
- [ ] Refresh: prints `{}` when there's nothing to say, sets fields with
      useful statuses, sets `attention` only when the user must act, and
      makes no changes.
- [ ] Failures exit non-zero with one clear stderr line. Test with auth
      removed (e.g. `env -u JIRA_API_TOKEN regia plugin run …`).
- [ ] Actions: each listed only when it applies, `confirm: true` on anything
      that changes outside state, a `message` on success, and idempotent.
- [ ] `regia plugin run <name> <agent>` ends with `✓ ok` (no `⚠ problems`)
      for each test agent, and `--action <id>` works for each action.
- [ ] Runs in under ~2 s (`time regia plugin run …`).
- [ ] `regia plugin list` shows it with ✓. Then open regia, select an agent,
      and check the tab, the badge, the inbox and the `!` menu.
