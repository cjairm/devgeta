---
description: Review a PR and post one cohesive review with inline comments — apply findings already in context (from a code/doc reviewer agent or another model) or review directly, dedup against existing threads, and submit a single verdict. Use for "review this PR", code review, or doc review.
---

Post review feedback to a PR as **one cohesive review**. Findings often already sit in the conversation — produced by a `code-reviewer`/`document-reviewer`/`skill-reviewer` agent or another model (gpt, qwen, kimi, …). Use those; if context is thin, review directly with the lens below. The repo is the current working directory.

## Authority to post

Running this command **is** the authorization to post. Once the review is composed, submit it yourself with `devgeta task submit-review` — and post any follow-up note with `devgeta task comment-pr` — straight away. **Do not ask the user to confirm, do not show the draft for approval first, and do not stop at "ready to post?".** The user asked for a review to be posted on the PR; pausing to check is the failure here, not the safeguard.

This authorizes _posting without asking_, nothing else. The verdict is still yours to judge on the evidence, and the dedup (step 3) and per-finding re-verification (step 5) still run before anything is posted.

## Usage

```
/review-pr [PR_NUMBER] [--base <merge-base-sha>] [--target <head-sha>]
```

The PR is resolved from the current branch unless you pass a number.

### `--target <head-sha>` — reviewing code that isn't checked out

Pass `--target` when the PR's code is not in the working tree: someone else's PR, a fork's PR, or any PR reviewed from an unrelated branch. It names the commit this review judges. **Without `--target`, everything below works exactly as it always has** — the working tree is the source, and nothing in this file changes.

`--target` comes with `--base`. The PR's diff is `<merge base>..<head>`, and a merge base cannot be worked out from a head alone, so both shas are passed in. **Given `--target` without `--base`, stop and say you need the base sha.** Do not guess one, and do not fall back to the checked-out branch's diff — that diff describes different code, so the review would be posted against the wrong changes.

With `--target`, three things change and nothing else:

1. **Resolve it first — and stop if it doesn't resolve.**

   ```bash
   git rev-parse --verify <head-sha>^{commit}
   ```

   If that fails, stop and tell the user this clone doesn't have that commit (usually it was never fetched). **Never fall back to reading the working tree.** The tree holds different code, so a review of it would be posted as a review of the PR — findings about files the PR never touched, and real findings dropped because the tree lacks the file.

2. **Every read of repo content resolves at that commit.** `git show <head-sha>:<path>` instead of opening the path on disk, and `git log <head-sha> -- <path>` instead of `git log <path>`. Same checks, same dedup rules, same verdict rules — only where the bytes come from changes. Step 2's `review-scope` and `branch-diff` describe the checked-out branch, so they don't apply here; the diff comes from `devgeta task review-package <base-sha> <head-sha>` instead — one call giving the commit list, the noise-filtered stat table, and the full diff of the reviewed range, which is also what tells you which lines can carry an inline comment in step 5. The two shas are the ones you were handed: `<base-sha>` is the `--base` value, `<head-sha>` the `--target` value. Never invent a base — the wrong one turns other people's commits into findings against this PR.

   **`--base` must be the merge base of the PR, not the tip of the base branch.** The tip looks close enough and is not: when the base branch moves on after the PR opens — the normal case on an active repo — a tip-based diff shows everything merged since as if this PR reverted it. Those read as real findings against work the author never touched. `devgeta task pr-review-target --pr <n>` prints the correct value on its `base:` line, and that value is already a merge base, so that is where a caller gets one.

3. **The submit names the commit** — add `--commit <head-sha>` to the `devgeta task submit-review` call in step 6.

Be clear on what that anchor does: it is **attribution, not a lock**. GitHub accepts a review whose `commit_id` is behind the PR's current head — this API has no atomic submit, so nothing here prevents the author pushing while you review. What it buys is that the posted review names the commit it actually judged: inline comments hang off that diff (GitHub marks them outdated once the head moves), the review record shows the sha, and branch protection's dismiss-stale-approvals has a sha to key off. A review that lands late is visibly stamped with the commit it read instead of silently claiming the new head.

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

