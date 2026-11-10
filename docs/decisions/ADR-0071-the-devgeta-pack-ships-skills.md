# ADR-0071 — The `devgeta` pack ships skills; review is a new skill plus a Superpowers wrapper

**Date:** 2026-11-09
**Status:** ACCEPTED

## Context

devgeta needs a way to review design docs, plans and code (alone or together),
post that review to a PR, and work through PR feedback
([cycle](../plans/cycles/2026-11-09-review-skills.md)). Superpowers'
`receiving-code-review` already has the judgment for feedback but none of the
PR mechanics; nothing upstream reviews a doc alone or posts to a PR.
[ADR-0069](ADR-0069-pack-customization-only-through-a-build-time-override-layer.md)
allows wrapping an upstream skill but not adding one, and says devgeta's own
skills ship in the `devgeta` pack — which today carries instructions only
(`DEVGETA.md` and its Pi loader), no skills.

Options weighed: wrap both Superpowers review skills (a wrapper can't change
its description, so "review this plan" may not trigger it, and
`requesting-code-review` runs after every task inside
`subagent-driven-development`); write two new skills (repeats
`receiving-code-review`); or one new skill plus one wrapper.

## Decision

1. The `devgeta` pack carries skills under `skills/<name>/SKILL.md`, delivered
   to every coder through the same mechanism each uses for other packs:
   a plugin in the `devgeta` marketplace (Claude Code), a skills path or
   minimal plugin (OpenCode), the existing Pi package (Pi).
2. Reviewing is a new `review` skill in the `devgeta` pack.
3. Addressing feedback is a wrapper over Superpowers' `receiving-code-review`
   that adds only the PR steps and defers judgment to `UPSTREAM.md`.
4. Amends [ADR-0069](ADR-0069-pack-customization-only-through-a-build-time-override-layer.md)
   §6: when a skill is excluded, devgeta's shipped override for it is dropped
   silently instead of failing the build; the error stays for the user's own
   overrides. Otherwise shipping the wrapper would break every install that
   already excludes `receiving-code-review`.
5. Neither builds state or helpers of its own: no journal, no `dg task`
   commands. They call `gh` and `git`.

## Consequences

- Later devgeta skills cost one folder, not engine work.
- The `devgeta` pack becomes a Claude Code plugin, so `claude plugin list`
  shows it; uninstall and `--exclude` work on its skills like any pack.
- The feedback wrapper follows Superpowers' pin; a pin bump re-checks it.
- Cross-model review is a usage pattern (run `review` in another coder or
  model), not a feature; nothing launches models.
