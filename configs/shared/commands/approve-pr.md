---
description: Verify a PR's review feedback was addressed, then approve it or report what's still blocking. The final-approver step after a review — use when deciding whether to approve a PR that has already been reviewed.
---

Confirm that the feedback on an already-reviewed PR was actually addressed, then approve it — or report what still blocks the merge.

## Authority to post

Running this command **is** the authorization to post. Once you have decided, post the verdict yourself — `devgeta task approve-pr` to approve, `devgeta task comment-pr` for the note when something still blocks. **Do not ask the user to confirm, do not show the body for approval first, and do not stop at "shall I approve?".** The user asked for the approval decision to be made and posted; pausing to check is the failure here, not the safeguard.

This authorizes _posting without asking_, nothing else. Every gate below still holds: a live blocker still means no approval, and approving over open non-blocking comments still rests on a reviewer agent's `APPROVE` verdict already in this conversation.

## Usage

```
/approve-pr [PR_NUMBER] [--target <head-sha>]
```

The PR is resolved from the current branch unless you pass a number. The repo is the current working directory.

This is the **deciding-approver** step, not a review — the full review lives in `/review-pr`. Because concerns were already raised, read the threads first to confirm they were genuinely resolved before you put your name on the merge.

### `--target <head-sha>` — deciding on code that isn't checked out

Pass `--target` when the PR's code is not in the working tree: someone else's PR, a fork's PR, or any PR judged from an unrelated branch. It names the commit this approval is about. **Without `--target`, everything below works exactly as it always has** — the working tree is the source, and nothing in this file changes.

With `--target`, four things change and nothing else:

1. **Resolve it first — and stop if it doesn't resolve.**

   ```bash
   git rev-parse --verify <head-sha>^{commit}
   ```

   If that fails, stop and tell the user this clone doesn't have that commit (usually it was never fetched). **Never fall back to reading the working tree.** The tree holds different code, so a thread would be checked against something the PR never contained — which is an approval resting on nothing.

2. **Every read of repo content resolves at that commit** — `git show <head-sha>:<path>` instead of opening the path on disk. Same gates, same triage, same severity rules; only where the bytes come from changes.

3. **The approval names the commit** — add `--commit <head-sha>` to the `devgeta task approve-pr` call in step 4.

4. **Every call that touches the PR names it — the gate reads as much as the post.** `--pr <n>` is mandatory on every `devgeta task` command here that resolves a PR: `pr-view` in step 2, both `review-threads` calls and `pr-checks` in step 3, `approve-pr` in step 4, and a `devgeta task comment-pr` if a blocker means you post that instead. Without a number those commands resolve the PR from the checked-out branch, and with `--target` that branch is not this PR: an omitted number does not error. On the post it approves whatever PR the checkout happens to have open; on a read it is worse than it looks, because every gate in step 3 is then decided on another PR's evidence — its threads, its reviews, its CI — and an approval reached that way was never about this PR at all. So a number must be in hand here — if none was passed with `--target`, stop and ask for one rather than letting step 1 infer it from the branch.

Be clear on what that anchor does: it is **attribution, not a lock**. GitHub accepts a review whose `commit_id` is behind the PR's current head — this API has no atomic submit, so nothing here prevents the author pushing while you decide. What it buys is that the approval names the commit it was based on, and that branch protection's dismiss-stale-approvals has a sha to key off, so an approval of an older commit is visibly an approval of that commit rather than a silent claim about the new head.

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

With `--target`, `--pr PR_NUMBER` is not optional on that read — the checkout is not this PR, so an omitted number describes whatever PR the checkout branch has open, and both checks below (is it open, does it already carry reviews) would be answered about that other PR.

Confirm the state is open. The `review:` line shows whether it already carries reviews — if it has none, say so and recommend `/review-pr` first rather than approving cold.

### 3. Check the gates

Read the open threads first:

