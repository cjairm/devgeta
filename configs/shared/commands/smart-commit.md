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

Write the generated message to a scratch file, validate it, then commit the
validated bytes:

```bash
SCRATCH=$(devgeta task scratch)

# Write the full message (subject + blank line + body) to the scratch file.
cat > "$SCRATCH/commit-msg.txt" <<'EOF'
Subject line

Body explaining why.

Closes #123
EOF

# Validate: if this commit's message carries a trailer you generated (e.g. a
# Co-authored-by line the user asked for), pass --require for that exact key.
# Generated none this time? Pass no --require — the check is then a
# successful no-op. Never hardcode a key: which trailers matter is a
# per-repository decision, not this command's.
devgeta task commit-trailers --message-file "$SCRATCH/commit-msg.txt" \
  [--require <key-you-generated> ...]

# Commit the SAME validated bytes. `git commit -m`/`-F -` cannot guarantee
# that; `-F <file>` reads exactly what was validated.
git commit -F "$SCRATCH/commit-msg.txt"

devgeta task scratch --clean "$SCRATCH"
```

`git commit -m "Subject" -m "Body"` is not equivalent here: it reassembles
the message from separate `-m` arguments rather than committing the file
`commit-trailers` just validated, so a discrepancy between the two could
slip through uncaught.

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
