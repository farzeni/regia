# Inbox

What is waiting for you today: questions first, then agents that
[look stuck](./agents#stuck-agents), then finished agents whose PR needs a look;
most recent first in each. Finished chats you haven't read (**✓ ready**) are
not listed: they are marked in the tree.

Each item takes two lines: the agent, then the group (and coordinator) it sits
under and what it is asking. A finished agent (ready or done, even already
seen) whose open PR has failing CI or changes requested is listed too, with
the reason in red (`⚠ PR #12: CI failing, changes requested`); it leaves once
the PR is fixed, merged or closed, or when you archive the agent.

Whatever has waited since before today (the day starts at 4am) folds into one
row at the bottom, **earlier days**, with how many and how old; `space` opens
it (a `/` filter searches inside it too). It starts folded each time regia
opens.

`Z` snoozes a question: it leaves the inbox and the "need you" count until the
agent does something new (`Z` again brings it back; on an unread reply, `Z`
just marks it read).

## Working down the inbox

`enter` the top item, reply, and exit: once the agent is working again it
leaves and the cursor is on the next item, now on top (if you left without
replying it stays selected). `b` jumps to the next agent whose
turn is yours.
