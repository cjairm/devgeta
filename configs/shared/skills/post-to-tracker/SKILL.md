---
name: post-to-tracker
description: >-
  Post an investigation, findings, numbers, a plan, or a quick status note as a
  comment on a GitHub PR, a GitHub issue, or a Jira ticket — written in plain,
  short language that a PM, support, or anyone non-technical can read. Use this
  whenever the user wants to share what we just did with someone else: "post
  this to the PR", "comment these findings on the ticket", "let the team know on
  the Jira issue", "drop a status update on #123", "share these numbers on the
  issue", "write this up for the ticket". Trigger it even when the user only
  gestures at posting ("put this on the ticket", "send this to the PR") and
  hasn't named the format — this skill figures out the target and the wording.
  It uses `gh` for GitHub and the Atlassian MCP for Jira, and always shows the
  draft for approval before posting.
---

# Post to tracker

Turn what we just worked on — an investigation, a root-cause finding, some
numbers, a plan, or a one-line status — into a clean comment on the right
GitHub PR/issue or Jira ticket. The reader might be an engineer, but might just
as easily be a PM or someone from support, so the message has to be readable by
anyone, not just whoever wrote the code.

## Why this exists

Investigations and results live in a coding session, but the people who need
them live in the tracker. Copy-pasting raw terminal output or a wall of
technical detail there is noise to most readers. This skill does the
translation: keep the facts and the certainty exactly as they are, drop the
jargon and the filler, and post it where the right people will see it.

## Workflow

Follow these in order. Each step is short; the judgment is in getting the target
and the wording right.

### 1. Gather the content

By default, pull the substance from what we just did in this conversation — the
investigation, the fix, the measurements, the plan. If the user pasted or
described something to post instead, use that. Don't invent detail that isn't
there; if a key fact is missing (a number, a link, an outcome), ask rather than
guess.

### 2. Resolve the target

Find where this should go. Look, in this order:

1. **What the user said.** "post to PR 123", "comment on ABC-45", a pasted URL —
   that wins over anything inferred.
2. **URLs/keys in the conversation.** Scan the context for:
   - GitHub PR: `github.com/<org>/<repo>/pull/<n>`
   - GitHub issue: `github.com/<org>/<repo>/issues/<n>`
   - Jira: `<site>.atlassian.net/browse/<KEY>`, or a bare key like `ABC-123`
3. **The current repo**, only for GitHub and only if a PR/issue number is
   already known — `gh` can resolve the repo from the working directory.

If exactly one target is clear, use it. If there are several, or none, ask which
one — don't pick for them. A wrong target posts private work to the wrong
audience, so this is worth a question.

### 3. Pick a template

Match the content to one of the templates in the section below. They're guides,
not forms — bend them to fit. If the content is a mix (findings _and_ a plan),
combine the relevant parts. If nothing fits, just write plainly in the voice
below.

### 4. Write the comment

Write it in the voice and formatting rules below. First person, since it posts
as the user. This is the part that makes the skill worth using — take the care
here.

### 5. Show the draft and confirm

Show the user, compactly:

