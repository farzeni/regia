# Writing regia plugins

A regia plugin is a script that regia runs for each agent. The script gets
the agent's details as JSON on stdin and prints JSON on stdout. regia shows
what it prints in four places:

- **fields**: lines on the agent's card in the right panel, e.g.
  `jira    PROJ-482 · In Review`
- **badge**: one or two characters on the agent's row in the list, e.g. `J`
- **attention**: a reason to put the agent in the inbox even though it isn't
  waiting on you, e.g. "PR open but ticket still In Progress"
- **actions**: entries in the `!` menu on the agent's panel, e.g. "move ticket
  to In Review". Picking one runs the script again with that action.

Plugins are for checks that depend on your own setup: the tracker you use, CI
workflows, review channels, deploy dashboards. regia handles scheduling,
caching, timeouts and display. The plugin finds the facts and decides what
needs your attention.

This document is the full contract. If you are an agent asked to write a
plugin, read it to the end and use the checklist at the bottom.

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
  "disabled": false
}
```

| key | default | meaning |
|---|---|---|
| `command` | required | The program and its arguments, as an argv array (not run through a shell). A path containing a `/` is relative to the plugin directory. A bare name is looked up in the plugin directory first, then on `PATH`. So `["./check.sh"]`, `["python3", "check.py"]` and `["/usr/local/bin/x"]` all work. |
| `label` | the directory name | The label for fields that don't set their own. |
| `dirs` | every agent | Run only for agents whose working directory is one of these or inside one. `~` is expanded. Glob characters are allowed and are matched against the directory and each of its parents, so `"/srv/*/repo"` works. A trailing `/**` means the same as the directory itself. Use this to keep work plugins off personal projects. |
| `states` | every state | Run only for agents in these states: `blocked` (waiting on you), `busy`, `idle`, `done`, `failed`, `ready`. `ready` and `done` match each other: `ready` is regia's name for a finished agent you haven't looked at yet. |
| `interactive` | `false` | Also run for interactive sessions, i.e. `claude` running in a terminal rather than as a background job. They have no job state, so `branch`, `intent` and `links` are mostly empty for them. |
| `every` | `"2m"` | How long a result is kept before the plugin runs again for that agent. Minimum `10s`. Output can override it (see `every` below). |
| `timeout` | `"10s"` | How long one run may take. After that the process is killed and the run counts as a failure. |
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

Each run is one process for one agent, from start to exit. Nothing stays
running between runs. Results are cached in memory per plugin and agent, so a
restart of regia runs everything again.

**Failures:** a non-zero exit, a timeout, or stdout that isn't JSON. The card
then shows a dim line with the last line of stderr, e.g.
`jira    ⚠ jira: exit status 1: not logged in`, and keeps showing the last good
fields above it. A plugin that keeps failing is retried less and less often,
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
| `event` | `"refresh"` (show what you know) or `"action"` (do something; see Actions). |
| `action` | The action's `id`. Present only when `event` is `"action"`. |
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

Lines on the card, shown after the git and PR lines, in the order you give
them. At most 8 per plugin; the rest are dropped.

| key | meaning |
|---|---|
| `text` | The line. Wrapped to at most 2 lines, then cut. Keep it short: lead with what matters, put details after ` · `. |
| `label` | The dim label on the left. Cut to 7 characters; defaults to the manifest's `label`. |
| `status` | Color: `ok` (green), `warn` (amber), `error` (red), `dim` (gray), or omitted (plain). Use `error` only for things that need action. |
| `url` | Shown after the text, and offered as "open …" in the `!` menu (opens in the browser). |

### badge

One or two characters after the agent's title in the list, colored by
`status`. Either `"J"` or `{"text": "J", "status": "warn"}`. Badges from
several plugins sit side by side, so use one only when it's worth seeing from
the list. Omit it when everything is normal.

### attention

A short reason (one line) to list the agent in the inbox's "needs a look"
group, even though it isn't waiting on the user. The inbox shows it as
`⚠ <label>: <attention>`. It's the strongest thing a plugin can do: set it
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
| `message` | Shown in the status line, e.g. `"PROJ-482 → In Review"`. Without it, regia shows `<label> done`. |
| `copy` | Put on the clipboard (with `copied` added to the message). Use it for anything the user will paste into the agent: a failing job's log excerpt, review comments, a ticket's acceptance criteria. |
| `open` | A URL to open in the browser. |
| `fields`, `badge`, `attention`, `actions` | If any are present, they replace the card's result for this agent right away. |

If the output has none of the display keys, the plugin is refreshed at the
next tick, so the card shows the new state. You don't need to return a
refresh from the action.

A non-zero exit shows `<label>: <action> failed: <last stderr line>` in the
status line. Actions should be idempotent where possible: the user might run
one twice.

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
8. **Keep text terse.** The panel is often 60–90 columns wide, and the label
   column takes 8. Use fragments, not sentences.
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
      and check the card, the badge, the inbox and the `!` menu.
