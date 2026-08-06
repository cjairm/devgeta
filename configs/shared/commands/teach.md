---
description: Teach a work item — ticket, issue, PR, or idea — in plain language: what it is, why it exists, and how the flow works, shown as a small diagram instead of walls of prose
---

Teach one work item in the fewest words that still carry every important fact.

Length is a feature here. A reader who has to scroll has already lost the thread, so a long explanation is a failed one. Show the flow as a small diagram instead of describing it in prose, and use one worked case instead of a list of examples.

## Usage

```
/teach [JIRA-KEY | github issue/PR number or URL | topic]
```

## Process

### 1. Get the work item

- **Jira key** (`PROJ-123` shape): fetch it with the Jira tools, including comments and linked issues.
- **GitHub issue/PR** (number or URL): fetch it with `gh issue view` / `gh pr view --comments`.
- **Anything else**: treat it as a topic and work from the conversation plus the code.
- **Nothing**: teach the item currently being discussed.

### 2. Ground yourself

Read the code, configs, or docs the work touches. Every name, command, and failure you mention must be real. Never invent a placeholder.

### 3. Pick what's load-bearing

Before writing, decide the two to four facts a reader must hold to work on this. Everything else gets cut — including facts you found while reading and facts already stated in the conversation. This step is the whole job; skipping it is what produces bloat.

### 4. Write it

## Shape

These parts, in this order. No others.

1. **What this is** — one or two sentences. Plain words, no ticket-speak.
2. **Why it exists** — one or two sentences: the problem, and who feels it.
3. **The flow** — a diagram (see below). For a bug: today, expected, and where they split.
4. **Watch out** — real risks, edge cases, and anything important the source leaves unspecified, named as an open question. Never guessed. Skip the part entirely if there is nothing real to put in it.
5. **References** — only if you have real sources to cite. Otherwise omit.

Close with one line offering to go deeper on a specific part. Do not pre-answer questions nobody asked.

## Diagrams

A workflow — one path, real names:

```
dg wt create #42
  -> read issue title from gh
  -> git worktree add ../repo-42
  -> tmux window: [ claude /teach ] [ make finit ]
  -> print window name
```

A bug — today against expected, with the split marked:

```
today:     git tag v1.9 -> workflow reads tag annotation -> release body EMPTY
                                    ^ a bare tag has no annotation to read
expected:  task release -> annotated tag from --message-file -> body = the notes
fix:       task release always annotates; a bare git tag is rejected
```

Rules: 12 lines or fewer, ASCII only (this renders in a terminal), one idea per
diagram. If it needs a legend, it is too complex — simplify or split it.

Add a worked case **only** when the diagram leaves a real question open. One case, three lines: situation, what happens, expected result. If it just re-walks the diagram, delete it.

## Length

Under 300 words plus the diagram for a typical item. Priority, in order: keep every load-bearing fact and caveat, then say it in plain words, then make it as short as those two allow. Never drop a fact to hit the count — if the item genuinely needs more, go over and say what you left out.

## Do not write

- Project background, history, or "what this repo is" framing.
- Anything already said in this conversation.
- An implementation tour, a file-by-file summary, or code blocks over five lines.
- Multiple examples, or an example that repeats the flow.
- A section with nothing real in it. Drop it; never pad it.
- Preamble, closing recap, or "in summary". The first line is content.
- Jargon. Spell a term out in plain words the first time, or don't use it.

## Rules

- Write for a mixed audience: a product manager and a senior engineer should both get it on one read. That means plainer words, not fewer facts.
- Don't teach the basics (what a PR is, what a test is). Unfamiliar with this system is not unfamiliar with software.
- Don't invent requirements. Silence in the source becomes an open question under "Watch out".
- Never modify files or run commands that change state — this command only explains.
