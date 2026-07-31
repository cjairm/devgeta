---
name: dg-worktree
description: Spin up a ready-to-work devgeta worktree environment for a ticket, issue, or branch. Runs `dg wt create` to create the worktree + tmux window with Claude Code launched already working on a `/teach` explanation of the ticket, and a second pane running the repo's init (`make finit`). Use this whenever the user asks to create a worktree, grab or start a ticket, "take a look at issue #N", spin up an environment for a branch, or begin work on a GitHub issue — even if they don't say "worktree" or "devgeta". Prefer this over EnterWorktree or plain `git worktree add` when the user wants a full working environment rather than switching the current session.
---

# dg-worktree: ticket-ready worktree environments

Create an isolated worktree with a fully set-up tmux window so the user can
start reading about the ticket immediately while the environment initializes.
devgeta does all the heavy lifting natively: `--prompt` launches Claude Code
already working on the ticket (a launch argument — it cannot be dropped by
TUI timing; never re-implement this with tmux send-keys), and `--pane` adds a
shell pane running a command. Your job is only the derivation: issue → name +
prompt, and repo → main checkout.

**Requires** on the host: the devgeta CLI (`dg`) with `--prompt`/`--pane`
support (v1.10.0+), a running tmux server, `gh`, and `make`.

## Workflow

### 1. Resolve the ticket (when an issue/ticket number is given)

```bash
gh issue view <N> --json number,title,state,assignees \
  -q '{number, title, state, assignees: [.assignees[].login]}'
```

- Derive the worktree name: `issue-<N>-<short-kebab-slug>` from the title.
  Keep it short (≤ 50 chars total), lowercase, letters/digits/dashes only —
  the name becomes a git branch and a tmux window name.
- If the issue is assigned to someone other than the current user, still
  proceed, but mention it in your summary — they may be duplicating claimed
  work (many repos treat assignment as ownership).
- If the user gave a plain name instead of an issue ("a worktree called
  spike-x"), skip `gh` and use their name as-is (kebab-cased).

### 2. Resolve the main checkout

`dg wt create` must act on the primary repo, not on a nested worktree the
current session may be sitting in. `git worktree list` always reports the
main checkout first:

```bash
REPO="$(git worktree list --porcelain | awk '/^worktree /{sub(/^worktree /,""); print; exit}')"
```

If the user wants this for a different repository, use its path instead.

### 3. Create the environment

One command. The `--pane` value is a shell command line used as written, so
the "does this repo have a finit target?" decision is a shell guard inside
the pane itself — either way the pane ends at a usable prompt:

```bash
dg wt create <name> --ai claude --repo "$REPO" \
  --prompt '/teach issue #<N> — <issue title>' \
  --pane 'make -n finit >/dev/null 2>&1 && make finit || echo "no finit target, skipping init"'
```

Without an issue there is nothing for `/teach` to explain — omit `--prompt`
(dg errors on `--prompt` only with AI-less layouts, so with `--ai claude`
including it is always safe when you have one).

devgeta creates the worktree + branch (in-repo config: under
`<repo>/.claude/worktrees/<name>`), the tmux window `wt-<repo>-<name>`, and
switches the user's tmux client to it. It adopts an existing branch of the
same name; if that branch is checked out in the main clone, devgeta moves the
main clone to the default branch first and says so.

### 4. Report back

Tell the user, concisely:

- The tmux window (`wt-<repo>-<name>`) and worktree path. If they were inside
  tmux, devgeta already switched them there; otherwise tell them which
  session to attach to (named after the repo).
- Whether Claude launched with the `/teach` prompt, and what the init pane is
  doing (`make finit`, or the skip message in repos without the target).
- Any ownership warning from step 1.

Do NOT switch the current session into the new worktree — the whole point is
that the new window has its own Claude session; this session stays where it is.

## Failure modes

- **`dg` missing / no tmux server**: dg exits with a one-line reason. Relay
  it; do not fall back to `git worktree add` silently — the user asked for
  the full environment, so a bare worktree without the window would be a
  surprise.
- **Branch/worktree already exists**: devgeta adopts existing branches, but a
  worktree already present at the target path is a real conflict. Show the
  user `dg wt l` output and ask whether to reuse (`dg wt repair <name>`) or
  pick a different name.
- **`make finit` fails inside the pane** (missing dependencies, a secrets or
  environment check the repo runs at init): that output is visible to the user
  in the pane, which is the intended behavior — report it if you notice it,
  but it is not this skill's failure.

## Cleanup

When the user is done with an environment created here: `dg wt rm` (interactive
picker) removes the worktree and its window together. Mention it only if they
ask how to tear down.