- **Where**: repo + PR/issue number, or Jira key + issue summary (fetch the
  summary with `getJiraIssue` so they can confirm it's the right ticket).
- **What**: the full drafted comment body, exactly as it will post.

Then ask if it's good to post. Wait for a yes. If they want changes, revise and
show it again. Never skip this — a tracker comment is public to the team and
can't be quietly unsent.

### 6. Post it

Use the platform's method (details in "Posting" below). Pipe the body via a file
or stdin, not a shell-quoted string, so backticks, quotes, and newlines survive
intact.

### 7. Report back

Give the user the link to the posted comment so they can check it or share it.
`gh` prints the comment URL; for Jira, give the issue URL.

## Voice and formatting

The voice mirrors the `/human` command: a short message a busy engineer would
actually send, not a formal document. The reader may be non-technical, so:

- **Lead with the point.** First line says the outcome or the ask. Detail comes
  after, only if it changes what the reader does.
- **Short.** One or two short paragraphs plus, at most, a small list or table.
  Cut filler, not facts.
- **Plain words.** No corporate speak, no buzzwords, no fancy wording. Spell out
  or skip jargon — if a term is unavoidable, say what it means in a few words.
- **Keep the certainty exactly as it is.** "Should be fixed" stays tentative. A
  failure stays a failure. An unknown stays an unknown. Never let the wording
  make the work sound more done or more confident than it is.
- **First person, no greetings or sign-offs** unless the user asks for them.
- **Don't drop caveats or bad news** to make it shorter or nicer. Brevity comes
  from cutting fluff, never substance.
- **Nothing that only works on your machine.** No local tool commands, no local
  file paths, no ids from your own bookkeeping. The reader can't run or look any
  of it up, so it reads as an instruction they can't follow. Say the outcome
  instead: what you found, what changed, what you need from them.

On markdown: trackers render it, so light structure is allowed where it genuinely
helps a reader scan — short bullet lists, one small table for numbers, a fenced
code block for a log line or error. Keep it minimal: no headers unless the
message really needs sections, and no nested lists. When in doubt, plainer wins.

## Templates

**Investigation / findings** — the main case: something was looked into, here's
what turned up.

```
<one line: the symptom or question, and the headline answer>

What I found: <root cause / conclusion in plain terms>
<0–3 short bullets of supporting detail, or a short code/log block if it helps>

Next: <the fix, a PR link, or what's needed from the reader>
```

**Numbers / results** — sharing measurements, benchmarks, before/after.

```
<one-line takeaway: what the numbers say>

| <metric> | <before> | <after> |
| --- | --- | --- |
| ... | ... | ... |

<optional: one line of caveat or next step>
```

If it's only a couple of numbers, skip the table and put them in the sentence.

**Plan / proposal** — proposing an approach and, usually, asking for something.

```
<one line: what I'm proposing and why>

Plan:
1. <step>
2. <step>

<one line on the main tradeoff or risk, if there is one>
<what I need from you: a decision, a review, access, a go-ahead>
```

**Quick note** — one small thing: a status confirmation or a single fact. No
structure at all, just one or two plain sentences.

```
<the thing, plainly>
```

## Posting

### GitHub (use `gh`)

`gh` is installed and resolves the repo from a full URL, so pass the URL when you
have it. Send the body over stdin so formatting survives.

- PR comment:
  ```bash
  gh pr comment <pr-url-or-number> --body-file - <<'EOF'
  <the drafted body>
  EOF
  ```
- Issue comment:
  ```bash
  gh issue comment <issue-url-or-number> --body-file - <<'EOF'
  <the drafted body>
  EOF
  ```

If only a number is known (no URL), run it from inside the repo so `gh` resolves
it. If `gh` reports it's not authenticated, tell the user to run `gh auth login`
— don't try to work around it.

### Jira (use the Atlassian MCP)

1. Get the `cloudId`. Try the site hostname from the URL first (e.g.
   `company.atlassian.net`) — pass it straight as `cloudId`. If that call fails,
   call `getAccessibleAtlassianResources` and use the returned id.
2. Get `issueIdOrKey` from the URL (`/browse/ABC-123` → `ABC-123`) or from what
   the user said.
3. In step 5, fetch the issue summary with `getJiraIssue` (fields: `summary`) so
   the confirmation shows which ticket it is.
4. Post with `addCommentToJiraIssue`:
   - `cloudId`, `issueIdOrKey`, `commentBody` = the drafted text
   - `contentFormat: "markdown"` — so the light markdown renders as written
5. Give the user the issue URL to check the comment.

If the Atlassian MCP tools aren't available in this session, say so plainly and
ask the user to connect it — don't fall back to some other channel.

## Notes

- One target per post. If the user wants it on both a PR and a Jira ticket, treat
  them as two posts (each may need slightly different wording for its audience) —
  confirm each.
- Don't post secrets, tokens, internal file paths, or customer data that
  wandered into the session. If the content contains something that shouldn't be
  public to the tracker's audience, flag it before posting.
- Adding a new comment is the default. Only edit or reply to an existing comment
  if the user asks for that.