Read the PR's purpose first — the description and linked ticket — before any code. Gather the findings already in the conversation. If there are none, review the change yourself with the lens in step 4. For a locally checked-out branch, run `devgeta task review-scope` for the orientation (branch, ahead/behind, commits, per-file stats), then `devgeta task branch-diff` (or `--file <path>` for one file) for the full noise-filtered diff. `review-scope` does a read-only fetch of origin and must run first, so the diff reflects current remote state — never `git pull` or merge, which would mutate the branch under review. With `--target`, neither of those two commands applies — follow the `--target` rule above instead, where the diff comes from `devgeta task review-package <base-sha> <head-sha>`.

### 3. Fetch existing threads and dedup — never repeat addressed feedback

```bash
devgeta task review-threads --state all
```

This returns three surfaces: inline review threads (resolved and unresolved), a "## Review summaries" section (submitted review bodies), and a "## Conversation" section (top-level PR comments). All prior feedback lives in one of these three — dedup a finding against all of them, not just inline threads.

**Dedup decides what you post. It never decides the verdict.** Dropping a finding here means "someone already said this, so I won't say it twice" — it never means "this is handled". Every finding you drop keeps its severity and goes into step 6's verdict exactly as if you had posted it: a `[CRITICAL]` dropped as a duplicate is still a `[CRITICAL]` standing against the PR. Keep the dropped findings and their severities in front of you; step 6 walks them again.

Drop a finding when ANY of these hold:

