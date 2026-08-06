---
name: dg-worktree
description: Use when the user wants an isolated working environment for a piece of work rather than a change in the current session — creating a worktree, grabbing or starting a ticket, "take a look at issue #N", "start implementing this cycle doc / plan / spec", spinning up an environment for a branch, or opening somewhere to poke at something they just described. Triggers even when they don't say "worktree" or "devgeta". Prefer over EnterWorktree or plain `git worktree add` whenever a full environment is wanted, not just a checkout.
---

# dg-worktree: ticket-ready worktree environments

Create an isolated worktree with a fully set-up tmux window so the user can
start working immediately while the environment initializes. devgeta does all
the heavy lifting natively: `--prompt` launches Claude Code already working (a
launch argument — it cannot be dropped by TUI timing; never re-implement this
with tmux send-keys), and `--pane` adds a shell pane running a command. Your
job is only the derivation: input → worktree name + opening prompt, repo →
main checkout, repo → init command (or none).

**Requires** on the host: the devgeta CLI (`dg`) with `--prompt`/`--pane`
support (v1.10.0+) and a running tmux server. `gh` is needed only for the
issue mode; `make` is not required at all.

## The three modes

Pick from what the user gave you — don't ask which mode this is.

| The user gave you                                     | Mode            | Worktree name from         | `--prompt`                                    |
| ----------------------------------------------------- | --------------- | -------------------------- | --------------------------------------------- |
| An issue/ticket number, or a URL to one               | **A — ticket**  | `issue-<N>-<slug>`         | `/teach issue #<N> — <title>`                 |
| A path to a written plan (cycle doc, spec, ADR, plan) | **B — plan**    | the doc's title            | `/subagent-driven-development <path>`         |
| Only context/description, no task and no doc          | **C — context** | 2–4 words from the context | their context + "don't start yet, wait"       |
| Only a name ("a worktree called spike-x")             | **C — context** | their name, kebab-cased    | none — nothing to carry, the coder opens idle |

Everything after step 1 is the same in all three modes.

## What context crosses over

The new session is a fresh Claude with its own context window and the same
tools you have. So the prompt carries **pointers, plus anything that exists
only in this conversation** — never a summary of something the new session can
read for itself:

- **Don't** paste the issue body: `/teach` fetches it with `gh issue view`,
  comments and all. Don't paste the plan's contents: the new session reads the
  file. A summary you write is lossy, and the session would then work from your
  paraphrase instead of the source.
- **Do** carry the user's own steer if they gave one — "only step 2", "skip the
  ADRs", "the bug is probably in the redis client". That is not in the issue or
  the doc, so dropping it loses it for good. One short clause appended to the
  prompt, in their words.

**The prompt is one single-line string.** devgeta shell-quotes it (apostrophes
are safe) and tmux types it into the pane's shell, so an embedded newline would
be typed as Enter and run a broken command. If what you want to hand over
doesn't fit on one line, it isn't a prompt — write it to a file and use mode B.

## Workflow

### 1. Resolve the input

#### Mode A — ticket or issue

```bash
gh issue view <N> --json number,title,state,assignees \
  -q '{number, title, state, assignees: [.assignees[].login]}'
```

- Name: `issue-<N>-<short-kebab-slug>` from the title. Keep it short (≤ 50
  chars total), lowercase, letters/digits/dashes only — the name becomes a git
  branch and a tmux window name.
- If the issue is assigned to someone other than the current user, still
  proceed, but mention it in your summary — they may be duplicating claimed
  work (many repos treat assignment as ownership).
