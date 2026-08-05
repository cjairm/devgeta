---
description: Review a PR and post one cohesive review with inline comments — apply findings already in context (from a code/doc reviewer agent or another model) or review directly, dedup against existing threads, and submit a single verdict. Use for "review this PR", code review, or doc review.
temperature: 0.1
permission:
  write: allow
  edit: deny
  bash:
    "*": deny
    "devgeta task *": allow
    "git diff*": allow
    "git fetch*": allow
    "git log*": allow
    "git branch*": allow
---

Post review feedback to a PR as **one cohesive review**. Findings often already sit in the conversation — produced by a `code-reviewer`/`document-reviewer`/`skill-reviewer` agent or another model (gpt, qwen, kimi, …). Use those; if context is thin, review directly with the lens below. The repo is the current working directory.

## Usage

```
/review-pr [PR_NUMBER]
```

The PR is resolved from the current branch unless you pass a number.

## Process

### 1. Find the PR

If a `PR_NUMBER` was given, use it (pass `--pr PR_NUMBER` below). Otherwise:

```bash
devgeta task current-pr
```

If it prints "No pull request found for the current branch.", stop and tell the user this branch has no PR.

### 2. Load context

```bash
devgeta task pr-view          # add --pr PR_NUMBER if you have one
```

Read the PR's purpose first — the description and linked ticket — before any code. Gather the findings already in the conversation. If there are none, review the change yourself with the lens in step 4. For a locally checked-out branch, run `devgeta task review-scope` for the orientation (branch, ahead/behind, commits, per-file stats), then `devgeta task branch-diff` (or `--file <path>` for one file) for the full noise-filtered diff. `review-scope` does a read-only fetch of origin and must run first, so the diff reflects current remote state — never `git pull` or merge, which would mutate the branch under review.

### 3. Fetch existing threads and dedup — never repeat addressed feedback

```bash
devgeta task review-threads --state all
```

This returns three surfaces: inline review threads (resolved and unresolved), a "## Review summaries" section (submitted review bodies), and a "## Conversation" section (top-level PR comments). All prior feedback lives in one of these three — dedup a finding against all of them, not just inline threads.

Drop a finding when ANY of these hold:

