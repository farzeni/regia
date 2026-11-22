# Plugins

Your own scripts can add to the agent panel: lines on the card (ticket status,
the CI workflows you care about, whether the PR was posted in the review
channel), a badge on the row, a reason to put the agent in the inbox, and
actions in the `!` menu (move the ticket, copy a failing log). A plugin is a
directory in `~/.config/regia/plugins/` with a `plugin.json` and any program:
regia runs it per agent in the background with the agent as JSON on stdin and
shows the JSON it prints. `dirs` in the manifest keeps work plugins on work
repos.

```bash
regia plugin new jira                # a working skeleton to start from
regia plugin run jira <agent-id>     # run it once and check its output
regia plugin list
```

In regia, `!` opens the plugin actions and links for the selected agent, and
`,` / `.` move between the agent box's tabs: `agent`, `changes`, then one per
plugin.

The full contract (input, output, actions, caching, rules and a checklist) is
the [plugin protocol](../plugins), written so an agent can build a plugin from
it.
[examples/plugins](https://github.com/farzeni/regia/tree/main/examples/plugins)
has a working one to install or start from: `gh-pr`, the agent's PR with CI
progress, reviews and comments
(`cp -r examples/plugins/gh-pr ~/.config/regia/plugins/`).
