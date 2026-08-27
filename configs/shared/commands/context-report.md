---
description: Measure what loads into every session in this repo, then say concretely what is worth trimming
---

Turn "this project's context feels heavy" into numbers, then into a short list
of specific things to cut. Base context — memory files, settings, skill and
command frontmatter, agents — loads on **every** session before anyone types
anything, so a byte here costs once per session forever, not once per task.

Read-only. This command measures and recommends; it never edits a file.

## Usage

```
/context-report [layer or file to focus on]
```

- **No arguments**: report every layer, then dig into the largest ones.
- **Argument given**: report everything, but dig into only what was named.

## Process

### 1. Measure

```bash
devgeta task context-report
```

It prints, per agent, every layer that loads before the first prompt, with a
byte count, a file count, and a note on how that layer is actually loaded. Read
those notes — they change what a number means. Frontmatter-only layers (skills,
commands) cost far less than their directories suggest; layers marked
informational are not in the total at all.

The token figure is bytes ÷ 4, not a real tokenizer. Use it as an order of
magnitude and say so when you quote it. Never present it as an exact count.

### 2. Sanity-check the file counts

A count higher than expected usually means files are being picked up from
outside this directory: agents concatenate memory files from every directory
above the working one, so a checkout nested inside another repository loads
both. If the memory layer names more files than this repo has, find out where
they come from before recommending cuts to the wrong file:

```bash
ls -la CLAUDE.md AGENTS.md 2>/dev/null
```

Then check the parent directories up to the home directory. Two near-identical
copies of a large memory file, loaded every session, is a layout problem, not a
content problem — report it as such, because trimming one copy fixes half of it.

### 3. Dig into the largest layers only

Take the top two or three layers by bytes and read those actual files. Stop
there: reading everything to save reading is self-defeating, and the small
layers cannot pay for the tokens spent auditing them.

For each one, sort what you find into:

- **Earns its place** — a constraint, convention, or decision an agent would get
  wrong by default, and cannot infer from the code.
- **The code already says it** — structure, file layout, naming, dependency
  lists, anything a capable agent reads directly when it needs it.
- **Said more than once** — the same rule restated in two files, or twice in one.
- **Stale** — describes a file, flag, or workflow that no longer exists. Verify
  before claiming this; a rule you cannot find in the code may still be live.

### 4. Report

Reply with, in this order:

1. **The numbers** — per agent: total bytes, estimated tokens (labeled as an
   estimate), and the layers that dominate. A short table is fine here; the
   report's own output does not need to be pasted back in full.
2. **Trim candidates** — the specific ones, largest first, each with the bytes
   it would save and the reason it is safe to cut. Name files and sections, not
   "the docs".
3. **What must stay** — anything large that you deliberately are not
   recommending, with the one-line reason. This is as useful as the cut list; it
   stops the same file being re-audited every month.
4. **The recurring lever** — see step 5.

Recommend; do not edit. What a project restates for its contributors is the
maintainers' judgment call, and a cut that looks redundant to a reader of the
code may be there because an agent kept getting it wrong.

### 5. Recommend the recurring saving too

Base-context trimming is a **one-time** saving, and there is a floor to it — at
some point everything left genuinely needs to be there. Verbose command output
is the other half, and it recurs on every run: a test suite or build that prints
thousands of lines costs those tokens again every single time it is invoked.

Devgeta caps that at write time, off by default. Check whether it is on:

```bash
devgeta config get integrations.output_budget
```

If it prints `false`, recommend turning it on — especially when this repo's
usual test or build command is a loud one:

```bash
devgeta config set integrations.output_budget true
devgeta configure claude --force      # or: devgeta configure opencode --force
```

Either `configure` re-converges both agents. When on, a matched command's output
is captured, reduced to head/marker/tail **only if** it exceeds the budget, and
the complete output is left on disk at a path the marker names — nothing is
lost, it just stops sitting in the transcript. Exit status is preserved exactly.

Say plainly what it does not cover, so the recommendation is honest: only single
commands are rewritten (anything joined with `&&`, `||`, `;`, or `|` is left
alone), and only commands devgeta's built-in rules recognize.

Suggest the setting; do not run `config set` or `configure` yourself unless the
user asks — both change how their agents are configured.

## Rules

- Read-only: never edit, delete, or reformat a file this command measured.
- Quote the token figure as an estimate, always. It is a byte count divided by
  four.
- No vague advice. "CLAUDE.md is large" is what the report already said; the
  value added here is which sections, how many bytes, and why they are safe.
- Bound the reading. A context audit that itself burns a large context has
  spent the saving before anyone acts on it.
- Verify before calling anything stale, and say so when you could not verify.
