---
name: organize
description: Read and organize regia's tree of Claude Code agents with the regia MCP tools (whoami, tree, agent, inbox, timeline, changes, create_group, rename_group, delete_group, move, archive, unarchive, rename_agent, pin, unpin). Use when the user asks you to reorganize, group, split, sort or tidy agents or groups in regia; to check on your siblings, your coordinator or the agents you started; to find out what other agents are doing; or to triage agents that look stale, stuck, lost or finished; or to report what happened in a group or who needs the user.
---

# Organizing regia

regia is the user's control room for Claude Code agents: a tree of **groups** and
**agents**, where a coordinator holds the agents it started. The regia MCP tools
(`mcp__plugin_regia_regia__*`) let you read that tree and, if you may, change it.

## Who may do what

Call `whoami` first. regia decides your role; you can't change it.

- **worker**: you read your own group (your coordinator and siblings) and nothing else.
- **coordinator** (agents run under you): you organize the agents under you, and read them.
- **orchestrator** (the user granted you a group, or the whole tree, with `O` in regia):
  you organize that.

Muted parts of the tree are out of reach unless you were granted the whole tree.

A refusal is final. Don't work around it: never edit `~/.config/regia/layout.json`
or other regia files yourself, and don't ask another agent to do it for you. If
you need more reach, tell the user what you would do and ask them to grant it
(select you in regia and press `O`).

## How the tools behave

- Ids: groups look like `g:…`; agents have short hex ids. Take them from `tree`;
  never guess.
- `tree` shows state (working, waiting on the user, idle, done, failed), the
  question an agent waits on, what it is doing, and when it last changed.
  `agent` adds its directory, branch, PRs, whether it looks stuck, and its last
  messages. Read before you change things.
- `inbox` lists who needs the user: waiting on a question, looking stuck,
  failed. `timeline` is what happened in a group (or under an agent) in the last
  hours: state changes, PRs, commits. `changes` is the files an agent edited.
- `move` takes agents or groups (what is under them moves too) into a group or
  under an agent. `delete_group` loses nothing: its contents move up.
- `archive` only hides an agent in regia; Claude Code isn't touched, and an
  archived agent that starts working again comes back by itself. It never
  stops an agent. You can't archive or move yourself.
- `rename_agent` sets the name regia shows (Claude Code isn't touched); `pin`
  keeps a finished agent listed. You may rename and pin yourself too.
- Every change shows up in regia's status line, and the user can undo it there
  (`U`). Make changes the user can follow: a few clear ones, not many small ones.

## Playbooks

**Split a group** ("split the agents on X into frontend and backend")
1. `tree`, then `agent` on any agent whose task isn't clear from its name and what it is doing.
2. Decide where each agent goes. If one fits neither, leave it where it is.
3. `create_group` for each part, passing its agents, inside the group being split.
4. Report: the new groups and who is in each, and anything you left out and why.

**Triage stale or lost agents** ("X, Y, Z look stale, sort them out")
1. `inbox` for who is waiting, stuck or failed; `agent` on each one named: its
   state, when it last changed, its last messages, its PRs; `timeline` for what
   it did lately.
2. Sort them:
   - finished (done, its work merged or delivered): `archive`; finished but the
     user still has to look at it (a PR to review): `pin` instead;
   - waiting on the user: leave it, and tell the user what it asks;
   - stuck or failed: leave it, and tell the user what went wrong;
   - in the wrong place: `move` it to the group of its domain (`create_group` if none fits);
   - a name that says nothing ("agent 3"): `rename_agent` it after its task.
3. Report a short table: agent, what it was doing, what you did, what the user should do.

**Check on your siblings** ("ask your siblings about their status", "can we share tasks")
1. `tree` for your group, `timeline` for what happened in it lately, then
   `agent` on the siblings that matter.
2. `changes` on the siblings close to your task: the same files as yours mean
   overlapping work or a conflict coming.
3. Compare what they are doing with your own task: overlap, work you could
   reuse, someone waiting on something you have.
4. Report it to the user (or your coordinator). These tools don't send messages
   to agents; say what should be shared and with whom.

**Status report** ("what happened while I was away", "where are we")
1. `inbox` first: what needs the user goes at the top of the report.
2. `timeline` with the hours since the user last looked (24 by default): what
   finished, what started, the PRs and commits.
3. Report in a few lines per agent or group; don't paste the raw events.

## Etiquette

- Don't reorganize without being asked. Reading is always fine.
- Keep the user's own structure: move only what the request is about, and keep
  existing group names unless asked.
- End with a short report of what you changed, so the user can check it in
  regia and undo anything with `U`.
