# ADR-0012 — Review knowledge lives in a local journal devgeta owns

**Date:** 2026-08-05
**Status:** ACCEPTED

## Context

Re-reviewing the same work goes in circles. A reviewer asks six questions, they get
answered, and the next review asks the same six. Nothing ever concludes.

The cause is that a review agent has no memory and nothing hands it one. Each run is a
fresh agent with an empty context, and the three reviewers are explicitly told not to look
for prior feedback:

> Posting to a PR, fetching existing review threads, and deduplication are handled
> downstream by `/review-pr` — do not fetch PR comments or check for prior feedback.
> — `configs/shared/agents/code-reviewer.md:18` (same line in `document-reviewer.md`,
> shorter variant in `skill-reviewer.md`)

That rule assumes `/review-pr` runs afterward and dedups. It does dedup, thoroughly —
against resolved threads, author replies, review summaries, and conversation comments
(`configs/shared/commands/review-pr.md:46-58`). But it only runs when a PR exists and
someone invokes it. Two common flows have neither:

1. **Reviewing your own changes before opening a PR.** Nothing has been pushed. There is
   no PR, no threads, no downstream. This flow has no memory of any kind.
2. **Reviewing someone else's live PR from a fresh session.** The prior round _is_ on
   GitHub, but the reviewer is forbidden from reading it and the human's answers were given
   in a chat session that no longer exists.

An earlier attempt fixed only the second flow, by letting the reviewers read
`devgeta task review-threads` themselves. That was rejected for a reason worth recording:
**it makes review memory depend on the cloud.** A reviewer that only remembers when GitHub
is involved still starts from zero on the flow where most local reviewing happens, and it
puts a network round-trip in the path of a review of uncommitted code.

So the question this ADR answers: **where does a reviewer's accumulated knowledge live,
such that it survives a new session and does not require a PR to exist?**

### What is actually worth remembering

Not "the last review's output". Three things, which differ in how they expire:

| Knowledge                | Example                                                                               | When it stops being true                                        |
| ------------------------ | ------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| **An answered question** | "Does the retry reuse the outer context?" → "Yes, `ctx` is passed at `client.go:88`." | When that code changes                                          |
| **A rejected finding**   | "N+1 query" → "Intentional; batch size is capped by config."                          | When the _reason_ stops holding — not when unrelated code moves |
| **A fixed finding**      | "Missing error wrap at `client.go:99`" → fixed                                        | Immediately suspect: a fix can be reverted or regress           |

The middle one is a human judgment and is the most valuable to keep. The last one is the
most dangerous to trust: a store that says "already fixed" and is believed without checking
is worse than circling, because now nobody is looking.

## Decision

**Review knowledge is a per-branch journal, written and read through `devgeta task`
commands, and every entry that cites code records the exact content it was judged
against.**

Five parts.

### 1. A journal file per review target, in the superpowers shape

Plain markdown, one file per branch, human-readable and hand-editable — the same shape
`writing-plans` uses for plans (`docs/superpowers/plans/YYYY-MM-DD-<name>.md`): a known
path, one file per unit of work, read at the start of a session, updated in place.

Keyed by **branch name**, not PR number, because a branch exists in both flows and a PR
belongs to a branch. Reviewing someone else's PR resolves its head branch first, so the
pre-PR and post-PR reviews of the same work share one file instead of splitting into two.

```markdown
---
branch: fix/retry-context
base: origin/main
last_review: 2026-08-05 (head a1b2c3d)
---

## Settled

- **rejected** [n1] `client.go:42` — N+1 query in the retry loop
  answer: intentional, batch size is capped by `config.MaxBatch`.
  (blob 626799f0f85326a8c1fc522db584e86cdfccd51f, head a1b2c3d)
- **answered** [n2] — Does the retry reuse the outer context?
  answer: yes, `ctx` is threaded through at `client.go:88`.
  (head a1b2c3d)
- **fixed** [n3] `client.go:99` — missing `%w` on the wrap.
  (blob 9f31c0244b0f5f6a1e3d1c9b7a8e5d2c4f60718a, head d4e5f6a)

## Open

- [n4] `store.go:12` — [IMPORTANT] write is not atomic; a crash mid-write truncates the
  file. (blob 77aa0916d3b8c5e2f419a7d0b6c83e5147f2a90b, head d4e5f6a)
```

