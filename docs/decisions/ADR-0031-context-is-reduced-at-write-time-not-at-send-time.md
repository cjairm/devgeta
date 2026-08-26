# ADR-0031 — Context is reduced at write time, never at send time

**Date:** 2026-08-25
**Status:** ACCEPTED

## Context

Devgeta's users hit their Claude/OpenCode plan limits. The obvious lever is to
send fewer tokens, and there is a growing class of tools that offer exactly
that: a compression proxy that sits between the coding agent and the model
provider and shrinks the request on its way out. [Headroom][headroom] is the
current example — an Apache-2.0, local-first proxy with a `headroom wrap claude`
entry point, advertising 60–95% fewer tokens.

The question this ADR answers: **should devgeta ship, install, or recommend a
request-rewriting layer as its answer to token cost?**

Devgeta already ships one thing in this space. `rtk` (the ai-tools category,
[ADR-0004](ADR-0004-ai-tools-install-category.md)) filters the output of a CLI
command _before the agent ever sees it_ — `git status` becomes `rtk git status`
and returns a compact rendering. That is a different mechanism from a proxy, and
the difference turns out to be the whole decision.

### Three constraints

**1. Prompt caching is the dominant cost term, and it is prefix-matched.**

A coding agent resends the entire conversation on every turn. What makes that
affordable is provider-side prompt caching: the stable prefix is billed at the
cached-read rate. Cache hits require an **exact prefix match** — change any byte
mid-prompt and every token after it is invalidated and reprocessed at full rate.
Anthropic's own cost guidance names cache misses as one of two behaviors that
dominate usage growth in a long session, and the cache lifetime is one hour on a
subscription (five minutes on usage credits or an API key).

The measurement study on this ([Local-Splitter][localsplitter], seven tactics on
coding-agent workloads) found prompt caching and local pre-filtering of tool
output to be the two tactics that delivered savings without a quality penalty,
while aggressive summarization _backfired_ — simple truncation outperformed it,
because information loss made the agent redo work.

So a layer that recomputes a compressed request on every turn is fighting the
mechanism that was already saving the money. It can cost more than it saves, and
the savings it reports are measured in tokens sent, not in tokens billed.

**2. Prompt compression is an attack surface, upstream of the model's
alignment.**

[CompressionAttack][compressionattack] demonstrates that an attacker who
controls text the agent reads — a web page, a dependency README, a log line —
can make character-level edits that steer a perplexity-based compressor into
dropping exactly the tokens that anchor correct behavior: negations,
disclaimers, numbers. Up to 80% attack success at 0.98 similarity to the
original text; every mitigation the authors tested detects under 5%.

A compressor is not safety-aligned, and it runs _before_ the component that is.
Devgeta ships guard hooks (`secret-guard`, `suppression-guard`,
`agent-config-guard`, `task-redirect`) whose entire job is to be a trustworthy
layer between the agent and the world. Adding an unaligned rewriter above them
inverts that.

**3. Whatever devgeta ships lands on machines that are not ours.**

[Principle 8](../../CLAUDE.md#3-product-principles). A proxy is a process that
sees every prompt, every file the agent reads, and every credential that passes
through a tool result. Even a local, open-source, well-behaved one is a new
trusted component in every user's loop, installed by us, that we do not
maintain and cannot test against. The bar for that is higher than "it saves
tokens."

Note what is _not_ a reason here. Headroom's OSS proxy runs locally and does not
route prompts to a vendor; its only outbound call is a daily update check
disabled by `HEADROOM_UPDATE_CHECK=off`. The privacy objection people reach for
first applies to hosted/managed tiers, not to this. We are rejecting the
approach on caching, attack surface, and shipped-trust grounds — not on a
privacy claim that does not hold.

## Decision

**Devgeta reduces context by shrinking content at the moment it is produced, and
never by rewriting a request in flight.**

Concretely:

- **Write-time reduction is the supported mechanism.** A tool's output is
  filtered, capped, or summarized _once_, when the tool runs, and the reduced
  form is what enters the transcript. It is then frozen: identical on every
  subsequent turn, so the cached prefix stays intact and the saving compounds
  instead of being re-paid.
- **Send-time reduction is out of scope.** Devgeta will not ship, install,
  auto-configure, or recommend a proxy, wrapper, or middleware that rewrites the
  request between the agent and the provider. This covers compression proxies,
  context-editing middleware, and any "wrap the agent" entry point.
- **The mechanism is deterministic and inspectable.** A write-time filter is a
  hook script or a CLI the user can run by hand and diff. No model call, no
  perplexity scoring, no learned importance — those are what create both the
  cache churn and the attack surface. Truncation with an explicit, visible
  marker beats clever summarization; that is the measured result, not a
  preference.
- **`rtk` stays the reference implementation of the pattern, and stays
  opt-in.** It is already write-time and already deterministic. Its hook remains
  opt-in per [ROADMAP](../../ROADMAP.md) — this ADR does not change that.

The one-line rule, for anyone deciding where a future feature belongs:

> If it changes bytes that are already in the transcript, it is wrong. If it
> changes what goes into the transcript in the first place, it is right.

## Consequences

**Easier:**

- Savings survive. Anything cut at write time is cut from every future turn's
  prefix at zero further cost, and the cache keeps working.
- The whole mechanism is a hook script. Devgeta already ships five, has a
  deployment path (`internal/apps/claude`), a mirroring rule for OpenCode, and
  behavioral test harnesses in both Go and Node. No new architecture.
- Nothing new sees the prompt stream. The trust boundary devgeta already
  maintains does not grow.
- Auditable by construction: a user can run the filter on a file and see exactly
  what the agent would have received.

**Harder:**

- We give up the headline number. A write-time filter cannot touch conversation
  history, so it cannot claim 60–95%. It caps the cost of _new_ verbose output
  only. The remedy for accumulated history is a different mechanism
  ([ADR-0032](ADR-0032-session-continuity-is-a-durable-note-not-a-longer-session.md)),
  which is why these two ADRs land together.
- Filters must be conservative. Cutting something load-bearing makes the agent
  re-run the command, which costs more than it saved. Every filter therefore
  needs a visible truncation marker and a documented way to get the full output.
- Per-tool work. There is no universal filter; each verbose tool class (test
  runners, builds, log reads) gets its own rule, and each must be general enough
  to ship to strangers.

**Accepted trade-offs:**

- **A user who wants a compression proxy can still install one.** We are
  deciding what devgeta ships and recommends, not restricting anyone's machine.
  This ADR is the reason it will not appear in an install category.
- **We are betting on caching staying prefix-matched.** If providers ship
  content-addressed or fuzzy caching, constraint 1 weakens and this is worth
  revisiting. Constraints 2 and 3 stand on their own.

[headroom]: https://github.com/headroomlabs-ai/headroom
[localsplitter]: https://arxiv.org/pdf/2604.12301
[compressionattack]: https://arxiv.org/html/2510.22963v2