- An existing thread or prior review already makes substantially the same point AND is **resolved** — resolved means handled; re-raising it is noise.
- An existing **OPEN** thread already makes substantially the same point AND the author **replied rejecting it or explaining why it doesn't apply** — treat that as settled and drop it, UNLESS the code has changed since that reply in a way that makes their reasoning no longer hold (only then re-raise it, and say why in the finding). Judge "changed since" primarily from the thread header's `(outdated)` marker (GitHub's own signal that the anchored code has since changed); `review-scope`'s commit lines already carry each commit's date, so for the branch's own commits you can compare the reply timestamp against those directly — no need for a separate git call. Its dates cover the whole branch, not one path, though, so when a thread isn't marked outdated but you suspect only the surrounding code (not the branch as a whole) moved, fall back to `git log <path>` for a path-scoped timestamp.
- The same point already appears in a review summary body or a conversation comment.

Match on the finding's **identity, not its location**: the file plus the specific code construct plus the concern being raised, using the diff hunk shown in the thread — NOT the line number. Line numbers shift when new commits are pushed, so a `path:line` match misses the same finding after it moves to a new line. Two findings are "the same point" when they flag the same problem in the same code, regardless of the current line number or exact wording.

Keep a count of what you skipped for the summary.

A finding produced by a reviewer agent carries a journal id — `(n4)` next to its location — because the reviewer opens one for every blocking finding. When you drop such a finding here, settle its entry with the reason you dropped it:

```bash
devgeta task review-note --settle --id n4 --as answered --note "already handled in a resolved thread on PR #123"
```

Otherwise the journal keeps an entry the PR has already closed, and the next review raises it again — the exact loop the journal exists to break. Findings you do post stay open; `/address-feedback` settles them when the author responds.

### 4. The review lens (high-leverage first — order matters)

Governing principle: **approve when the PR leaves the codebase healthier than without it**, not when it's "perfect". The question is "is the codebase better merged than not?", not "would I have written it this way?". If the PR is huge, the first finding is to split it — small PRs get genuinely reviewed; large ones get rubber-stamped.

Work the passes in this order so design problems surface before you nitpick code that shouldn't exist:

1. **Design** — does it belong here, fit existing patterns, sit at the right abstraction? Flag over-engineering (generality/features not needed now).
2. **Functionality** — does it do what it claims on the unhappy paths too? Edge cases, nulls, empty inputs, boundaries, downstream failures, concurrency.
3. **Complexity** — too complex = can't be understood quickly, or invites bugs on the next edit. If you can't follow it, others won't either.
4. **Tests** — real coverage of the new logic and edge cases; would they fail if the logic broke?
5. **Naming / comments / docs** — names convey intent; comments explain _why_; update READMEs when behavior changes.
6. **Style** — last and lightest. Prefix optional style points with `Nit:`; never block on personal preference.

Across every pass, a **security lens** for anything touching data, auth, or external input: input validation, authz, injection (SQL/XSS), committed secrets, and the safety of new dependencies.

For a **doc review**, swap passes 2–4 for: accuracy, completeness, structure, and clarity.

Severity tags drive the verdict: `[CRITICAL]` (data loss, security, correctness — a blocker), `[IMPORTANT]`, `[MINOR]`/`[Nit]`. Anchor disagreement on engineering principle, not authority — and call out what's genuinely good, especially well-addressed prior feedback.

### 5. Compose the review — a summary body plus inline comments

Findings that point at a specific line become **inline comments** anchored to the diff; everything else goes in the summary **body**.

**Re-verify each finding against the current file before anchoring it.** Read the cited file — don't trust the finding's quoted snippet — and confirm the code actually exists at (or near) the cited `file:line`. If the line drifted, re-anchor to where the code is now; if that new location isn't in the diff, it can't take an inline comment (see the note below), so move the finding to the body's "General notes" instead. If the cited code is gone or was never there — a hallucinated or already-resolved finding, common when findings come from another model — drop it.

**Write plainly.** Everything posted must be understandable by any engineer, including a junior one: everyday words, short sentences, no fancy vocabulary or filler. Each comment says what's wrong, why it matters, and the fix — nothing more.

**Body** — GitHub-Flavored Markdown, written to a scratch file (`/tmp/review.md`); pass it with `--body-file` so backticks and apostrophes survive:

```markdown
## Summary

<!-- 1–2 lines: does this improve code health? -->

## Strengths

<!-- What's done well. Don't skip this. -->

## General notes

<!-- Findings with no single line to anchor to, and any cross-cutting concern. -->

## Questions

<!-- Anything you need the author to clarify. -->

---

<!-- footer when applicable: "Skipped N finding(s) already addressed (resolved threads, author replies, review summaries, or conversation comments)." -->
```

**Inline comments** — write a JSON array to a scratch file (`/tmp/comments.json`). Each entry anchors to a diff line; only lines present in the diff can carry one. Lead the body with the severity tag:

```json
[
  {
    "path": "internal/client.go",
    "line": 42,
    "body": "**[CRITICAL]** Missing error handling — a nil response here panics. Guard before dereferencing."
  },
  {
    "path": "internal/client.go",
    "start_line": 60,
    "line": 65,
    "body": "**[Nit]** This block reads more clearly as an early return."
  }
]
```

`line` is the line in the file (right side of the diff); add `start_line` for a multi-line range. Drop any finding already covered by an existing thread, review summary, or conversation comment (step 3).

### 6. Submit one review

**Before you submit:**

- **Reflect the current state of the PR.** Review against the latest commit/diff, not a revision you looked at earlier. If new commits landed while you were reviewing, recheck that your findings still apply and drop any that a later commit already resolved — this is what the step 5 re-verification check is for; if you haven't run it since the latest commits landed, do it now.
- **Credit prior reviewers, don't echo them.** If a finding you're keeping matches a point a prior reviewer already raised (kept per the step 3 dedup rules — new evidence or a different angle), say so and credit them instead of restating it as new.

Post the body and the inline comments together as a single review, choosing the verdict:

| Verdict         | When                                       | `--event`         |
| --------------- | ------------------------------------------ | ----------------- |
| Request changes | Any `[CRITICAL]` / blocking issue          | `request-changes` |
| Approve         | No blockers; leaves the codebase healthier | `approve`         |
| Comment         | Suggestions only, nothing blocking         | `comment`         |

When approving **with** comments the author should look at before merging (non-blocking, but worth addressing), open the body's Summary with `LGWC; <one short clause>`. A clean approve with no comments opens with `LGTM.` instead.

```bash
devgeta task submit-review \
  --event request-changes \
  --body-file /tmp/review.md \
  --comments-file /tmp/comments.json      # omit when there are no inline findings
```

Add `--pr PR_NUMBER` when you resolved a number in step 1. The review posts atomically — one notification, all inline comments grouped under it.

**Re-review with nothing new to add** — split by whether prior feedback is actually settled:

- **Every prior thread was addressed and you have no new findings → approve.** Don't post a comment saying "nothing to add" — a comment doesn't dismiss a prior request-changes review, so it leaves the PR blocked for no reason. Submit `--event approve` with a one-line body that matches what actually happened: if feedback was raised and addressed, acknowledge it warmly ("LGTM. Thanks for working on the suggestions 🔥" — vary the phrasing); if nothing was ever raised, plain `LGTM.` — never thank the author for addressing feedback that was never given.
- **Unresolved threads remain unaddressed and that's the main issue → don't approve.** Flag it in one brief `comment-pr` rather than re-listing each thread.

## Output

Return a terse summary to the user:

```
## Review posted to PR #<num> — <request changes | approve | comment>

- findings: <N posted>
- skipped: <M already addressed (resolved, replied-to, or raised elsewhere)>

<PR URL>
```

## Notes

- This command never edits code. It reads, then posts exactly one review.
- Invoke the `devgeta` binary only — never a `dg` alias, `go run`, or a local build. Only the installed binary is available in this environment.
- **Dedup is mandatory**: never duplicate a finding already raised. Treat a resolved thread as handled, and treat an open thread as handled too once the author replied rejecting it or explaining why it doesn't apply — unless the code changed since in a way that reopens the concern.
- A line that isn't part of the diff can't take an inline comment — move that finding to the body's "General notes" instead.

## References

- Google Engineering Practices — the standard of code review & what to look for: https://google.github.io/eng-practices/review/reviewer/
- "Software Engineering at Google", Code Review chapter: https://abseil.io/resources/swe-book/html/ch09.html