Every entry has a stable id (`n1`, `n2`, …, assigned by the writer, never reused within a
journal) so an answer can name the exact question it settles.

### 2. Staleness is per-entry and per-path, computed from content, not commits

A commit SHA cannot carry staleness here, and the reason is the ADR's own primary flow:
pre-PR reviews routinely judge **uncommitted** code. Two different dirty versions of a file
share the same HEAD, so `git diff <sha>..HEAD -- <path>` would report "unchanged" and mark
an entry fresh when the very line it judged has been rewritten.

So each entry that cites a path records the **blob hash of that file's content as reviewed**
— `git hash-object <path>` on the working-tree file, which is the same identity whether the
content is dirty, staged, or committed. (Verified in this repo: `git hash-object` hashes
current working-tree bytes, not the last commit.) Staleness is then one comparison:

```
git hash-object <path>   ==  entry's recorded blob  ->  fresh
                         !=                          ->  stale: re-check, don't trust
```

The head SHA is still recorded, but as human context ("when was this settled"), never as
the staleness signal. This is the local equivalent of GitHub's `(outdated)` marker, which
`/review-pr:57` already relies on for exactly this judgment. A **rejected** entry is the
exception — a human's reasoning does not expire because a neighbouring line moved, so its
reason is what gets re-read, not the verdict. An entry citing no path (a design-level
question) has no blob and never goes stale mechanically; it is always shown with its date.

Entries never silently vanish. A stale entry is shown as stale, so the reviewer knows both
that the question was answered _and_ that the answer may no longer hold.

**The stamp is taken when an entry is settled, not when it was opened.** The blob records
the content the _conclusion_ is true of, and the conclusion is formed at settle time. This
matters most in the commonest case there is: a `fixed` entry is settled precisely because
the cited file changed, so an open-time stamp would mark it stale the instant it was
settled, and every later review would re-check a fix that is perfectly fine — the exact
noise this design exists to remove. It also keeps the two write paths consistent: the
direct settle form has always stamped at settle time, so an open-then-settle exchange and a
directly-settled one now read the same. When the cited file no longer exists at settle time
(a finding fixed by deleting it), the original stamp is kept and the entry reads stale;
failing the settle would leave the exchange open with no way to close it.

### 3. Writes go through a task command, never free-form agent file edits

Two new subcommands, following the existing `review-scope` / `review-threads` /
`submit-review` family:

- `devgeta task review-notes` — print the journal for the current branch (or `--branch`),
  with each entry's id and its fresh/stale verdict already computed. One call, compact
  output, the same contract `review-threads` has. `--path` prints the journal file's
  location for hand correction.
- `devgeta task review-note --open [--at <path>] --note "<text>"` — record an open question
  or finding, returning its new id (`Noted n4`, per the task-design mutation contract:
  verb + target, so the caller can reuse the id without re-fetching).
- `devgeta task review-note --settle --id <id> --as rejected|answered|fixed --note "<text>"` —
  move an open entry to Settled with its resolution. The cited path carries over from the
  opened entry; the settle form takes no `--at` of its own, so a settle can never silently
  retarget the question it closes. (`--settle` is a boolean with a separate `--id` rather
  than `--settle [id]`: an optional flag value makes pflag parse the natural
  `--settle n4` as a bare flag plus a stray positional.)
  The **blob stamp is refreshed** at settle time — see §2.
- `devgeta task review-note --settle --as rejected|answered|fixed [--at <path>]
--note "<text>"` — the direct form, for an exchange that was never open (question asked
  and answered in one conversation): appends straight to Settled. It takes the same
  optional `--at` the open form does — without it, a directly-settled entry that cites
  code would have no blob to stamp and could never go stale.

