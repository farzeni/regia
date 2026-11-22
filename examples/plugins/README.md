# Example plugins

Working plugins to install as they are or to start from. The contract they
follow is in [docs/plugins.md](../../docs/plugins.md).

## gh-pr

The agent's GitHub PR in its own tab of the agent's box, from `gh`:

```
╭─ agent │ gh-pr ✗ ──────────────────────────────────── ! actions ─╮
│ pr       #1 draft · +18 −0  https://github.com/you/repo/pull/1 │
│ ci       ▰▰▰▰▰▰▰▱▱▱ 2/3 · 1 running · 1 failed: flaky           │
│ reviews  approved by ana · waiting on bob                      │
│ talk     3 comments · last by ana 5m ago                       │
╰────────────────────────────────────────────────────────────────╯
```

- **badge:** `✓`, `…` or `✗` for CI on the agent's row
- **attention:** CI failing or changes requested puts the agent in the inbox
- **actions (`!`):** copy the failing jobs' logs to paste into the agent;
  re-run the failed jobs (once CI has stopped); mark a draft ready for review
- re-checks every 20 s while CI runs, every minute otherwise, never once the
  PR is merged or closed (then only the `pr` line is left: no badge, alarm or
  actions)

regia's own `pr` line is hidden on agents whose PR this plugin shows.

It needs `gh` (logged in) and Python 3. It uses only the standard library.

```bash
cp -r examples/plugins/gh-pr ~/.config/regia/plugins/
regia plugin run gh-pr <agent-id>      # check it on an agent with a PR
```

regia picks it up within a few seconds, with no restart needed. To limit it
to some repos, add `"dirs": ["~/work"]` to its `plugin.json`.
