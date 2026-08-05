---
description: Address PR review feedback — implement suggestions, reply to reviewers, and resolve threads on the current branch's PR
temperature: 0.2
permission:
  write: allow
  edit: allow
  bash:
    "*": deny
    "devgeta task *": allow
    "git add *": allow
    "git commit *": allow
    "git push*": allow
    "git status": allow
    "git diff*": allow
    "go *": allow
    "npm *": allow
    "npx *": allow
    "pnpm *": allow
    "yarn *": allow
    "make *": allow
---

Address PR review feedback: implement the requested changes, reply to each reviewer thread, and resolve the ones you handled.

## Usage

```
/address-feedback [PR_NUMBER]
```

The PR is resolved from the current branch unless you pass a number. The repo is the current working directory.

## Process

### 1. Find the PR

If a `PR_NUMBER` was given, use it and pass `--pr PR_NUMBER` to every command below.

Otherwise resolve it from the branch:

```bash
devgeta task current-pr
```

If that prints "No pull request found for the current branch.", stop and tell the user this branch has no PR — they can open one with `/create-pr`. Do nothing else.

### 2. Read the unresolved threads

```bash
devgeta task review-threads --state unresolved   # add --pr PR_NUMBER if you have one
```

`--state unresolved` is the default, but pass it explicitly — these are the only threads to act on; resolved ones are already handled. The output is compact markdown. Each block is headed `## path:line (thread <id>)`, may include a diff hunk, then `**author** (comment-id): body`. The thread `<id>` is what you reply to and resolve.

Read the whole list before changing anything — a later comment can change how you handle an earlier one.

### 3. Load the repo's conventions

Before touching code, read the repo's entry-point docs so every change follows its established patterns:

1. `CLAUDE.md`, `AGENTS.md`, or `INSTRUCTIONS.md` — development practices, patterns, build/test/commit conventions.
2. `README.md` if none of those exist — basic project info and links to deeper docs.
3. Follow the links those files point to when they cover the area you're changing (e.g. a testing or error-handling guide).

These conventions govern how you implement every fix in the next steps.

### 4. Triage each thread

Sort every thread into one of these, then work in logical batches (not one commit per comment):

| Bucket        | What it is                                  | Action                                              |
| ------------- | ------------------------------------------- | --------------------------------------------------- |
| Fix           | A concrete change you agree with            | Implement it, reply, resolve                        |
| Already done  | Code already satisfies the comment          | Reply pointing to the code/commit, resolve          |
| Discuss       | Disagreement or a tradeoff worth explaining | Reply with reasoning, resolve unless it's blocking  |
| Out of scope  | Valid but not for this PR                   | Reply that you'll track it separately, resolve      |
| Needs clarity | You genuinely don't understand the ask      | Reply with a specific question — **do not resolve** |

**Verify before you change anything.** For each "Fix" thread, check the suggested change against two bars before writing code:

- **Repository patterns** — look at how the surrounding code already solves similar problems (and what the docs from step 3 say). Your change must match the codebase's existing patterns.
- **Industry best practices** — the suggestion should be sound engineering, not just what the reviewer typed. If it conflicts with a well-established practice or with the repo's own conventions, don't implement it blindly — move it to the Discuss bucket and explain the tradeoff in plain terms.

A reviewer comment is an input, not an order: agree with it by verifying it, not by default.

### 5. Implement, verify, and commit

Make the changes following the conventions from step 3, then verify and commit the same way — how this project builds, tests, and commits. Don't assume a language or toolchain.

Group related fixes into meaningful commits and push so reviewers can see what changed since their pass.

### 6. Reply and resolve

For each thread you handled, reply then resolve:

```bash
devgeta task reply-thread <thread-id> "Done in <short-sha> — <what changed>"
devgeta task resolve-thread <thread-id>
```

Use `--body-file <path>` instead of an inline body when the reply needs multi-line Markdown.

Reply to every thread, even trivial ones, so the reviewer never has to guess whether you saw it. Reference the commit when a fix landed there. Leave "needs clarity" threads open.

**Reply style — keep it simple.** Every reply must be readable by any engineer, including a junior one:

- Use plain, everyday words. No fancy vocabulary, no jargon unless the reviewer used it first.
- Short sentences. Say what you changed and why in one or two lines.
- No filler ("as per your suggestion", "I have proceeded to..."). Just: what changed, where, and the commit.
- If you disagree, explain the tradeoff in simple terms — no lecturing.

Good: `Done in a1b2c3d — moved the check before the loop so we don't hit the API on every item.`
Bad: `Per your astute observation, I have refactored the aforementioned logic to preemptively short-circuit redundant invocations.`

**Also record it in the branch's review journal**, so the next review — which may be a fresh session with no memory of this one — inherits the outcome instead of raising the same point again:

```bash
devgeta task review-notes                       # ids of anything already open
devgeta task review-note --settle --id n4 --as fixed --note "atomic rename added in a1b2c3d"
```

Use `--as fixed` for a fix, `--as answered` for a question you answered, and `--as rejected` when you pushed back — for a rejection, the note must carry **why**, because that reason is what a later reviewer re-reads before overriding it. For a point that was never opened as an entry, drop `--id` and the note is recorded directly.

This is the step that makes reviews converge. A thread resolved on GitHub only helps while the PR exists; the journal is what a reviewer reads on a branch with no PR, and in a session that never saw this conversation.

### 7. Re-request review

After all threads are handled and pushed, add each reviewer whose feedback you addressed back to the PR's reviewers list so GitHub asks them for a fresh review:

```bash
devgeta task request-review <reviewer1> [reviewer2 ...]   # add --pr PR_NUMBER if you have one
```

Get the reviewer usernames from the thread authors in step 2. Request every distinct human reviewer you replied to in one command; skip bot authors.

## Output

Return only this — no preamble, no narration:

```
## PR #<num> — feedback addressed

### Changes
| File | Change | Commit |
|------|--------|--------|
| ... | ... | <sha> |

### Threads
| Location | Bucket | Outcome |
|----------|--------|---------|
| path:line | Fix | replied + resolved |
| path:line | Needs clarity | asked, left open |

### Verification
<result of the repo's build/test command, or "n/a — no code changes">

### Re-review
Requested from: <reviewer1>, <reviewer2>
```

## Notes

- Resolve a thread only after you've actually addressed it; never resolve to clear noise.
- Don't make changes unrelated to the feedback.
- If a push or a resolve fails, report it and continue with the rest — your local commits are safe.