`--at` is optional in both entry-creating forms for the same reason: a design-level
question ("should this be an ADR?") cites no file, and §2 already defines such entries as
having no blob and no mechanical staleness. When `--at` names a path that does not exist
in the working tree, the command fails with the path echoed back rather than writing an
entry with a stamp it could not compute — a typo'd path silently recorded as "no blob"
would create an entry that never goes stale while claiming to cite code.

Wherever a path is given, the writer stamps the blob hash and head SHA itself — the agent
supplies only the path and the text, so a stamp can never be wrong or forgotten. Writes are atomic
(write-temp-then-rename, the same rule CLAUDE.md §7 mandates for the global config); a
crash never truncates a journal. No locking beyond that: this is a single-user tool, and
atomic rename makes the worst concurrent case a lost update, never a corrupt file.

Why a command instead of letting the agent write markdown:

- The reviewers are read-only today (`edit: deny`). Granting write access to satisfy a
  journal would fail the least-privilege bar the skill reviewer enforces
  (`skill-reviewer.md:44`), and would let a review agent write anywhere.
- The staleness logic belongs in Go with tests, not in prose repeated across three prompts.
- The format stays stable, so a journal written by one reviewer is readable by the others
  and by `/review-pr`.

This is also the CLAUDE.md §6 rule about routing external tools through a wrapper, applied
to state: the journal has one writer.

### 4. Answers must be written back, or none of this works

**This is the load-bearing part.** The circling is not "the reviewer forgot its own
findings" — it is "the human answered and the answer went nowhere". A journal that only
records what the reviewer says is a one-way log, and the loop survives it.

So: when a question is answered in conversation, the answer is recorded before the session
ends. The reviewer records its own open questions as it asks them (`review-note --open`,
which returns the entry id); whoever answers — the main agent in the session, or
`/review-pr` from a PR thread — writes the answer back with
`review-note --settle --id <id> --as answered`. The id printed at open time is what makes the
settle unambiguous: the answer names the exact question it closes.

If that write-back is skipped, this ADR buys nothing. It is the step to verify first when
circling continues after implementation.

### 5. The journal lives in the repo's common git directory, keyed by branch

```
$(git rev-parse --git-common-dir)/devgeta/review/<encoded-branch>.md
```

`--git-common-dir` resolves to the **main** `.git` directory from every checkout — the main
one and every linked worktree alike (verified in this repo from both: a `review-note
--open` run inside a linked worktree wrote its journal into the main checkout). That is what makes
the journal genuinely per-branch: the same branch reviewed in the main checkout today and
in a worktree tomorrow reads and writes one file. The per-worktree git directory
(`--git-dir`) was considered first and rejected — it splits the same branch's knowledge
into two journals the moment the checkout moves, which re-creates the amnesia this ADR
exists to fix.

`<encoded-branch>` is a **reversible, collision-free encoding** of the branch name, not a
lossy slug: `/` and every byte outside `[A-Za-z0-9._-]` is percent-encoded
(`fix/retry` → `fix%2Fretry`). Two branches can never share a file
(`fix/a-b` vs `fix/a/b` would collide under slash-to-hyphen), the original name is
recoverable for listings, and the encoded form cannot contain a path separator — so a
hostile branch name (`../../x`) cannot escape the review directory. The writer additionally
verifies the resolved path is inside it before writing.

This location is chosen for what it makes impossible, not for convenience:

- **Nothing here can be committed.** It is outside the work tree, so it never appears in
  `git status`, never lands in a commit, and never shows up inside the diff a reviewer is
  reading. That also removes the `.gitignore` question entirely — ADR-0010 forbids devgeta
  from editing another repo's `.gitignore` (`warnIfWorktreesNotIgnored`,
  `worktree.go:1612-1630` warns and proceeds, never edits), and here there is nothing to
  ignore and no `.git/info/exclude` to write.
- **Cleanup keys off branch deletion, in Go, not off an agent.** `dg wt remove` already
  deletes the worktree's **branch** (`cmd/worktree.go:192`, force `-D`); the same teardown
  deletes `review/<encoded-branch>.md` in the same operation. Deleting the journal for a
  branch that just stopped existing is mechanical code with tests, not an instruction a
  model must remember.

