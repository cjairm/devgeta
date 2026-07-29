---
description: Re-explain the previous answer or a given topic in plain language for someone new to the project
temperature: 0.1
permission:
  write: deny
  edit: deny
  bash:
    "*": deny
---

Explain something so a person with zero project background understands it on first read. This is a fresh explanation, not a sentence-by-sentence rewrite — keep every technical fact and caveat, change only how it's told.

## Usage

```
/explain-simply [topic]
```

## Process

### 1. Pick the mode

- **Topic given** (`$ARGUMENTS` is non-empty): explain that topic. Read the relevant code and docs first so every fact is accurate — never explain from guesswork.
- **No topic**: re-explain the previous answer, and only that answer.

### 2. Stay in scope

Scope depends on the mode:

- **No topic**: cover exactly what the previous answer covered, nothing wider. Do not add background, history, or "what this project is" framing it didn't have. Do not pull in related topics, files, or facts it never mentioned.
- **Topic given**: cover only that topic. Getting its facts from the code and docs is required, not scope creep — that's where the facts come from. What's out of scope is everything next to it: adjacent features, neighboring files, how the rest of the project works.

In both modes: explain a term only when it appears in what you're explaining and blocks understanding — one short sentence, then move on. "Assume no prior knowledge" sets the reading level, not the scope. It never means re-explaining the whole project.

### 3. Write the explanation

Follow every rule below. Explain the parts that matter for understanding; whenever you leave something out, say what you skipped.

## Writing rules

- Assume no prior knowledge of the project, its tools, or its history.
- Use simple English. Any engineer, regardless of seniority, should understand it on one read.
- Short paragraphs. One idea each.
- No buzzwords, no filler, no "LLM-sounding" prose.
- Spell out every term, codename, and abbreviation the first time it appears.
- Replace references ("see the ticket", "per the ADR", "as discussed in Slack") with a one- or two-sentence explanation of what that source says. Keep a reference only in a **References** section at the end, and only if someone might genuinely want to read more.
- Prioritize understanding over precision of wording — but never change a technical fact to make it read better.
- If simplifying would drop a caveat, limitation, or failure, keep the caveat and simplify its wording instead.
- Length, in this order of priority: keep every technical fact and caveat first, spell out the terms that block understanding second, and only then make it as short as those two allow. Never drop a fact to hit a word count.
- With that satisfied, short is the target: under 200 words for a typical answer, and usually shorter than the source, since plain words are shorter than jargon. If yours runs longer than the source, check whether you added scope instead of clarity — that's the usual cause, not the facts.

## Rules

- Never modify files or run commands that change state — this command only explains.
- Facts come from the code, docs, or conversation — if something can't be verified, say so instead of smoothing it over.
- Structure (headers, lists) only when it genuinely helps a first-time reader scan.
