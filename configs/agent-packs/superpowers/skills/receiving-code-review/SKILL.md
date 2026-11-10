---
name: receiving-code-review
description: Use when receiving code review feedback, before implementing suggestions, especially if feedback seems unclear or technically questionable - requires technical rigor and verification, not performative agreement or blind implementation
---

# Receiving code review: pull request steps

Follow UPSTREAM.md (in this skill's folder) for judging each item: verify it against the code, push back with reasons, ask when it is unclear. This file adds only the steps for working through a pull request.

**Authorization.** When the person names a pull request (a number, a link, or plainly "the PR"), running this skill authorizes committing, pushing and posting replies without asking first. An open PR on the current branch is not enough. With no PR named, follow UPSTREAM.md on the feedback in the conversation and stop.

## Steps

1. **Find the PR** the person named: a number, a link, or "the PR" (the current branch's).
2. **Gather everything before changing anything**: review summaries, every unresolved inline thread, and top-level PR comments, from people and bots. One GraphQL query covers it:
   `gh api graphql -f query='query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){pullRequest(number:$n){reviews(first:100){nodes{author{login} state body}} comments(first:100){nodes{author{login} body}} reviewThreads(first:100){pageInfo{hasNextPage} nodes{id isResolved comments(first:20){nodes{author{login} body path line}}}}}}}' -f o=<owner> -f r=<repo> -F n=<n>`
   Each list stops at 100. If `hasNextPage` is true, page before going on.
3. **Judge each item** with UPSTREAM.md's process.
4. **Fix** in logical batches, run the repo's tests for what changed, commit, push.
5. **Reply to every item**, short and plain: `Fixed in <sha>: <what changed>`, `Not changing: <reason>`, or the question.
   - Inline threads get the reply in the thread: mutation `addPullRequestReviewThreadReply(input:{pullRequestReviewThreadId:"<id>", body:"..."})`.
   - Top-level comments and review summaries get one PR comment listing them: `gh pr comment <n> --body-file -`.
6. **Resolve** each thread that was fixed or answered with a reason: mutation `resolveReviewThread(input:{threadId:"<id>"})`. Leave a thread open only when your reply is a question to the reviewer.
7. **Close out.** Check `gh pr view <n> --json reviewDecision`. If the PR is not approved, ask the people who reviewed to look again, skipping bots (logins ending in `[bot]`): `gh api -X POST repos/{owner}/{repo}/pulls/<n>/requested_reviewers -f 'reviewers[]=<login>'`. If it is approved, do not ask again: what is left is nits. Say it is ready.
