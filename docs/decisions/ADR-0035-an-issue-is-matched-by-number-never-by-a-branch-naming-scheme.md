# ADR-0035 — An issue is matched by number, never by a branch-naming scheme

**Date:** 2026-08-26
**Status:** PROPOSED

## Context

`issue-scope <n>` (Slice C) has to answer "what already exists for this
issue" — which open PRs reference it, which local branches reference it, and
which worktree holds one of those branches — without knowing anything about
how the repository it's running in names branches or writes PR bodies.
Devgeta ships to strangers' repositories (CLAUDE.md §3 principle 8); a
matcher that assumes "the branch whose name _is_ the issue number" or greps
PR text for `#12` encodes one team's convention as product behavior, and is
wrong at the boundaries besides: a substring search for `#12` matches inside
`#1234`.

Two different kinds of association are involved here, and they do not carry
the same evidentiary weight. A PR that GitHub itself has already linked to an
issue is a confirmed fact. A branch that happens to contain the digits `12`
in its name might be _about_ issue 12, or might be `v1.12`-something, or a
year, or coincidence — the number alone cannot tell those apart. Treating
both the same way — as "the issue's PRs and branches" with no distinction —
would report a coincidence with the same confidence as a real link, which is
worse than not reporting it at all: a caller that trusts an `issue-scope`
answer at face value would act on a branch that has nothing to do with the
issue.

## Decision

### 1. PR↔issue: GitHub's own cross-referenced timeline events, same-repo only

The accepted source for "which PRs reference this issue" is the issue's
**timeline cross-referenced events** — the same mechanism GitHub itself uses
to show "N linked pull requests" on an issue page. `gh issue view <n> --json`
does not expose these; they are reached through the GraphQL
`timelineItems(itemTypes: [CROSS_REFERENCED_EVENT])` connection, the same
`gh api graphql --paginate` route `FetchReviewThreads`
(`internal/tooling/terminal/dev_tools/githubcli/githubcli.go:257-268`) already
uses for the same reason: `--paginate` emits one JSON document per page, so
the connection must be paginated rather than capped at a fixed page size the
way `prDiscussionQuery`'s nested collections are (`githubcli.go:229-232`) —
an issue can accumulate more than 100 cross-references over its life, and
silently dropping the rest is worse than the round trip it costs to paginate.

Body, title, and comment text grep is **explicitly not an accepted source**.
That is precisely the boundary-wrong route this cycle exists to replace — the
domain-context section of the cycle doc names `#12` matching inside `#1234`
as the concrete failure this decision closes off.

Same-repository references only, this cycle: a cross-reference from another
repository is filtered out of the result — reported neither as a candidate
nor as confirmed. Cross-repo GitHub App integrations and forks make a
cross-repo reference's meaning genuinely repository-dependent in a way this
ADR does not attempt to resolve; it is dropped rather than guessed at.

### 2. Branch↔issue: an exact grammar, reported only as a candidate

A local branch is reported when its name contains the issue number's **exact
digit run**, with the characters immediately before and immediately after
that run — where they exist — **not** in the character class
`[0-9A-Za-z.]`, and an optional single `#` allowed immediately before the
run. This is the whole rule; every example below is derived from it, not
listed as a separate case:

| Branch fragment | Matches issue 12? | Why                                                     |
| --------------- | ----------------- | ------------------------------------------------------- |
| `#12`           | yes               | digit run `12`, preceded by `#`, nothing after          |
| `12`            | yes               | digit run `12`, no neighbors                            |
| `x#12`          | yes               | preceded by `#`, `x` is before the `#`, not the run     |
| `#1234`         | no                | the digit run is `1234`, not `12`                       |
| `#12x`          | no                | character after the run (`x`) is in `[0-9A-Za-z.]`      |
| `v1.12`         | no                | character before the run (`.`) is in the excluded class |
| `2012`          | no                | digit neighbors on both sides — run is `2012`           |
| `12.3`          | no                | character after the run (`.`) is in the excluded class  |

This is stated as an exact character-class rule rather than "word boundary"
because the two are not the same thing and a review round confirmed the
difference concretely: a regex `\b` sits at the transition between a `.` and
a digit (there is no word character on the `.` side), so `\b` **would**
match `12` inside `v1.12` — exactly the false positive this rule exists to
avoid. "Word boundary" is the wrong vocabulary for this rule; the character
class above is the right one, and it is what the implementation's table test
must derive every case from.

Every match found this way is reported as a **candidate**, and is never
promoted to a confirmed link regardless of how specific the branch name
looks. No naming scheme is assumed to hold — not "the branch for issue 42 is
named `42`," not "issue branches are always prefixed `issue-`." A bare number
in a branch name is a coincidence often enough that treating it as confirmed
would be wrong in exactly the repositories that don't share the convention
whoever wrote the branch name had in mind.

### 3. The reporting obligation: what matched, from which source, how strong

`issue-scope`'s output states, for every association it reports: what
matched (a PR number, a branch name), which source produced it (the
cross-reference timeline, or the branch-name grammar), and how strong that
association is (confirmed, or candidate). A caller — human or agent — reading
the output can tell a real link from a coincidental one without having to
know the two detection mechanisms exist. Nothing is reported as flatly "the
issue's branches" or "the issue's PRs" with the distinction only implicit in
which code path produced it.

## Consequences

- `issue-scope` cannot mis-associate `#12` with `#1234`, or a version string
  like `v1.12` with issue 12 — the grammar is exact where a naive substring or
  word-boundary check would not be.
- A genuinely linked PR is reported with the same confidence GitHub's own UI
  gives it, because it comes from the same underlying signal
  (`CROSS_REFERENCED_EVENT`), not a re-derived guess.
- A branch name is never silently promoted from "contains this number" to
  "is about this issue" — every candidate is labeled as a candidate in the
  output, and a caller that wants to filter to confirmed-only associations
  can do so from the label alone.
- Cross-repository cross-references are invisible to `issue-scope` this
  cycle. A future cycle that wants them has to decide separately what
  "confirmed" means across a repository boundary — this ADR deliberately
  does not pre-answer that.
- The branch matcher is a small, pure, separately-tested function
  (`internal/tooling/task/issue.go`) with an adversarial table derived
  directly from the grammar in §2 — the same shape of test the scratch key
  sanitizer (ADR-0033) and the trailer detector (Slice H) use for their own
  security/correctness boundaries.