- If the user gave a plain name instead of an issue ("a worktree called
  spike-x"), that is mode C with the name already chosen.
- The issue body is NOT copied into the prompt — `/teach` fetches it. Only a
  steer the user gave here and that the issue does not contain gets appended:
  `… — <title>. Focus on the mobile path.`

#### Mode B — a plan document

**Read the doc first.** It is what the new session will be executing, and two
things in it change what you do:

- Its title gives the worktree name. Use the title, not the filename's date
  prefix: `docs/plans/cycles/2026-08-05-review-loop.md` titled
  `# Cycle: Review loop — one command that…` → `review-loop`. Two to four
  words, kebab-case, ≤ 50 chars.
- Its status. If the doc is already marked Done, or is a draft with an
  unresolved "get approval before implementing" step, say so and confirm
  before creating anything — those docs are not ready to be executed.

The prompt path must be the path **as it will exist inside the worktree**, so
use the repo-relative path (`docs/plans/cycles/2026-08-05-review-loop.md`),
never the absolute path of the current checkout. The doc IS the context — do
not summarize it into the prompt. Append only what the user said here that the
doc does not say: `/subagent-driven-development <path> — start at step 2, the
ADRs are already written.`

**The doc must already exist on the base the worktree is cut from — check
before creating.** `dg wt create` bases a new branch on `origin/<default>` (on
`HEAD` only when the repo has no origin), _not_ on your current working tree.
So a doc that is uncommitted, or committed but not pushed, is simply not in the
new worktree, and the session opens on a file that isn't there:

```bash
DEF="$(git -C "$REPO" symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null || echo HEAD)"
git -C "$REPO" cat-file -e "$DEF:<doc-path>" 2>/dev/null && echo present || echo missing
```

`present` → proceed. `missing` → **stop before creating anything** and tell the
user the doc has to land on the base first, with the commands filled in:

```bash
git add <doc-path> && git commit -m "docs: plan <name>" && git push
```

Do not create the worktree and copy the file in afterwards. The coder is
launched by `dg wt create` itself, so any copy you do after it returns races
the session's first read — it would usually win and occasionally not, which is
worse than failing, because the failure mode is a confused session rather than
a clear error. Landing the doc on the base is the only ordering that is
actually guaranteed.

#### Mode C — context only

The user described something ("the redis cache timeouts started after the 7.2
upgrade, I want to poke at it") with no ticket and no plan. Derive a 2–4 word
kebab name from that context (`spike-redis-timeouts`).

That context lives only in this conversation — there is no issue and no doc for
the new session to read, so if you drop it, it is gone. Carry it as
orientation, explicitly not as a task:

```bash
--prompt 'Context for this worktree: <their context, one line, their words>. Do not start any work yet — wait for my next message.'
```

**Do not invent a task.** The "wait" clause is what keeps this mode honest;
without it the session runs off on work the user never asked for, which is
worse than passing nothing. If they gave nothing but a name ("a worktree called
spike-x"), there is no context to carry — omit `--prompt` entirely and let the
coder open idle.

### 2. Resolve the main checkout

`dg wt create` must act on the primary repo, not on a nested worktree the
current session may be sitting in. `git worktree list` always reports the
main checkout first:

```bash
REPO="$(git worktree list --porcelain | awk '/^worktree /{sub(/^worktree /,""); print; exit}')"
```

If the user wants this for a different repository, use its path instead.

### 3. Decide the init pane

**Every mode runs this probe, including mode A.** Not every repo has an init
target, and some have no `make` at all, so ask the question before creating
rather than letting a pane exist only to print an error:

```bash
make -C "$REPO" -n finit >/dev/null 2>&1 && echo has-finit || echo no-finit
```

- `has-finit` → two panes: the coder and `--pane 'make finit'`.
- `no-finit` → one pane: the coder alone. Pass no `--pane` flag at all. (This
  also covers hosts where `make` isn't installed: the probe just fails.)

Do not substitute a different bootstrap command when `finit` is absent —
running some other repo's `setup`/`bootstrap` target uninvited is a surprise,
not a convenience. If the user names their own init command, pass it as
`--pane '<their command>'`.

### 4. Create the environment

One command. Only `--prompt` differs per mode; `--pane 'make finit'` is
appended in every mode when — and only when — step 3 said `has-finit`.

```bash
# Mode A, repo with finit (coder + init pane)
dg wt create <name> --ai claude --repo "$REPO" \
  --prompt '/teach issue #<N> — <issue title>' --pane 'make finit'

# Mode A, repo without finit (coder only)
dg wt create <name> --ai claude --repo "$REPO" \
  --prompt '/teach issue #<N> — <issue title>'

# Mode B
dg wt create <name> --ai claude --repo "$REPO" \
  --prompt '/subagent-driven-development <repo-relative doc path>' --pane 'make finit'

# Mode C
dg wt create <name> --ai claude --repo "$REPO" \
  --prompt 'Context for this worktree: <their context>. Do not start any work yet — wait for my next message.' \
  --pane 'make finit'
```

devgeta creates the worktree + branch (in-repo config: under
`<repo>/.claude/worktrees/<name>`), the tmux window `wt-<repo>-<name>`, and
switches the user's tmux client to it. It adopts an existing branch of the
same name; if that branch is checked out in the main clone, devgeta moves the
main clone to the default branch first and says so.

### 5. Report back

Tell the user, concisely:

- The tmux window (`wt-<repo>-<name>`) and worktree path. If they were inside
  tmux, devgeta already switched them there; otherwise tell them which session
  to attach to (named after the repo).
- What the coder started on: the `/teach` explanation (A), the plan it is
  implementing plus its one-line objective (B), or the context it is holding
  while it waits (C).
- The init pane — `make finit`, or that the repo has no `finit` target so no
  init pane was added.
- Any ownership warning from mode A.

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
- **Plan doc is wrong, gone, or not yet on the base** (mode B): stop before
  creating and say which. A worktree whose coder opens on a plan that isn't
  there burns the session's first minutes on a file-not-found loop, and the
  cause (the doc is only in your working tree) is not something that session
  can work out for itself.
- **`make finit` fails inside the pane** (missing dependencies, a secrets or
  environment check the repo runs at init): that output is visible to the user
  in the pane, which is the intended behavior — report it if you notice it,
  but it is not this skill's failure.

## Cleanup

When the user is done with an environment created here: `dg wt rm` (interactive
picker) removes the worktree and its window together. Mention it only if they
ask how to tear down.
