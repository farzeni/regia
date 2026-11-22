# Group awareness

Agents can know about the group they're in. Only groups that were made on
purpose count: groups you created by hand, and a coordinator with the agents it
started (or that you moved under it). Automatic project groups don't, so agents
that only share a directory know nothing about each other.

- **`regia context`** (run by an agent): its group, its coordinator, its
  siblings and its own agents with state and last line. Outside regia,
  `regia context <id>` prints what an agent would see.

## Timeline

**Timeline** (`T`, "what happened while I was away"): the panel of a group or
coordinator swaps for one feed of everything its agents did, oldest first and
the newest at the bottom, a rule per day, one line each: the time, who, and
what. It lists:

- the agents' state changes (▶ started, ● waiting with its question, ◆ back to
  work, ✓ done, ✗ failed, from `~/.claude/jobs/<id>/timeline.jsonl`);
- the commits on their branches since they started (⎇, `git log` of each
  distinct directory and branch, read in the background and cached for 30 s;
  a commit goes to the last agent on that branch started before it);
- the PRs they opened (⇡, from `state.json`, dated when the agent first
  mentioned the link).

`pgup`/`pgdn` scroll it, `T` goes back, and `T` on an agent opens its group's
timeline. Whatever can't be read is left out. Agents working in the main
checkout share its branch, so commits made there by others show too.

## Staying out of the agents' way

The plugin's hook does nothing for agents outside a group. A grouped agent gets
a short introduction once (where it is and what `regia context` is for), then
a single line only when it was moved, at most once a minute (or when it gets a
message). The introduction says what "coordinator" and "siblings" mean and
comes again after the conversation is compacted or cleared, and when a message
from you talks about the group ("check your siblings", "use regia context",
"your coordinator", at most every 10 minutes); an agent outside a group is
then told it is in none.

## Turning it off

`regia group off` for every session (`regia group on` to undo),
`REGIA_GROUP=off` for one session, or `M` in regia to mute one group or
coordinator (its agents get nothing, and it isn't listed to others).