**Cleanup is deliberately not tied to approval.** Two reasons. Approval is a judgment an
agent has to make and act on, which is exactly the step that cannot be relied upon; and
approval is not terminal — one more commit reopens everything, so deleting on approval
discards the memory at the moment the next review would most want it ("settled at `abc123`,
approved"). Cleanup is tied to teardown, which is a mechanical event:

| Trigger                          | Performed by                                                                                 | Depends on an agent? |
| -------------------------------- | -------------------------------------------------------------------------------------------- | -------------------- |
| Branch deleted by `dg wt remove` | the same Go teardown, same operation                                                         | No                   |
| Branch gone (deleted by hand)    | `review-notes --prune`: drop journals whose branch no longer exists locally or on the remote | No — checks a fact   |
| "The review was approved"        | the reviewing agent, mid-conversation                                                        | Yes — rejected       |

Lifecycle edges, decided here rather than left to implementation:

- **Detached HEAD**: there is no branch to key by, so there is no journal. `review-notes`
  prints a fixed sentinel (`No branch — review notes are keyed by branch.`) and the review
  proceeds without memory, exactly as today. Inventing a key from the SHA would create
  journals nothing ever cleans.
- **Branch rename**: the journal does not follow; the new name starts empty and the old
  name's file remains until pruned. Renames mid-review are rare enough that following them
  is not worth the machinery; `review-notes --path` makes a hand move possible.

Accepted costs of this location:

- **Not browsable.** `devgeta task review-notes` prints the content and `--path` prints the
  file location, so a wrong entry is still correctable by hand — through a command rather
  than by opening a visible folder. This is the one thing the in-repo `.devgeta/reviews/`
  option would have done better, and it is traded for never being committable.
- **Removing a worktree removes its branch's journal** (because `dg wt remove` deletes the
  branch). Judged correct — the work was torn down — but reviewing the same branch name
  again later starts from zero.
- **A PR reviewed without a checkout** has no local branch to die with, so its file
  persists in the main clone's git directory until `--prune` runs. A few KB of invisible
  text per PR reviewed.

### What implementation had to prove — all met

- `--git-common-dir` resolves to the main `.git` from a **linked worktree** — proven with
  the real binary: `review-note --open` run inside a linked worktree wrote its journal into
  the main checkout's git directory.
- Blob-hash staleness catches a dirty edit that a commit-SHA comparison misses — the exact
  failure mode of part 2's rejected design. Proven against real git and by a unit test
  verified to fail when `Verdict` compares HEAD instead.
- Encoding round-trips hostile branch names (`fix/a-b` vs `fix/a/b`, `../../x`, unicode)
  with no collision and no path escape.
- Teardown deletes the branch's journal, and a crash mid-write leaves the previous journal
  intact (atomic rename). Both `dg wt remove` and `worktree-finish` are covered — the
  latter deletes branches through a different path and needed its own hook.
- `--at` with a nonexistent path fails without writing, and `review-notes` renders an
  entry whose cited file was later **deleted** as stale (the blob comparison has nothing
  to hash against — that is staleness, not an error).

Cleanup keys off the **branch**, never the worktree's row/directory name: `FlattenName`
strips `/`, so branch `feat/login` lives in a directory called `feat-login`. Keying cleanup
off the row name both left the real journal (`feat%2Flogin.md`) behind and could delete
`feat-login.md`, which may belong to a genuinely different branch. `Git.BranchForWorktree`
is the one resolver both teardown paths use; when it cannot resolve, the journal is left for
`review-notes --prune` rather than guessed at.

## Consequences

**Easier**

- A review can converge. An answered question is answered once, in both flows, whether or
  not a PR exists and whether or not the session that answered it still exists.
- Local reviewing gets memory for the first time — the flow that had none.
- The knowledge is inspectable and correctable by hand. A wrong entry is a text edit, not a
  mystery.
- No network in the path. GitHub threads stay useful and complementary (`/review-pr` keeps
  using them, and the journal can be seeded from them), but nothing depends on them.

**Harder / accepted trade-offs**