```bash
devgeta task review-threads --state unresolved      # add --pr PR_NUMBER if you have one
```

With `--target`, `--pr PR_NUMBER` is not optional on this read and on both that follow it — the checkout is not this PR, so an omitted number returns another PR's threads and checks, and the approval gets decided on gates that belong to a PR nobody asked about. "No unresolved review threads." from the wrong PR reads exactly like a clean bill of health for this one.

"No unresolved review threads." means there is nothing to check here.

**An open thread is not automatically a blocker.** Authors routinely fix a comment and never click "Resolve", so the thread stays open even though the concern is gone. For each open thread, check whether the point was actually handled:

- Read the cited file and see whether the code now does what the comment asked. Locate the code with the thread's diff hunk, not its line number — lines shift when new commits land. With `--target`, read it as `git show <head-sha>:<path>` rather than from disk.
- An author reply that rejects the comment or explains why it doesn't apply also counts as handled.

If either holds, treat the thread as satisfied and note it in the report ("addressed, thread left open"). **Never withhold approval over the resolve button, and never ask the author to go resolve a thread whose concern is already handled.**

**Unaddressed is not the same as blocking.** A thread whose concern is still live in the code blocks approval only when the concern itself is a blocker — something that breaks correctness, security, or data integrity. Bots and reviewers routinely leave suggestions, style points, and nits that nobody acted on; those are worth passing along, not worth holding a merge over. Sorting by "did anyone touch it" instead of "does it matter" is how a PR ends up parked over a Copilot nit.

So triage each live thread into one of two buckets:

- **Blocker** — the code is wrong, unsafe, or loses data. Do not approve.
- **Non-blocking** — a suggestion, nit, style preference, or optional improvement. Approve, and name it.

**Who raised it never enters the triage.** A blocker is a blocker whether it came from you, a reviewer agent, Copilot, or a human — and whether it was raised once or three times. "Someone else already flagged this" is a reason not to repeat the comment; it is never a reason to downgrade it to non-blocking. Sorting by who spoke instead of by what's wrong is how a live blocker gets approved over.

Approving over live non-blocking comments takes one thing beyond your own read: **a reviewer agent verdict of `APPROVE` already in this conversation** — from a `code-reviewer`/`document-reviewer`/`skill-reviewer` run, or another model's review sitting in context. That verdict is the judgment that the code has no blockers; it is what you are standing on when you say so on the PR. Without it you have no such basis — say the PR needs `/review-pr` first rather than approving over comments you haven't independently judged.

Then confirm the resolved ones were actually fixed, not just replied to and forgotten:

```bash
devgeta task review-threads --state resolved      # add --pr PR_NUMBER if you have one; not optional under --target
```

Skim these; only open a file to verify when a resolution looks doubtful (with `--target`, `git show <head-sha>:<path>` again). Trust GitHub's resolution state as the primary signal — don't re-litigate the whole diff.

Finally, look at CI — but treat it as a signal, not a gate:

```bash
devgeta task pr-checks      # add --pr PR_NUMBER if you have one; not optional under --target
```

A failing or errored check is often flaky, an unrelated job, or otherwise still valid, so it does **not** by itself block approval. Flag it in the report so the user can judge; don't let it decide.

### 4. Decide

**Write plainly.** Anything posted to the PR — the approval body or a comment — must be understandable by any engineer, including a junior one: everyday words, short sentences, no fancy vocabulary or filler. And nothing that only works here: no local tool commands, no review-journal ids, no local paths. The reader has none of that, so it reads as an instruction they cannot follow.

**Approve when both gates hold:** the PR is open, and nothing live is a blocker — a thread's concern is either handled (whether or not it's marked resolved) or non-blocking. Failing checks are noted, not blocking.

Three cases cover almost every PR that reaches here:

**1. An unresolved blocker someone else already raised → do NOT approve.** It doesn't matter that the point isn't yours, or that it's already sitting in a thread nobody acted on. Post one comment that says the rest looks good, names the outstanding item, and commits to approving once it's addressed — then stop:

