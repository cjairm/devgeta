# ADR-0022: A PR review targets immutable SHAs and a PR-scoped journal, through the same runner

**Status:** ACCEPTED
**Date:** 2026-08-07
**Deciders:** cjairm
**Related:** [ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md), [ADR-0018](ADR-0018-review-loop-refuses-the-default-branch.md), [ADR-0019](ADR-0019-a-review-covers-the-branch-working-state.md), [ADR-0021](ADR-0021-pr-review-trigger-is-a-polled-state-read.md), [cycle 2026-08-06-pr-review-loop](../plans/cycles/2026-08-06-pr-review-loop.md)

---

## Context

[ADR-0021](ADR-0021-pr-review-trigger-is-a-polled-state-read.md) decides **when** a PR gets
reviewed. This one decides **what gets reviewed, and where the findings go** — the harder
half, because every piece of devgeta's review machinery was built for a branch that is
checked out, and a PR under review usually is not.

Four things are true of the code today, and each one breaks on a foreign PR:

1. **`review-package` does not fetch and does not use a merge base.**
   `internal/tooling/task/reviewpackage.go:42-66` verifies both refs with
   `rev-parse --verify` and then builds `base + ".." + head`. Two separate problems follow.
   Nothing guarantees either ref exists locally or is current — a review could silently run
   against a stale copy fetched days ago. And for `git diff`, `base..head` is an **endpoint
   comparison**: if the base branch advanced after the PR opened, every commit that landed
   on the base in the meantime shows up in the diff **reversed**, as if this PR were undoing
   them. The reviewer would then report findings about code the PR never touched.
2. **The journal is keyed by the current branch.**
   [ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md) §5 keys review memory to the
   checkout's branch name. For a PR that is not checked out, that name is at best unrelated
   and at worst harmful: the review would read another branch's settled decisions as if they
   applied to this PR, and write this PR's findings under that branch's name — where the
   next review of that branch reads them, and where branch teardown deletes them.