- **Two stores now describe the same thing** when a PR exists: the journal and the PR's
  threads. They can disagree. Accepted because they answer different questions — the
  threads are the shared record with the author, the journal is the reviewer's working
  memory — and because the journal is the only one that exists in both flows.
- **A fresh "settled" entry can still hide a real regression.** Blob identity only covers
  the cited file: a finding settled against a file that has not changed since can still be
  broken by a change elsewhere (a caller, a config, a schema). The journal is a memory aid,
  never a substitute for the verification bar each reviewer already has.
- **New persistent state to maintain**, with a format that will need a migration path if it
  changes. This is the cost the earlier prompt-only attempt avoided; it is being paid
  deliberately, because prompts cannot remember anything.
- **The journal is per-clone.** Two clones of the same repo do not share memory, and
  `dg wt remove` (which deletes the branch) takes the journal with it. The data-dir
  alternative would survive both, at the cost of the cleanup problem described under
  "Alternatives".

**Revisit this decision when**

- Circling continues even with journals present — that means the write-back step (part 4)
  is being skipped, and no storage choice fixes a loop nothing writes to. Instrument that
  first, before redesigning.
- Journal entries and PR threads disagree often enough that reviewers stop trusting either.
  That is the signal to seed the journal from `review-threads` automatically instead of
  keeping the two stores independent.
- A real need appears for memory shared across clones or machines — that reopens the
  data-dir alternative, which this ADR rejects only on cleanup grounds.

**Not in scope, but adjacent and worth fixing in the same work**

`document-reviewer.md` has no verdict section at all — its output ends at a risk rating and
a "Questions for the Author" section, while `code-reviewer.md:120-122` and
`skill-reviewer.md:79-81` both end on
`**Status:** APPROVE | REQUEST CHANGES | NEEDS DISCUSSION`. No amount of stored knowledge
lets it approve something when approving is not in its template. That is a one-line fix
plus a test that all three reviewers carry a reachable verdict, and it is independent of
this decision.

## Alternatives considered

**Let the reviewers read `devgeta task review-threads` themselves.** Implemented, then
rolled back. Fixes only the PR flow, leaves pre-PR reviewing with no memory at all, and
makes review memory depend on the cloud.

**Commit the journal to the repo (`docs/reviews/…`), the way superpowers commits plans.**
Rejected: a review journal is the reviewer's working notes, not a shared artifact of the
work. Committed to the branch under review it would appear in that PR's own diff, get
reviewed by the reviewer reading it, and conflict whenever two people review the same
branch. Plans are shared and belong in the repo; review memory is not.

**A visible `.devgeta/reviews/` in the repo root.** The first shape considered, and the one
that best matches the superpowers plans folder: browsable and hand-editable. Rejected
because keeping it out of commits requires either editing `.gitignore` (forbidden by
ADR-0010), writing `.git/info/exclude` behind the user's back, or a first-run warning the
user has to act on — and because an untracked folder in the work tree shows up in
`git status` and inside the diff the reviewer itself reads. Storing under the git directory
gets the same per-branch cleanup with none of that.

**The per-worktree git directory (`--git-dir` instead of `--git-common-dir`).** Attractive
because `git worktree remove` deletes that directory itself, making cleanup free. Rejected
because it is per-checkout, not per-branch: the same branch reviewed in the main checkout
and later in a linked worktree gets two different git directories and therefore two
journals — re-creating, between checkouts, exactly the split memory this ADR exists to
remove. Cleanup instead rides on `dg wt remove` deleting the branch, which the same Go
teardown already performs.

**devgeta's data dir (`paths.Paths.Data.Root`) with cleanup on approval.** Considered
because it survives worktree deletion and is shared across clones of a repo. Rejected on
cleanup: deleting on approval requires an agent to decide the review is done and then act
on it, which is the least reliable step in the whole flow, and approval is not final anyway
— a later commit reopens what was settled. Teardown is a mechanical event; approval is a
judgment. The journal follows the event.

**Keep no state; make the prompts ask fewer questions.** Reduces the symptom without
addressing the cause. A question asked once and answered still comes back on the next
fresh session, because nothing recorded the answer.
