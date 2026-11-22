---
layout: home

hero:
  name: regia
  text: A terminal control room for Claude Code background agents.
  tagline: See at a glance which agents are waiting for you, organize them your way (projects, groups, coordinators with their agents underneath), read their conversations, and jump in and out of any session in a keystroke.
  actions:
    - theme: brand
      text: Get started
      link: /guide/getting-started
    - theme: alt
      text: Why regia
      link: '#why-regia'

features:
  - title: Who needs you, now
    details: The inbox lists the questions agents are waiting on, the ones that look stuck, and finished work whose PR fails CI. Work down it with enter, reply, ←.
  - title: In and out in a keystroke
    details: enter attaches to a session; ← brings you back to regia in about 80 ms. A new task goes from the input box to a background agent, in the right folder, in the right group.
  - title: Swarms stay readable
    details: Agents a coordinator starts nest under it on their own. Groups, pins, archive and a timeline per group keep fifty agents down to the few that matter today.
  - title: Agents can help organize
    details: Through regia's MCP server an agent can read the tree and, when it claims part of it, tidy it. You see every change, and U undoes it.
---

## Why regia

Claude Code runs background agents well, but `claude agents` is a flat list. With ten or fifty agents running,
the questions are always the same: who is waiting for me, what did they do while I was away, which ones
belong together, and is that work landed?

regia answers them on one screen:

```
▾ shop  (5 agents · 1 need you · 1 ready · 2 busy)
  ● blocked  checkout redesign  [3fa9c1d2]
    ◆ busy     shop/checkout/api  [7b21e0aa]
    ◆ busy     shop/checkout/ui  [c0ffee12]
  ✓ ready    fix flaky cart test  [9d4e5f60]
  ✓ done     bump dependencies  [12ab34cd]
▾ infra  (2 agents · 0 need you · 0 ready · 1 busy)
  ◆ busy     migrate backups  [5e6f7a8b]
  ✗ failed   rotate certificates  [0a1b2c3d]

inbox (1):
  ● checkout redesign — the two agents disagree on the API shape: keep REST or move to RPC?
```

(That is `regia --dump`, the same tree as text. The real screen adds the selected agent's conversation, its
changes, its PR and an input box for new tasks.)

regia only reads Claude Code's own files and drives the `claude` CLI. It has no server and no database: your
organization is one file, `~/.config/regia/layout.json`.

**When you don't need regia:** if you run one or two background agents at a time, `claude agents` and
`claude attach` are enough.