3. **The journal stamps against the working tree.**
   `internal/tooling/reviewjournal/manager.go:169-190` — `stamp` does `os.Stat` on the cited
   path in the **working tree** (a missing file fails the write, ADR-0012 §3's typo guard)
   and hashes it via `HashObjectIn`; the head stamp is the checkout's `HEAD`. Reviewing a
   foreign PR from an unrelated branch, the cited file is absent (so the write fails
   outright) or present with unrelated content (so the staleness signal lies). This is why a
   PR-scoped journal **key** alone is not enough.
4. **`review-run` is HEAD-shaped by two prior decisions.**
   [ADR-0018](ADR-0018-review-loop-refuses-the-default-branch.md) makes it refuse the
   default branch and a detached HEAD;
   [ADR-0019](ADR-0019-a-review-covers-the-branch-working-state.md) makes it review the
   branch's working state — commits plus uncommitted work — and adds a third refusal, "no
   commits ahead **and** a clean tree". All three refusals, and the working-state scope,
   assume the thing being reviewed is the checkout.

So the question: what does a PR review point at, where does its memory live, and does the
existing runner stretch to cover it?

## Decision

**A PR review targets immutable SHAs fetched read-only on every triggered tick, journals
under a PR-scoped key at the reviewed revision, and runs through the same `review-run` — in
an explicit-range mode, never a second runner.**

Five parts.

### 1. Read-only fetch of `refs/pull/<n>/head`, bounded

Every triggered tick fetches the PR head ref and the base ref before resolving anything,
reusing the bounded-timeout helper `ReviewScope` already calls (`tm.Git.FetchOriginTimeout`).
What is deliberately NOT reused is `ReviewScope`'s fall-through-on-failure behavior
(`scope.go:89`, where a failed fetch sets a `fetchFailed` bool and the scope read continues
anyway): here, a fetch failure **ends the tick with a report** and never falls back to
whatever is on disk, because reviewing a stale ref produces a confident review of code the
PR no longer contains — worse than not reviewing at all.

`refs/pull/<n>/head` is used rather than the head repository's branch ref because the
upstream repo serves it for **fork PRs too**. That is one code path covering forks and
same-repo branches alike, with no fork-remote handling and no second URL to authenticate
against.

The fetch writes a non-branch ref. It moves no local branch, changes no upstream tracking,
and touches no working tree — the user's checkout is exactly as they left it, which matters
for a command that runs unattended on an interval.

### 2. The range is `merge-base(base, head)..head`, as resolved SHAs

Two properties, decided together.

**Merge base, not base tip.** The base of the range is
`git merge-base <base-branch> <head>`, so a two-dot diff yields exactly the PR diff GitHub
shows. This is the fix for the endpoint-range problem in the Context — and it is applied by
the **caller supplying the merge base as `base`**. `review-package` itself is left unchanged:
its contract is "diff exactly the two refs I gave you", which is the right contract for an
explicit historical range and is what makes it reusable here.

**Immutable SHAs, not ref names.** What the tick prints and every later step consumes are
resolved commit SHAs, not `main` and `feature-x`. A review takes minutes across several
models; a ref name resolved twice during that window can mean two different commits. SHAs
mean the reviewers, the journal stamps, the finding verification, and the posted review all
describe one fixed snapshot, and that snapshot is what GitHub showed when the tick started.

The resolved target — `base:`, `head:`, the journal key, and the noise-filtered changed-file
list — is printed once by `dg task pr-review-target` and is **the** context for the rest of
the tick. Nothing downstream reads the working tree.

### 3. The journal key is `pr/<owner>/<repo>/<n>`

A scoped exception to [ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md) §5, not a
replacement for it. ADR-0012's branch key stays correct for its own case — reviewing work
you have checked out, where the branch is what the work _is_, and where branch teardown is
what cleans the journal up. A PR you are reviewing from someone else's fork has no such
branch locally, so the key falls back to the identity the PR does have.

The key is passed through the `--branch` override that `review-notes` and `review-note`
already expose (`reviewnotes.go:38`, `journalBranch`), so this needs no new journal
addressing. It is a safe filename for free: `reviewjournal/encoding.go` percent-encodes
every byte outside `[A-Za-z0-9._-]`, so the `/` separators encode, two keys can never
collide, and the encoded name cannot contain a path separator and therefore cannot escape
the review directory.

What this buys is the same thing ADR-0012 bought branches: a review of PR #213 today and a
re-review after the author pushes read and write the same memory, so a finding settled once
stays settled, and a rejected finding's reasoning survives the session that produced it.

### 4. Stamping and freshness resolve against the reviewed revision

The key alone is not sufficient — this is the direct consequence of Context point 3, and it
is why this decision has a fourth part instead of stopping at a filename.

`reviewjournal.Manager` gains a revision mode, exposed as `--rev <sha>`:

- **Stamp at a revision.** The cited path's blob comes from
  `git ls-tree --full-tree <rev> -- <path>` — git resolves the tree entry directly, so there
  is no `os.Stat` and no hashing of a checkout file. A path that does not exist **at that
  revision** fails the write, which preserves ADR-0012 §3's typo guard rather than weakening
  it.

  `ls-tree` rather than `rev-parse --verify <rev>:<path>`, which the first implementation
  used, for two reasons found in review. `rev-parse` reports a missing path and an
  unresolvable revision identically (`fatal: Needed a single revision`, exit 128), so a
  `--rev` that was never fetched — this feature's likeliest real failure, since the flow
  fetches `refs/pull/<n>/head` — would be reported as "your cited path does not exist",
  sending an agent off rewriting correct paths. And `rev-parse` resolves a **directory** to a
  tree object and exits 0, so a cite naming one would be stamped with a tree hash, the exact
  write the typo guard exists to refuse. `ls-tree` separates the cases on the exit code
  (nonzero = the lookup failed; exit 0 with empty output = definitively absent) and names the
  object's type, so a non-blob is rejected. The cited path is passed as a `:(literal)`
  pathspec, and **not** because of globbing: `git ls-tree` does no glob matching at all.
  Measured on git 2.51.1, `ls-tree -- 'f*.go'` matches nothing even though `f1.go` exists
  (`git ls-files` with that same pathspec matches it), `:(glob)` is rejected outright as
  `pathspec magic not supported by this command`, and a real file named `f[1].go` resolves
  to the same blob with or without the prefix. What the prefix actually prevents is a
  **leading colon**: `ls-tree -- ':weird.go'` reads that colon as pathspec magic and returns
  exit 0 with empty output — which this code, correctly for every other input, reads as
  "definitively absent" and turns into "cited path `:weird.go` does not exist at `<rev>`".
  That is exactly the misleading-absence failure the rest of this section exists to stop, so
  a correct cite would be blamed as a typo. `:(literal):weird.go` resolves it.
  `--rev` is also resolved and verified **once per command** with
  `ResolveCommit`, before any entry is touched, so a revision this repository does not have
  fails clearly instead of degrading into "every entry is stale" on the read path, where
  `Verdict` returns a bare string and cannot report an error at all.

- **The head stamp is the commit `<rev>` resolves to**, the reviewed commit, not the
  checkout's `HEAD`. It is the resolved sha and never the caller's spelling: this decision
  exists because a review must target something immutable, and a ref name — `refs/pull/213/head`,
  a branch, `HEAD` — recorded verbatim starts describing a different commit at the next fetch,
  making two ticks' entries incomparable. A caller that already passes a full sha resolves to
  itself.
- **Freshness compares against the current PR head.** The next tick passes the new head SHA,
  so "stale" means "the PR changed this file since the finding was raised" — never "your
  checkout differs from the PR", which is true of almost every file and would mark the whole
  journal stale.

Without `--rev`, behavior is byte-identical to today's working-tree stamping. Branch reviews
are unaffected.

### 5. `review-run` gains an explicit-range mode — there is no second runner

`dg task review-run` already owns reviewer-list resolution from `review.reviewers`,
sequential headless model runs, `--reviewer` agent selection, the five-outcome verdict parse
([ADR-0017](ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md)), `--note`, and
the sampled progress reporting. A PR-range runner beside it would re-implement every one of
those, and CLAUDE.md §6 names that as the defect to fix, not a style preference: when a
second use appears, the logic is shared in the same change.

So `review-run` gains `--base <sha> --head <sha> --journal <key> --report-dir <dir>`
(required together, mutually exclusive with running on a branch). Everything else about the
command is unchanged. This also **generalizes** it — any historical range becomes reviewable,
not only a PR's.

Range mode **skips all three HEAD-dependent refusals** — ADR-0018's default-branch and
detached-HEAD refusals, and ADR-0019's "no commits ahead and clean tree" — because each one
guards something this mode supplies explicitly. ADR-0018 refuses the default branch to
prevent an empty diff being approved and to prevent an uncleanable `main` journal; here the
diff is an explicit non-empty range and the journal is explicitly keyed. ADR-0019's check
asks "does this branch change anything at all"; here the range answers that directly. A
refusal that exists to protect an inference is not needed where the value is stated.

And unlike branch mode, **range mode reviews the immutable SHAs only. The working tree is
never part of the diff.** ADR-0019 deliberately widened a branch review to include
uncommitted work, because on your own branch that is what would merge. On a PR you are
reviewing, uncommitted files in your checkout belong to something else entirely, and
including them would put your own unrelated work in front of a reviewer judging someone
else's PR.

## Consequences

### Positive

- **The reviewed diff is the PR's diff.** The merge base removes the reversed-changes class
  of false finding entirely, rather than asking reviewers to notice and ignore it.
- **The review target cannot drift mid-review.** Everything from the reviewer prompt to the
  posted review's commit anchor names one pair of SHAs, so a push during the review is
  detectable (the head moved) rather than silently half-applied.
- **Forks need no special handling.** One ref pattern, served by the upstream repo, works for
  every PR shape.
- **The user's checkout is untouched.** No branch is created, moved, or checked out; nothing
  is stashed. A loop running on an interval never surprises the human at the keyboard.
- **PR review memory accumulates per PR and cannot contaminate a branch.** A finding settled
  on PR #213 is read on the next tick of PR #213 and nowhere else.
- **One runner, one verdict contract.** ADR-0017's escalation rules, the five outcomes, and
  the reviewer configuration apply to PR reviews for free, and a future change to any of them
  lands in both flows at once.

### Negative

- **Every triggered tick pays a fetch.** Bounded and read-only, but it is network in the path
  of a review, and a network failure ends the tick. Accepted: the alternative is reviewing a
  ref that may be stale, which fails silently instead of loudly.
- **`review-run` grows a second mode**, and the two modes differ in what they refuse and in
  whether the working tree is in scope. That is real added surface in a command that already
  has several branches of behavior, and it is why each skipped refusal and the
  SHAs-only scope are pinned by their own tests.
- **The journal now has two keying schemes.** A reader of `reviewjournal` has to know that a
  key may be a branch name or a `pr/...` key, and that a `pr/...` journal has no cleanup
  path: branch teardown does not see it, and `review-notes --prune` deliberately skips it —
  "does a branch by this name exist?" answers "gone" for every PR key, so pruning them by
  that test would delete an open PR's settled findings mid-review. It persists in the clone's
  git directory until someone deletes it by hand, and deciding it automatically would mean
  asking GitHub whether the PR is still open, which that local command does not do. ADR-0012
  already accepted the leftover-journal cost for PRs reviewed without a checkout; the
  explicit key makes it more common.
- **Uncommitted work on the PR author's side is invisible**, by definition. A PR review sees
  pushed commits only. That is correct, but it is a real asymmetry with how ADR-0019 treats
  your own branch, and reviewers must not be told the two are the same.

### Neutral

- `review-package` is unchanged. It stays the "diff exactly these two refs" primitive, and
  correctness of the range is the caller's responsibility — which is what lets the same
  command serve both a PR range and any historical range someone types by hand.
- The commit anchor on the posted review (`commit_id`) is attribution, not enforcement.
  GitHub does not reject a review whose commit is behind the head; what the anchor buys is
  that the review names the SHA it actually judged, so a late-landing review is visibly
  stamped rather than silently claiming the current head.

## Alternatives Considered

### Diff the base branch tip against the head (`base..head`, as `review-package` does)

The obvious reading of "the PR's changes", and the shape the existing command already builds.

Rejected because it is wrong whenever the base branch has moved since the PR opened — which
is the normal case on any active repository. `git diff base..head` compares two endpoints, so
every commit merged into the base in the meantime appears in the diff as a reversal. The
reviewer then reads deletions of other people's work as this PR's doing. The failure is
silent and looks like a genuine finding, which is the worst available shape. The merge base
is what GitHub itself compares against, so using it also makes the reviewed diff match what
the author sees on the PR page.

### Fetch the head repository's branch ref instead of `refs/pull/<n>/head`

Read `headRefName` and the fork owner from the PR, add the fork as a remote, fetch that
branch.

Rejected: it needs fork-remote management (add, name, deduplicate, clean up), a second
authentication path for private forks, and a code path that only runs on fork PRs — the case
least likely to be exercised in testing. `refs/pull/<n>/head` is served by the upstream repo
for every PR, so one fetch covers both shapes and there is no fork-only branch to get wrong.
It is also the ref GitHub itself defines as "the PR's head", so it cannot disagree with the
PR page.

### Check out the PR locally and review it as a branch

Fetch into a branch, switch to it, and reuse the existing branch-mode review unchanged.

Rejected: it moves the user's `HEAD` as a side effect of a review, which
[ADR-0018](ADR-0018-review-loop-refuses-the-default-branch.md) already refused to do even for
the user's own work — and here the loop runs unattended on an interval, so the user would
find their checkout relocated with no prompt at all. It also requires a clean tree, cannot
run while the user is mid-edit, and would make the review's scope depend on the checkout
under ADR-0019, which is the opposite of the immutable target this decision needs.

### Key the journal to the checkout's branch (leave ADR-0012 as-is)

No new key; the PR review writes under whatever branch happens to be checked out.

Rejected on both directions of the leak. Reading, it applies another piece of work's settled
decisions to this PR — an entry saying "rejected: intentional, capped by config" is answering
a question nobody asked here. Writing, it files this PR's findings under a branch name, where
the next review of that branch reads them as its own and where deleting that branch destroys
the PR's memory. The two are unrelated units of work, and ADR-0012's own reasoning — one
journal per review target — is what says so.

### Build a second reviewer runner for PR ranges

A `pr-review-run` beside `review-run`, purpose-built for SHAs.

Rejected as the CLAUDE.md §6 DRY defect, stated plainly. It would duplicate reviewer-list
resolution, the sequential headless-run loop, `--reviewer` validation, `--note` handling,
progress reporting, and ADR-0017's five-outcome verdict parse — and the duplication would
matter most exactly where it is most dangerous, in the verdict parse, where the two copies
drifting means a PR review and a branch review disagree about what `APPROVE` requires.
Extending the existing command instead cost one mode flag group and made the command more
general than it was.