- An existing thread or prior review already makes substantially the same point AND is **resolved** — resolved means handled; re-raising it is noise.
- An existing **OPEN** thread already makes substantially the same point AND the code now does what that thread asked — a thread staying open means nobody clicked "Resolve", not that the concern is live. Check the current file (locate the code by the thread's diff hunk, not its line number; with `--target`, read it with `git show <head-sha>:<path>`) and drop the finding if it's already fixed.
- An existing **OPEN** thread already makes substantially the same point AND the author **replied rejecting it or explaining why it doesn't apply** — treat that as settled and drop it, UNLESS the code has changed since that reply in a way that makes their reasoning no longer hold (only then re-raise it, and say why in the finding). Judge "changed since" primarily from the thread header's `(outdated)` marker (GitHub's own signal that the anchored code has since changed); `review-scope`'s commit lines already carry each commit's date, so for the branch's own commits you can compare the reply timestamp against those directly — no need for a separate git call. Its dates cover the whole branch, not one path, though, so when a thread isn't marked outdated but you suspect only the surrounding code (not the branch as a whole) moved, fall back to `git log <path>` for a path-scoped timestamp (with `--target`, `review-scope` doesn't apply — `review-package`'s commit list carries the same dates, and the path-scoped fallback is `git log <head-sha> -- <path>`).
- The same point already appears in a review summary body or a conversation comment. Don't restate it — but note that this bullet, unlike the three above it, says nothing about the concern being handled. Someone raised it; nobody said it was fixed. Its severity carries into step 6 untouched.

Match on the finding's **identity, not its location**: the file plus the specific code construct plus the concern being raised, using the diff hunk shown in the thread — NOT the line number. Line numbers shift when new commits are pushed, so a `path:line` match misses the same finding after it moves to a new line. Two findings are "the same point" when they flag the same problem in the same code, regardless of the current line number or exact wording.

Keep a count of what you skipped for the summary.

A finding produced by a reviewer agent carries a journal id — `(n4)` next to its location — because the reviewer opens one for every blocking finding. When you drop such a finding here **because the concern is genuinely handled** — a resolved thread, code that now does what the thread asked, or an author reply that settles it — settle its entry with that reason:

```bash
devgeta task review-note --settle --id n4 --as answered --note "already handled in a resolved thread on PR #123"
```

Otherwise the journal keeps an entry the PR has already closed, and the next review raises it again — the exact loop the journal exists to break. Findings you do post stay open; `/address-feedback` settles them when the author responds.

**Never settle an entry you dropped only because someone else raised the same still-live point.** Leave it open. `answered` tells the next review the concern is gone, so settling it there deletes a live blocker from the only record that tracks it — and it is the same mistake as letting the drop change the verdict, one layer down.

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

**Re-verify each finding against the current file before anchoring it.** Read the cited file — don't trust the finding's quoted snippet; with `--target`, read it as `git show <head-sha>:<path>` rather than from disk — and confirm the code actually exists at (or near) the cited `file:line`. If the line drifted, re-anchor to where the code is now; if that new location isn't in the diff, it can't take an inline comment (see the note below), so move the finding to the body's "General notes" instead. If the cited code is gone or was never there — a hallucinated or already-resolved finding, common when findings come from another model — drop it.

**Write plainly.** Everything posted must be understandable by any engineer, including a junior one: everyday words, short sentences, no fancy vocabulary or filler. Each comment says what's wrong, why it matters, and the fix — nothing more.

Allocate a scratch directory for this review's files — one call, reused for both:

```bash
SCRATCH=$(devgeta task scratch)
```

**Body** — GitHub-Flavored Markdown, written to a scratch file (`"$SCRATCH/review.md"`); pass it with `--body-file` so backticks and apostrophes survive:

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

**Inline comments** — write a JSON array to a scratch file (`"$SCRATCH/comments.json"`). Each entry anchors to a diff line; only lines present in the diff can carry one. Lead the body with the severity tag:

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

- **Reflect the current state of the PR.** Review against the latest commit/diff, not a revision you looked at earlier. If new commits landed while you were reviewing, recheck that your findings still apply and drop any that a later commit already resolved — this is what the step 5 re-verification check is for; if you haven't run it since the latest commits landed, do it now. With `--target` the reviewed revision is fixed instead: judge against `<head-sha>`, and let `--commit` say so on the posted review rather than quietly re-pointing it at a newer head.
- **Credit prior reviewers, don't echo them.** If a finding you're keeping matches a point a prior reviewer already raised (kept per the step 3 dedup rules — new evidence or a different angle), say so and credit them instead of restating it as new.

Post the body and the inline comments together as a single review, choosing the verdict:

| Verdict         | When                                                                                                | `--event`         |
| --------------- | --------------------------------------------------------------------------------------------------- | ----------------- |
| Request changes | A live blocker that you are the one raising                                                         | `request-changes` |
| Comment         | A live blocker someone else already raised (case 1 below), or suggestions only with no verdict cast | `comment`         |
| Approve         | No live blocker; leaves the codebase healthier — including when non-blocking comments stand         | `approve`         |

**Judge the verdict on every finding, including the ones dedup dropped.** Before picking, walk the step 3 drop list once more and ask of each: is this concern still live in the code, and is it a blocker? Only two things ever matter — **severity** and **is it still live**. Who raised it, whether it was raised before, and whether you are posting it are all irrelevant to the verdict.

Three cases decide it:

**1. A live blocker someone else already raised → do NOT approve.** Copilot, another bot, or a human reviewer flagged something that breaks correctness, security, or data integrity, and the code still has it. Don't post the finding again, and don't stack a second `request-changes` on the review that already asked for it. Submit `--event comment` with a body that says the rest looks good, names the one outstanding item, and commits to approving once it's addressed:

```
Everything looks good on my side except one thing: <the blocker in one line> — already flagged by <who>. I'll approve once that's addressed.
```

This is the case that goes wrong. The finding gets dropped as a duplicate in step 3, never gets counted in step 6, and a PR with a live blocker gets approved. **"Already raised" means don't say it twice. It never means it stopped blocking.**

**2. A non-blocking comment or suggestion someone else left → LGWC: approve.** A suggestion, nit, style point, or optional improvement nobody acted on. Untouched is not the same as blocking. Approve, and name who left it so the author knows whose feedback to go read — it is still worth doing, just not worth holding the PR over:

```
LGWC; Copilot left some comments worth addressing — I don't see anything blocking.
LGWC; Copilot and @maria left a few suggestions — worth a look, but nothing blocking.
```

**3. Failing CI is `/approve-pr`'s call, not this command's.** This command doesn't fetch check status, so a red check you heard about second-hand never decides the verdict here. `/approve-pr` runs `devgeta task pr-checks` and treats a failing check as flagged-not-blocking — an `LGWC` approve that names the check.

Open the body's Summary with `LGWC; <one short clause>` whenever you approve with comments standing. A clean approve with no comments opens with `LGTM.` instead.

Withhold approval only for a concern that is a blocker and still live in the code — whoever raised it, and whether or not you are the one posting it.

`--event` carries the verdict you just picked; there is no default, so substitute it rather than copying a value. Case 1 posts `comment` — `request-changes` is only for a blocker you are the first to raise, and posting it on a blocker someone else already flagged is the stacked review case 1 forbids.

```bash
devgeta task submit-review \
  --event <approve|request-changes|comment> \
  --body-file "$SCRATCH/review.md" \
  --comments-file "$SCRATCH/comments.json"      # omit when there are no inline findings
```

Add `--pr PR_NUMBER` when you resolved a number in step 1. With `--target`, add `--commit <head-sha>` too, so the posted review names the commit it judged. The review posts atomically — one notification, all inline comments grouped under it.

**Clean up on every exit path, not just the happy one:**

```bash
devgeta task scratch --clean "$SCRATCH"
```

Run this once you are done with the directory — after a successful submit, and equally after a failed one or any early exit (you abandon the review, the PR turns out to be already handled, submit errors out). `--clean` is idempotent, so running it twice is safe and running it after a partial failure is safe.

If submit failed, **print the review body and any inline comments into your reply before cleaning up**, so nothing is lost and the user can post it by hand. Do not leave the directory behind as the backup — a stale scratch directory is only swept during `dg configure --force`, so "I'll leave it there just in case" means it sits around indefinitely.

**Re-review with nothing new to add** — split by whether prior feedback is actually settled. Judge that from the code and the replies (with `--target`, the code is `git show <head-sha>:<path>`), **not** from GitHub's resolved flag: an open thread whose point was fixed counts as addressed, and an unclicked "Resolve" button is never a reason to hold a PR.

- **Every prior thread's concern was addressed and you have no new findings → approve.** Don't post a comment saying "nothing to add" — a comment doesn't dismiss a prior request-changes review, so it leaves the PR blocked for no reason. Don't ask the author to resolve threads either. Submit `--event approve` with a one-line body that matches what actually happened: if feedback was raised and addressed, acknowledge it warmly ("LGTM. Thanks for working on the suggestions 🔥" — vary the phrasing); if nothing was ever raised, plain `LGTM.` — never thank the author for addressing feedback that was never given.
- **A prior concern is still live in the code but doesn't block → approve with `LGWC`, naming who raised it.** An open suggestion or nit from a bot or another reviewer is worth passing along, not worth holding the PR over.
- **A prior concern is still live in the code and is a real blocker → don't approve** (case 1 above). Flag the concern itself in one brief `comment-pr` — the rest looks good, this one item is outstanding, you'll approve once it's addressed — rather than re-listing each thread or asking for resolutions.

## Output

Return a terse summary to the user:

```
## Review posted to PR #<num> — <request changes | approve | comment>

- findings: <N posted>
- skipped: <M not posted (already addressed, or the same point already raised elsewhere)>
- live blockers not posted: <K, and who raised each — none is the normal case>

<PR URL>
```

## Notes

- This command never edits code. It reads, then posts exactly one review.
- Post it yourself, without asking — see "Authority to post" above. Coming back with a draft and a "want me to post this?" is not a safe default here; it is the command failing to do its job.
- Invoke the `devgeta` binary only — never a `dg` alias, `go run`, or a local build. Only the installed binary is available in this environment.
- **Dedup is mandatory**: never duplicate a finding already raised. Treat a resolved thread as handled, and treat an open thread as handled too once the code does what it asked, or the author replied rejecting it or explaining why it doesn't apply — unless the code changed since in a way that reopens the concern. A thread being open only means nobody clicked "Resolve".
- **Dedup suppresses duplicate comments only — it never softens the verdict, and it never settles a journal entry that is still live.** A blocker you didn't post because someone else got there first still blocks: don't approve, post the case 1 comment, and leave its journal entry open.
- A line that isn't part of the diff can't take an inline comment — move that finding to the body's "General notes" instead.

## References

- Google Engineering Practices — the standard of code review & what to look for: https://google.github.io/eng-practices/review/reviewer/
- "Software Engineering at Google", Code Review chapter: https://abseil.io/resources/swe-book/html/ch09.html
