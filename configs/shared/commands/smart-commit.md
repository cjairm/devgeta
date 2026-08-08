---
description: Create focused commits from staged changes
model: haiku
---

Create small, reviewable commits. Commits are granular steps; PRs show the complete picture.

## Process

### 1. Check Staged Changes

```bash
git status --short
git diff --cached --stat
```

If stat insufficient: `git diff --cached`
If nothing staged: inform user to run `git add`.

### 2. Get Commit Style

```bash
git log --oneline -5
```

Match repo conventions (conventional commits, prefixes, etc.).

### 3. Validate Scope & Size

**Good:**

- One self-contained change (one part of feature, not whole feature)
- ~100 lines is reasonable, 200-500 acceptable if focused

**Warn and suggest splitting:**

- Multiple unrelated changes
- Refactor mixed with behavior change
- 1000+ lines
- Changes spread across many unrelated files

**Splitting strategies:**

- Separate refactorings from features/fixes
- Split by layer (model, API, client)
- Split by sub-feature
- Tests for existing code can be separate

### 4. Generate Message

**Subject (max 70 chars):**

- Imperative: "Add" not "Added"
- Specific: "Fix login crash on empty password" not "Fix bug"
- Match repo style
- No period
- Include $ARGUMENTS context if provided

**Body (complex changes only):**

- Blank line after subject
- Explain WHY (diff shows what)
- Wrap at 72 chars
- Reference issues: "Closes #123"

### 5. Execute

Run git commit with generated message:

```bash
git commit -m "Subject line"
```

With body:

```bash
git commit -m "Subject" -m "Body explaining why.

Closes #123"
```

After commit: show hash and message. If large, suggest splitting next time.

## Rules

- MUST execute the `git commit` command yourself, without asking — running this command is the authorization to commit. Do not hand the message back for approval and wait for a go-ahead.
- One logical change per commit
- Include related tests in same commit
- Refactors separate from behavior changes
- Each commit leaves system working
- Smaller preferred over larger

## Examples

**Good:**

```
Add user profile caching

Redis with 5-min TTL. Improves response ~30%.
```

```
Fix crash on empty password login

Closes #410
```

**Bad:**

- "fix stuff" / "updates" / "WIP" - not descriptive
- "Add feature X with refactoring" - multiple concerns

#### References

- https://google.github.io/eng-practices/review/developer/
- https://gist.github.com/hcastro/52c5824a747b901c289261518504effb
