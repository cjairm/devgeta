---
description: Write this branch's handoff note before ending a session, so the next one starts with context instead of a cold read
---

Distill what a fresh session needs to continue this branch, then store it with
`devgeta task handoff --write`. Ending a session and starting a new one costs
far less than growing one session forever — but only if what mattered survives
the gap. That is what this writes.

## Usage

```
/save-context [anything you want carried forward verbatim]
```

- **With arguments**: include that text in the note, as given, alongside what you
  distill yourself.
- **No arguments**: distill from this session alone.

## Process

### 1. Read what is already there

```bash
devgeta task handoff --read
```

`--write` **replaces** the note; it never appends. So anything in the existing
note that is still true and still useful has to be carried into the new one, or
it is gone. Anything it says that this session has since done, changed, or
disproved gets dropped or corrected — a stale note is worse than no note.

A branch with no note prints a clean message and exits 0. That is the normal
state for a fresh branch, not an error.

### 2. Decide what is worth carrying

Write for someone who has this repository and its git history, and nothing else
— no memory of this conversation. Cover only what they cannot get from the code:

- **Goal** — what this branch is trying to do, in one or two sentences.
- **State** — what is done and verified, what is written but unverified.
- **Next step** — the single most useful thing to do next, concretely.
- **Decisions** — a choice made this session and the reason for it, especially
  where the code alone would look arbitrary or wrong.
- **Blockers** — what is failing, what is waiting on someone, what was tried and
  did not work.
- **Landmarks** — the few file paths and the exact commands (tests, build, repro)
  the next session will want, so it does not have to search for them again.

Leave out anything reconstructible: file contents, diffs, full test output, the
commit list, a narration of the session. Point at them instead — `git log`,
`git diff`, a path, a command. Keep it short; the note is read at the start of
every following session on this branch, so its length is a recurring cost.

Never write a credential, token, or anything else secret into the note.

### 3. Write it

Multi-line text goes through a file, not `--note`, so nothing in it can be
mangled by the shell:

```bash
SCRATCH=$(devgeta task scratch)

cat > "$SCRATCH/handoff.md" <<'EOF'
Goal: ...

Done: ...

Next: ...

Notes: ...
EOF

devgeta task handoff --write --note-file "$SCRATCH/handoff.md"
devgeta task scratch --clean "$SCRATCH"
```

Use `--note "..."` only for a genuinely single-line note.

The note is capped at 8 KiB rendered. Over the cap, the write is **refused
outright** — the previous note is left exactly as it was and nothing is silently
truncated. Cut it down and write again; do not retry the same text.

### 4. Confirm

Say which branch was written and, in two or three lines, what the note now
says. If the note replaced an earlier one, say what you dropped from it.

## Rules

- MUST run the write yourself, without asking — running this command is the
  authorization to store the note. Do not hand the text back for approval first.
- One note per branch. `--branch <name>` targets another branch; omit it for the
  current one.
- A detached HEAD has no branch, so it has no note. The command says so and
  exits 0 — report that instead of trying to work around it.
- Never store secrets, and never store what git already records.
- Nothing here calls a model on your behalf: the command stores exactly the text
  you give it. The judgment about what is worth keeping is yours.
- Read it back with `/load-context`, and clear it with
  `devgeta task handoff --clear` once the branch is finished.
