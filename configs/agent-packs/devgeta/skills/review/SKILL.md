---
name: review
description: Use when asked to review a design doc, plan, code change or pull request before it is built on or merged — "review this", "review the plan", "review PR 12"
---

# Review

Review work with fresh eyes, in order of what is cheapest to fix: design, then plan, then code. Weight security, regressions and test quality highest.

**Authorization.** When the person names a pull request (a number, a link, or plainly "the PR"), running this skill authorizes posting the review to it without asking first. An open PR on the current branch is not enough. With no PR named, report in the session and post nothing. This skill never edits files.

## What it reviews

What the person named: files, a branch, a PR number or link. Nothing named: the current branch against its merge base with the default branch, including uncommitted and untracked files. On a PR: the PR's diff (`gh pr diff <n>`).

## Stages

Sort what is in scope into design docs, plans and code. The repo's own conventions decide what counts as which (its instructions file, templates, doc folders). Run the stages that are present, in order. A blocker in an earlier stage ends the review there with the verdict "fix the design first": do not review later stages against an unsettled design.

## Before judging

- Read the repo's instructions and contributing docs, and any template the doc should follow. Repo rules beat the defaults here.
- When code is in scope, read the design doc or plan it links to, if there is one.
- If this session wrote the work, say so at the top and recommend a fresh session or another model. Then continue.

## Checklists

Run a check only when its stage is present.

**Design doc**
- Problem stated before solution: who has it, what happens if we do nothing.
- Goals, non-goals, how success is measured.
- At least two real alternatives, with reasons.
- Assumptions marked verified or not, riskiest first.
- Failure modes: partial failure, retries, concurrency, bad input, load, tenant isolation where relevant.
- Hard-to-reverse choices (data model, public API, security boundary) get the deepest pass.
- How we will know it is broken, how to roll back, how to migrate.

**Plan**
- Every task traces to a requirement, and back.
- Riskiest work first.
- Each step has a testable definition of done.
- Dependencies and rollback per step.
- Anything outside the goals is cut.

**Code**
- Intent first: if the description and tests do not say what it does, that is the first finding.
- Matches the approved design; deviations are explained.
- Correctness: edge cases, error paths, concurrency, idempotency, transactions. Run the tests when you can.
- Security: authorization on every path, input validation, secrets, migration safety.
- Regressions: grep every caller of a changed function or behavior.
- Tests: would they fail if the logic were wrong? They should test behavior, not implementation.
- Simplicity: deletable code, abstractions with one user.
- Could someone debug this in six months?

## Findings

Each finding has: severity (blocker, should-fix, nit), `file:line`, what is wrong, the fix, and why it matters. One or two short sentences each, plain words, a term explained the first time. A junior engineer should be able to act on it after one read.

- No finding without a concrete case: an input, a state, a caller that breaks.
- Only blockers stop approval.
- A code finding that shows a design flaw also names the doc that needs the fix.

## Re-review

When the PR already has review threads, check each earlier blocker first (fixed or not, with evidence), then review only what changed since. Never repeat a point an open thread already makes.

## Output

**No PR named, or no `gh`:** the report in the session: one line on what the change does, findings grouped by severity, the verdict, and a "not checked" line.

**PR named:** post one GitHub review.

1. `gh pr view <n> --json author,headRefOid,baseRefName,files` and `gh api user --jq .login`.
2. Existing threads, for the re-review rule: `gh api graphql -f query='query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){pullRequest(number:$n){reviewThreads(first:100){nodes{isResolved comments(first:20){nodes{author{login} body path line}}}}}}}' -f o=<owner> -f r=<repo> -F n=<n>`. Page if there are more than 100.
3. Post once with `gh api repos/{owner}/{repo}/pulls/<n>/reviews --input -`, sending `{commit_id, event, body, comments:[{path, line, side:"RIGHT", body}]}`. `commit_id` is `headRefOid`.
   - One inline comment per finding whose line is in the diff. A finding whose line is not in the diff goes in `body`.
   - `body`: what the change does, the findings list, the verdict, and "not checked".
   - `event`: `REQUEST_CHANGES` when there are blockers, `APPROVE` when there are none.
   - GitHub does not let you approve or request changes on your own PR. When the PR author is the current `gh` user, use `COMMENT` and make the first line of `body` "Verdict: approve" or "Verdict: request changes".
