---
description: Finish a piece of work end to end — sweep its docs for approval, verify, land it on the default branch locally, and report the worktree's disposition
---

Finish the current branch: propose flipping any document status this branch left un-approved, verify the work, merge it into the default branch locally, and report what happened. This never pushes.

## Authority to commit and merge

Running this command **is** the authorization to commit the doc-status edits (step 4) and to run the merge (step 5). **Do not ask "shall I commit?" or "shall I merge?" — just do both, then report.**

This authorization covers only steps 4 and 5. It does **not** cover step 2: each individual document's status flip still needs its own explicit yes from you before it's made — invoking `/finish-work` does not pre-approve any of them. It also does not imply a push — the merge this command runs never pushes, and nothing else in this command does either.

## Usage

```
/finish-work [name]
```

`name` is the worktree to finish, resolved exactly the way `devgeta task worktree-finish` resolves it: an explicit name wins, otherwise the current directory (if it's inside a linked worktree) wins, otherwise the command errors and lists what it found. Omit `name` when running from inside the worktree you want to finish — the normal case.

## Process

### 1. Orient and check readiness

```bash
devgeta task review-scope
devgeta task worktree-finish [name] --check
```

`review-scope` gives you the shape of the change (commits, files touched). `--check` is the read-only readiness report this whole command drives on — it never mutates anything. Read its full output before doing anything else; every later step reacts to a specific line in it.

If its `ready:` line says `ready: no — <reason>`, **stop here and report the reason** — do not route around it. Say what would clear it:

- A dirty worktree or a dirty/wrong-branch main checkout: fix that by hand, this command does not do it for you.
- An open review-journal finding: it can be cleared with `devgeta task review-note --settle --id <id> --as answered|rejected|fixed --note "<text>"`. You may **propose** a specific settlement, but never settle it yourself without the maintainer's yes — the resolution and its note are their call, not this command's.
- An unanswerable divergence probe: report the underlying git error verbatim; there's nothing this command can decide on your behalf.

Everything below assumes `ready: yes`, or a `ready: no` whose blocker has just been cleared by hand and re-checked.

### 2. Propose document status flips

The check output includes one `doc-status: <path>: <marker>` line per changed or untracked Markdown file. `<marker>` is that file's raw status value, handed over verbatim — `--check` never judges which values count as finished, and neither do you by assumption. That judgment has to be discovered fresh, from whatever repo this command is running in, every time.

For each `doc-status:` line with an actual marker (skip any line reading `no status marker` unless step 2c below finds a reason to look again, and skip the `doc-status: unknown (...)` sentinel entirely — it means the whole sweep couldn't run, not that a document needs a decision):

**a. Discover this document's status vocabulary.** Starting in the document's own directory and walking up toward the repository root (never past it), try these in order and stop at the first that yields a vocabulary:

1. The nearest template for documents of this kind, at or above the document. A document's template is not always a sibling — it can sit a directory above the folder that holds the document itself. Recognize a template by its name (`TEMPLATE.md`, `template.md`, `_template.md`, `0000-template.md`, or similar), and read the allowed status values off that template's own status line or section.
2. A README or index encountered on that same walk that documents what the statuses mean (a legend next to a set of similarly-tracked documents).
3. The values this document's siblings already use for the same marker, when the directory ships neither a template nor a legend.

**b. If none of the three yields a vocabulary, ask the maintainer** which value (if any) means "final" for this document — never guess, and never assume a vocabulary from another kind of document in the same repo applies here.

**c. A `no status marker` line is not always final.** `--check` only recognizes three shapes for a status marker (a front-matter `status` key, a header-block label line, or a `Status` section). A repo can track status a different way entirely, and every one of those arrives as `no status marker`. If the walk in (a) finds a template for this document's kind whose own status marker uses a shape `--check` didn't recognize, read the document the way that template writes it, and propose from there. If the walk finds nothing — no template, no legend, no siblings with a marker — leave the document alone; it is not a status-approval subject.

**d. Propose the flip, one document at a time, and wait for an explicit yes before making it.** Never batch-approve, and never treat silence or a general "looks good" as approval for a specific document. A decline or "not yet" leaves that document exactly as it is.

### 3. Verify

Build, lint, and run the tests for what changed — using **this repo's own convention**, not any convention borrowed from another project. Look for it in whatever this repo documents: a `CONTRIBUTING.md`, a `CLAUDE.md`/`AGENTS.md` section on which tests to run, a `package.json` script, a Makefile target, or similar. When nothing documents a convention, run the full test suite. Report the actual result — do not claim a pass you didn't observe.

If verification fails, stop and report the failure. Do not commit or merge broken work.

### 4. Commit the doc edits

Only the documents whose flip was approved in step 2 get committed here — nothing else.

```bash
git add <approved-doc-paths>
git commit -m "<message>"
```

Run `add` and `commit` as two separate commands: a pre-commit secret-scanning hook can only scan what's already staged, so a combined invocation would scan nothing.

If step 2 approved no flips at all, skip this step — there's nothing to commit.

### 5. Merge

```bash
devgeta task worktree-finish [name] --merge
```

This rebases onto the default branch only if it has diverged, fast-forward-merges into the default branch from the main checkout, then removes the worktree and deletes the branch. It never pushes. If it refuses (a dirty worktree, a dirty or wrong-branch main checkout, an open journal finding, or an unanswerable divergence probe), report the refusal exactly as printed and stop — do not retry around it.

### 6. Report

The worktree is gone once step 5 succeeds, so this step runs from the **main checkout**, not the removed worktree. Compute how far the default branch now sits ahead of its own upstream:

```bash
git -C <main-checkout> rev-parse --abbrev-ref <default-branch>@{upstream}
```

If that resolves, count commits with:

```bash
git -C <main-checkout> rev-list --count <default-branch>@{upstream}..<default-branch>
```

and report that count. `@{upstream}` resolves whatever remote the branch actually tracks — never assume `origin`.

If the upstream lookup fails (the repo has no remote configured for this branch, which is a normal setup, not an error), report the count as **unknown — no upstream configured**, never a guess. This is a reporting step only: the merge already succeeded by the time you compute this, so a lookup failure here is never treated as a command failure and never produces a non-zero exit.

## Output

Return only this — no preamble, no narration:

```
## Finish work — <branch>

### Document status
| Document | Was | Proposed | Outcome |
|----------|-----|----------|---------|
| path     | ... | ...      | approved / declined / n/a |

(or: "No non-final status markers found — nothing proposed.")

### Verification
<result of the repo's own build/lint/test run>

### Merge
<"Merged <branch> into <default>; worktree removed." OR "Blocked: <--check's or --merge's reason, verbatim>">

### Push
Nothing was pushed.

### Default branch
<N> commits ahead of its upstream. (or: "unknown — no upstream configured")
```

## Notes

- This command never pushes, and never runs any part of a release process — landing on the default branch locally is the entire job.
- A `ready: no` block from `--check` is reported and the command stops there. It never routes around a block, and it never settles a journal finding on its own — settling is always the maintainer's call, even when this command proposed the resolution.
- `--check`'s advisory lines — predicted merge conflicts, stale journal findings — are informational only. They are shown in the readiness report but never block a merge by themselves; only the report's own `ready:` line decides that.
- Each document's status flip needs its own yes. The authorization in this file's opening section covers steps 4 and 5 only, never step 2.
