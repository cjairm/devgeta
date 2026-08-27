---
description: Load this branch's handoff note and orient in the repo before starting work
---

Pick up a branch where the last session left it: read its handoff note, check
the note still matches reality, and report where things stand. Read-only — this
command orients, it does not start the work.

## Usage

```
/load-context [branch]
```

- **No argument**: the current branch.
- **Branch given**: that branch's note (`--branch <name>`).

## Process

### 1. Read the note

```bash
devgeta task handoff --read
```

A branch with no note prints a clean message and exits 0 — that is the normal
state for a fresh branch, not an error. Skip to step 3.

### 2. Check the note against the tree

The note is a claim written at a past commit, not the current truth. It stamps
the HEAD it was written at, and `--read` tells you when HEAD has moved since.

- **HEAD unchanged**: take the note at face value.
- **HEAD moved**: the tree has advanced past what the note describes. Find out
  how far before trusting its "next step":

  ```bash
  git log --oneline <stamped-head>..HEAD
  git status --short
  ```

  If commits landed after the note, its next step may already be done. Say so
  rather than repeating work.

### 3. Orient, cheaply

Whether or not there was a note, get the minimum needed to know where you are:

```bash
git status --short
git log --oneline -5
```

Read further only where the note or the diff points you, and only what you need.
The whole purpose of a handoff note is to avoid re-reading the codebase to
rebuild context — so re-reading it anyway spends exactly the tokens this is
meant to save.

### 4. Report

Reply with a short summary, under 150 words:

- What this branch is doing and how far it got.
- The next step, adjusted for anything that changed since the note.
- Anything the note flagged as blocked, unverified, or uncertain.
- If there was no note: say so plainly, and give what the git state alone shows.

Then stop. Do not begin the work unless the user asks for it — loading context
and acting on it are two separate decisions, and the second one is theirs.

## Rules

- Read-only. Never modify a file, and never run a state-changing command — no
  commit, no push, no checkout, no branch switch.
- Never treat the note as authoritative over the code. Where they disagree, the
  code wins and you say the note is stale.
- Do not silently fill gaps. If the note is thin or contradicts the tree, report
  the gap instead of inventing the missing plan.
- Do not rewrite or clear the note here — `/save-context` writes it, and
  `devgeta task handoff --clear` removes it when the branch is done.
