# Investigation — [What Was Investigated]

**Date:** YYYY-MM-DD
**Status:** Open | Concluded | Superseded by [link]
**Feeds:** [cycle doc / ADR this informs, or "none yet"]

<!--
An investigation gathers evidence before a decision. It records what was found,
where it came from, and what we recommend — it does not decide. The decision
goes into an ADR (docs/decisions/), the work into a cycle doc (docs/plans/cycles/).

Write one when a choice depends on facts nobody has checked yet: comparing
third-party tools, probing how an external program behaves, sizing a problem.

Rules:
- Every number or claim names its source (a command, a URL, a file and line)
  and is dated. Facts about other people's projects go stale fast.
- Keep facts and recommendations apart: "Evidence" says what is true,
  "Verdict" and "Action" say what we think should happen.
- Say what was NOT checked. An unverified claim is an open question, not a
  finding.
- Nothing that changes the maintainer's machine is part of an investigation
  (CLAUDE.md §4). Probes run in a sandbox; say which one.

File name: docs/investigations/YYYY-MM-DD-<slug>.md. Delete sections that
don't apply.
-->

## Question

What are we trying to find out, and why now? One short paragraph. Link the
code, doc, or decision that raised it.

## Scope

- **In:** what this investigation covers.
- **Out:** what it deliberately leaves for later.

## Method

How the evidence was gathered, so someone can repeat it:

- Sources: [registries, APIs, docs, source code, a sandbox probe]
- When: [date the numbers were read]
- Criteria: [what "good" means here, stated before looking — e.g. "well
  maintained = commits in the last month, more than one contributor, bugs
  answered, supports the current version"]
- Not done: [anything installed, run, or measured? say so either way]

## Summary

One row per item, so the reader can see the outcome without reading on.

| Item   | Action                                           | Why, in a few words |
| ------ | ------------------------------------------------ | ------------------- |
| [name] | Keep / Drop / Add / Adopt / Reject / Needs probe | [short reason]      |

---

## Findings

<!--
Group items by action when the investigation is a keep/drop/add review of a
set (e.g. "## Keep", "## Drop", "## Add"); otherwise one heading per item or
per question. Each item uses the same four parts.
-->

### [Item name]

What it is and what it does, in one or two sentences.

- **Evidence:** [facts with sources — versions, numbers, quotes from docs or
  code, file:line]
- **Verdict:** [what the evidence means against the criteria]
- **Action:** [the concrete change: which file, which list, which doc]
- **Open questions:** [what is still unverified, if anything]

---

## Considered, not adopted

| Item   | Why not                                     |
| ------ | ------------------------------------------- |
| [name] | [reason, with the evidence that decided it] |

## Shared work

Changes that are needed by several items rather than one (shared code, ordering,
migration notes for existing users). Delete if none.

## Verification still needed

What must be probed before the recommendations can be acted on, and how to probe
it safely.

1. [probe] — answers [open question]

## Decisions this will need

Which ADRs must be written or amended, and which questions are the maintainer's
to answer.

- [ADR-XXXX §N — what changes]
