# ADR-0056 — The dashboard lists agents in their own section

**Date:** 2026-09-28
**Status:** ACCEPTED

## Context

`dg ws` shows agent state as a colored glyph on worktree and session rows, and per pane
when a row is expanded ([ADR-0008](ADR-0008-agent-state-on-every-pane-row.md)). Two
problems:

- **Most rows aren't agents.** Every window gets a status glyph, including windows that
  only run Neovim or a shell, so the colors are noise wherever no agent runs.
- **"Who needs me?" means scanning the whole tree.** There's no list of agents, no state
  word, and no coder name. Answering it means reading dot colors across every repo and
  expanding rows to find the pane.

The layout was chosen from mockups at https://claude.ai/artifact/K9jS9aw5yyRZkNNbKUzJ3d.
A tmux-sidebar alternative (two real tmux panes) was mocked and rejected; see below.

## Decision

The left column holds two stacked sections: **spaces** on top, **agents** below.

**Spaces** is today's tree (repos, worktrees, sessions) with **no status markers at all**:
no agent colors and no "has a window" dot. Pane rows under an expanded row keep their
`window:index command` text but lose their state glyph. Agent state is shown only in the
agents section. This supersedes ADR-0008's per-pane state glyph; the pane rows themselves
stay.

**Repos with no open window start folded.** This is a default, not a rule re-applied on
every refresh. It's worked out for a repo the first time the dashboard sees that repo
during a launch: at the first worktree load, or when the repo first appears later. After
that, only the user folds or unfolds it. The 3-second tick never re-folds a repo you opened.
An explicit choice beats the default and is saved
([ADR-0050](ADR-0050-dashboard-view-state-lives-in-a-tmux-server-option.md)): folding goes
into the saved `collapsed` list as today, and unfolding a repo whose default is folded goes
into a new `expanded` list. So the next launch respects the choice either way. The same
pruning rule applies to both lists: keys for rows that no longer exist are dropped on write.

**Agents** is one flat list of every agent pane ([ADR-0055](ADR-0055-an-agent-says-what-it-is.md))
across all repos and sessions. It's sorted by urgency, blocked > error > done > working >
idle, which is ADR-0005's aggregation order with "idle" split into done and idle. Ties
sort by location label, then by pane index. Each agent takes two lines:

```
! devgeta/fix-stale-notify      :1
  blocked · claude
```

The first line is the state glyph, where the pane lives (`repo/worktree`, or the session
name), and `:N`, the pane index. The second line is the state word and the coder name. The
selection bar runs down both lines as one stripe. `↵` switches to that exact pane, reusing
the pane-row switch and its per-pane acknowledgement. The right pane is unchanged: on an
agent row it shows what that pane's own row would show (the worktree's diff).

**Folding.** `a` folds or unfolds agents and `w` folds or unfolds spaces, from anywhere,
in any order. (`s` already means "new session".) A folded section collapses to a one-line
bar at the bottom of the column. The agents bar keeps per-state counts (`! 1  ◆ 1  ● 1  ○ 1`)
so a new blocked agent is visible while the section is closed. The last open section can't
be folded; its key does nothing. Folding the section the cursor is in moves the cursor to
the other section.

**Split.** Each section scrolls on its own. `+` / `-` move the split, and so does
dragging the agents header line with the mouse, the same way the left/right divider is
dragged today. The dashboard already runs in cell-motion mouse mode, so this changes
nothing about text selection.

**Filter.** `/` searches only the open sections. In agents, it matches the location, the
coder name, and the state word, so `/blocked` and `/opencode` work.

**Saved view state** (ADR-0050) gains four optional fields: `expanded` (above),
`agentsFolded`, `spacesFolded` and `split`. **The cursor is still not saved**, for
ADR-0050's reason: the dashboard opens on the row for the session you're in, and a saved
cursor would compete with that. If spaces is folded, the cursor starts on the agent in the
window you came from, by the same rule, or on the first agent when that window runs none.
Either way the cursor never sits in a folded section: walking into one stops at its edge, as
ADR-0057 already says for the pane-move keys.

The version stays `1`, because every new field is optional and additive. A value without
them reads as "both open, default split, no expanded repos". An older binary's decoder
ignores fields it doesn't know. The only cost is that an older binary writing the state
drops them, and that only happens while two versions run side by side. Bumping to `2` would
be worse: every older binary would discard the whole value, folds included.

**Bottom line.** It stays for messages only, as today. No permanent summary is added: the
agents header and its folded bar already carry the counts.

**Rejected: a tmux sidebar of two real panes** (`dg ws` split to the left, spaces and
agents as separate tmux panes). It would reuse tmux's resize keys, but it means two
processes both polling tmux, it loses the diff pane, it changes every user's window layout,
and making it follow you between windows means `join-pane` on every window change, which
rearranges layouts and fights zoom. "The dashboard wherever I am" is better served later by
a tmux popup, which doesn't touch the layout. That's a separate decision.

**Rejected: fold keys on the section header (`h`/`l`), like repo headers.** Tried in the
mockups. Unfolding meant moving the cursor to the folded bar first. A key per section works
from anywhere.

## Consequences

- Easier: "who needs me" is answered by the top of one list. Rows without agents are quiet.
  Nothing new is stored: agent rows come from the pane layer the fast tick already scans
  (ADR-0024).
- Harder: the model gains a second list with its own cursor range, scroll offset and fold
  state, and every place that assumes one row list (cursor clamping, `VisibleWindow`,
  filter, `placeCursorOnActive`, saved view state) has to handle two sections.
- Accepted: an agent appears twice, as its worktree in spaces and as itself in agents.
  That's intended: one list is the map, the other is the queue.
