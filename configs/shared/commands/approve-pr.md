---
description: Verify a PR's review feedback was addressed, then approve it or report what's still blocking. The final-approver step after a review — use when deciding whether to approve a PR that has already been reviewed.
---

Confirm that the feedback on an already-reviewed PR was actually addressed, then approve it — or report what still blocks the merge.

## Authority to post

Running this command **is** the authorization to post. Once you have decided, post the verdict yourself — `devgeta task approve-pr` to approve, `devgeta task comment-pr` for the note when something still blocks. **Do not ask the user to confirm, do not show the body for approval first, and do not stop at "shall I approve?".** The user asked for the approval decision to be made and posted; pausing to check is the failure here, not the safeguard.

This authorizes _posting without asking_, nothing else. Every gate below still holds: a live blocker still means no approval, and approving over open non-blocking comments still rests on a reviewer agent's `APPROVE` verdict already in this conversation.

## Usage

```
/approve-pr [PR_NUMBER]
```

The PR is resolved from the current branch unless you pass a number. The repo is the current working directory.

This is the **deciding-approver** step, not a review — the full review lives in `/review-pr`. Because concerns were already raised, read the threads first to confirm they were genuinely resolved before you put your name on the merge.

## Process

### 1. Find the PR

If a `PR_NUMBER` was given, use it (pass `--pr PR_NUMBER` below). Otherwise:

```bash
devgeta task current-pr
```

If it prints "No pull request found for the current branch.", stop and tell the user this branch has no PR. Do nothing else.

### 2. Confirm it's reviewable

```bash
devgeta task pr-view          # add --pr PR_NUMBER if you have one
```

Confirm the state is open. The `review:` line shows whether it already carries reviews — if it has none, say so and recommend `/review-pr` first rather than approving cold.

### 3. Check the gates

Read the open threads first:

```bash
devgeta task review-threads --state unresolved
```

"No unresolved review threads." means there is nothing to check here.

**An open thread is not automatically a blocker.** Authors routinely fix a comment and never click "Resolve", so the thread stays open even though the concern is gone. For each open thread, check whether the point was actually handled:

- Read the cited file and see whether the code now does what the comment asked. Locate the code with the thread's diff hunk, not its line number — lines shift when new commits land.
- An author reply that rejects the comment or explains why it doesn't apply also counts as handled.

If either holds, treat the thread as satisfied and note it in the report ("addressed, thread left open"). **Never withhold approval over the resolve button, and never ask the author to go resolve a thread whose concern is already handled.**

**Unaddressed is not the same as blocking.** A thread whose concern is still live in the code blocks approval only when the concern itself is a blocker — something that breaks correctness, security, or data integrity. Bots and reviewers routinely leave suggestions, style points, and nits that nobody acted on; those are worth passing along, not worth holding a merge over. Sorting by "did anyone touch it" instead of "does it matter" is how a PR ends up parked over a Copilot nit.

So triage each live thread into one of two buckets:

- **Blocker** — the code is wrong, unsafe, or loses data. Do not approve.
- **Non-blocking** — a suggestion, nit, style preference, or optional improvement. Approve, and name it.

Approving over live non-blocking comments takes one thing beyond your own read: **a reviewer agent verdict of `APPROVE` already in this conversation** — from a `code-reviewer`/`document-reviewer`/`skill-reviewer` run, or another model's review sitting in context. That verdict is the judgment that the code has no blockers; it is what you are standing on when you say so on the PR. Without it you have no such basis — say the PR needs `/review-pr` first rather than approving over comments you haven't independently judged.

Then confirm the resolved ones were actually fixed, not just replied to and forgotten:

```bash
devgeta task review-threads --state resolved
```

Skim these; only open a file to verify when a resolution looks doubtful. Trust GitHub's resolution state as the primary signal — don't re-litigate the whole diff.

Finally, look at CI — but treat it as a signal, not a gate:

```bash
devgeta task pr-checks
```

A failing or errored check is often flaky, an unrelated job, or otherwise still valid, so it does **not** by itself block approval. Flag it in the report so the user can judge; don't let it decide.

### 4. Decide

**Write plainly.** Anything posted to the PR — the approval body or a comment — must be understandable by any engineer, including a junior one: everyday words, short sentences, no fancy vocabulary or filler.

**Approve when both gates hold:** the PR is open, and nothing live is a blocker — a thread's concern is either handled (whether or not it's marked resolved) or non-blocking. Failing checks are noted, not blocking.

```bash
devgeta task approve-pr --body "<body picked below>"
```

**The body must match what actually happened on this PR — never thank the author for addressing feedback that was never given.** Pick by situation:

| Situation                                                       | Body                                                                                                      |
| --------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| No feedback was ever raised (no threads, no prior review notes) | `LGTM.` — plain, nothing more                                                                             |
| Feedback was raised and addressed                               | `LGTM. Thanks for working on the suggestions 🔥` — vary the phrasing, keep it one line                    |
| Comments are still open but none of them block                  | `LGWC; <who> left some comments worth addressing — I don't see anything blocking.` — **name the authors** |

Name the people (and bots) whose comments are still open — the `review-threads`
output gives you each commenter's login. Naming them is the whole point of the
line: the author learns whose feedback to go read, and nobody has to guess what
"with comments" refers to. Examples:

```
LGWC; Copilot left some comments worth addressing — I don't see anything blocking.
LGWC; Copilot and @maria left a few suggestions — worth a look, but nothing blocking.
```

Keep it to one line, and never dress a real blocker up as a comment.

Don't paste the gate summary or per-thread detail into the PR — that belongs in the report to the user, not the review. If checks are red, mention it in one short clause (e.g. "LGTM — CI has a failing job worth a look") rather than withholding approval.

**If a real gate blocks** (a live concern that is a blocker, or a resolution that doesn't hold up), do **not** approve. Report it to the user; the author can clear it with `/address-feedback`. If a note on the PR is warranted, post one terse comment naming the concern itself — never "please resolve the threads":

```bash
devgeta task comment-pr --body "<one short line naming what's left>"
```

## Output

Return only this terse summary to the user — keep it out of the PR itself:

```
## PR #<num> — <approved | not approved>

- threads: <all handled | N open but addressed | N open, non-blocking (from <who>) | N blocking>
- checks: <passing | N failing (flagged, non-blocking)>
- reviews: <present | none>
- verdict in context: <reviewer agent APPROVE | none>

<if not approved: a short bullet per blocker>
```

## Notes

- This command never edits code and never runs a full review — that's `/review-pr`.
- Post the verdict yourself, without asking — see "Authority to post" above. Reporting the decision back and waiting for a go-ahead leaves the PR exactly where it started.
- Severity decides, not activity. A thread blocks when its concern is a blocker and still live in the code. A thread whose concern was fixed is not a blocker, and neither is an untouched suggestion or nit — approve with `LGWC` and name who left it. Resolving threads is the author's bookkeeping, not an approval gate.
- Approving over live comments rests on a reviewer agent's `APPROVE` verdict in context. Without one, don't approve over them — point at `/review-pr` instead.
- Failing CI is flagged, not a blocker — the user decides what to do about it.
- Both the approval and any comment stay terse; the detail goes to the user, not the PR.
