---
description: Open a PR from the current branch — generate a title and body from the branch's commits and diff, honoring the repo's PR template
temperature: 0.1
permission:
  write: allow
  edit: deny
  bash:
    "*": deny
    "devgeta task *": allow
    "git branch*": allow
    "git push*": allow
    "git status": allow
    "cat .github/*": allow
    "test -f *": allow
---

Open a pull request describing the branch's overall impact. Commits are granular steps; the PR shows the complete picture — what changed, why, and how to verify it.

## Process

### 1. Gather context

```bash
devgeta task review-scope --bodies
```

This reports the branch, the repo's actual default branch (never assume `main`), ahead/behind, commit lines (short SHA, ISO date, subject) each with its commit body indented beneath it, and a per-file stat table (lockfile-style noise excluded and noted).

If the stat isn't enough to understand the change, read the full diff: `devgeta task branch-diff` (or `--file <path>` for one file). Self-review it here — catch the obvious before a reviewer spends attention on it.

### 2. Check for a PR template

```bash
test -f .github/PULL_REQUEST_TEMPLATE.md && cat .github/PULL_REQUEST_TEMPLATE.md
```

If one exists, it is the source of truth for the body's structure. Fill its sections; delete any that don't apply.

### 3. Analyze the change

From the commits + diff, work out:

- **What** changed (one line — the reviewer can read the diff for mechanics)
- **Why** it was needed (the problem, the business/technical motivation, links)
- **How to verify** it (the command you ran, steps, or what to look at)
- **What could fail** (risk, breaking changes, migrations, what's _not_ included)

### 4. Generate the title and body

**Title — Conventional Commits, imperative, ≤72 chars:** `type(scope): description`.

- Types match this repo's history: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`.
- Standalone — understandable without the body. Prefix a ticket ID from `$ARGUMENTS` when given.
- Good: `fix(auth): prevent token refresh race on concurrent requests`
- Bad: `Fix bug`, `Update code`, `Phase 1`

**Body — use the repo template if present; otherwise this lean shape (every section deletable):**

```markdown
## What & why

<!-- One or two sentences: the problem and the change. Link the ticket. -->

Closes #

## How to test

<!-- The command you ran or steps to verify. Delete if trivially obvious. -->

## Notes for reviewer

<!-- Non-obvious decisions, tradeoffs, areas to scrutinize. Delete if none. -->

## Risk

<!-- Breaking changes, migrations, config/secret changes, rollback. "None" otherwise. -->
```

**Describe intent and impact, never mechanics** — the code is the source of truth for _how_. The trap is restating the diff:

- ❌ "Changed a variable name, updated some CSS, modified the API call." (narrates the diff — verbose _and_ useless)
- ✅ "Replaced static error messages with one that pulls the user's recent actions from the session log, so failures carry context." (shorter, actually informative)

Don't pad. A one-line PR ("bump dep to patch CVE-X") gets one line — match the body to the size of the change, not the size of the template.

**Write plainly.** The body must be understandable by any engineer, including a junior one: everyday words, short sentences, no fancy vocabulary or filler.

### 5. Create the PR

The body is **GitHub-Flavored Markdown** and renders as written — headings, lists, fenced code, tables, and task lists all work. Write it as Markdown, not plain text.

Prefer `--body-file` over inline `--body`: a body with fenced code blocks or backticks will be mangled by the shell when passed inline (backticks trigger command substitution inside double quotes, apostrophes break single quotes). Write the body to a scratch file, then point at it:

```bash
git push -u origin $(git branch --show-current)
# write the assembled Markdown body to /tmp/pr-body.md, then:
devgeta task create-pr --title "<title>" --body-file /tmp/pr-body.md   # add --base <branch> for a non-default target
```

Inline `--body "<text>"` is fine only for a trivial one-line body with no backticks or apostrophes.

`devgeta task create-pr` prints the new PR's URL. If it fails, output the title + body so the user can open the PR manually.

## Rules

- One concern per PR. A small, focused PR reviews better and hides fewer bugs than a 2,000-line one — note explicitly if this is part of larger work.
- Always generate the title/body from analysis — never `--fill`.
- Never modify commits.
- The repo template is the source of truth when it exists.
- Write for a developer reading this years later without your context: why over what, decisions not evident in the code, areas needing extra review.

## References

- Google Engineering Practices — writing the change description: https://google.github.io/eng-practices/review/developer/
- Conventional Commits: https://www.conventionalcommits.org/
- PR description anti-patterns (describe why, not the diff): https://gist.github.com/hcastro/52c5824a747b901c289261518504effb