```bash
devgeta task comment-pr --body "Everything looks good on my side except one thing: <the blocker in one line> — already flagged by <who>. I'll approve once that's addressed."
```

With `--target`, `--pr PR_NUMBER` is not optional on that call — the checkout is not this PR, so an omitted number posts the comment to whatever PR the checkout branch has open.

**2. A non-blocking comment or suggestion someone else left → LGWC: approve.** Approve and name who left it, so the author knows whose feedback to read. Say it's worth doing — you are passing it along, not dismissing it.

**3. Failing checks with no code blocker → LGWC: approve, and name the failing check.** A red check is a signal for the user to judge, not a gate (see step 3). Name the specific job so the author can go look; don't withhold approval over it.

To approve (cases 2 and 3, and every PR with nothing outstanding at all):

```bash
devgeta task approve-pr --body "<body picked below>"
```

Add `--pr PR_NUMBER` when you resolved a number in step 1. With `--target`, that flag is not optional — the checkout is not this PR, so an omitted number approves whatever PR the checkout branch has open. Add `--commit <head-sha>` too, so the approval names the commit it was based on.

**The body must match what actually happened on this PR — never thank the author for addressing feedback that was never given.** Pick by situation:

| Situation                                                       | Body                                                                                                      |
| --------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| No feedback was ever raised (no threads, no prior review notes) | `LGTM.` — plain, nothing more                                                                             |
| Feedback was raised and addressed                               | `LGTM. Thanks for working on the suggestions 🔥` — vary the phrasing, keep it one line                    |
| Comments are still open but none of them block                  | `LGWC; <who> left some comments worth addressing — I don't see anything blocking.` — **name the authors** |
| Checks are red but nothing in the code blocks                   | `LGWC; <check name> is failing — worth a look, but nothing blocking in the code.` — **name the check**    |

Name the people (and bots) whose comments are still open — the `review-threads`
output gives you each commenter's login. Naming them is the whole point of the
line: the author learns whose feedback to go read, and nobody has to guess what
"with comments" refers to. Examples:

```
LGWC; Copilot left some comments worth addressing — I don't see anything blocking.
LGWC; Copilot and @maria left a few suggestions — worth a look, but nothing blocking.
```

Keep it to one line, and never dress a real blocker up as a comment.

Don't paste the gate summary or per-thread detail into the PR — that belongs in the report to the user, not the review. If checks are red, name the failing job in one short clause (e.g. `LGWC; the lint job is failing — worth a look, but nothing blocking in the code.`) rather than withholding approval.

**If a real gate blocks** (a live concern that is a blocker, or a resolution that doesn't hold up), do **not** approve — whoever raised it. Report it to the user; the author can clear it with `/address-feedback`. Post one terse comment naming the concern itself — never "please resolve the threads" — and say you'll approve once it's addressed, so the author knows nothing else is outstanding:

```bash
devgeta task comment-pr --body "Everything looks good on my side except one thing: <what's left in one line>. I'll approve once that's addressed."
```

With `--target`, `--pr PR_NUMBER` is not optional on that call — the checkout is not this PR, so an omitted number posts the comment to whatever PR the checkout branch has open.

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
- Severity decides, not activity, and not authorship. A thread blocks when its concern is a blocker and still live in the code — no matter who raised it or how many times. A thread whose concern was fixed is not a blocker, and neither is an untouched suggestion or nit — approve with `LGWC` and name who left it. Resolving threads is the author's bookkeeping, not an approval gate.
- Approving over live comments rests on a reviewer agent's `APPROVE` verdict in context. Without one, don't approve over them — point at `/review-pr` instead.
- Failing CI is flagged, not a blocker — the user decides what to do about it.
- Both the approval and any comment stay terse; the detail goes to the user, not the PR.
