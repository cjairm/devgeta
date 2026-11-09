# ADR-0068 — `dg agent-pack`: a pinned upstream plus a build layer, installed through each coder's own system

**Date:** 2026-11-08
**Status:** ACCEPTED

## Context

After the skills reset ([investigation](../investigations/2026-11-06-skills-and-agents-reset.md))
devgeta installs no agent content. We want
[Superpowers](https://github.com/obra/superpowers),
[Ponytail](https://github.com/DietrichGebert/ponytail) and devgeta's own
`DEVGETA.md` in Claude Code, OpenCode and Pi.

Neither upstream is a folder of skills. Superpowers' bootstrap is a hook in
Claude Code, a plugin in OpenCode and an extension in Pi. Ponytail's ruleset
lives in hooks and commands. Copying `skills/` loses both
([investigation, finding 2](../investigations/2026-11-08-agent-packs.md#2-what-the-two-upstreams-need-in-order-to-work)).
Upstream's own install commands can't remove one skill in Claude Code, the
official Claude marketplace updates on its own, and none of them can be
customized.

A sandbox probe ([Probe results](../investigations/2026-11-08-agent-packs.md#probe-results-2026-11-08))
showed that each coder loads a pack from a local folder, in place:

- Claude Code: a `directory` marketplace declared in `settings.json`.
- OpenCode: an absolute folder path in `opencode.json` `plugins`.
- Pi: `pi install <path>`.

In all three, the skills, commands and extensions came from the built tree,
with its exclusion and overrides applied. The probe could not check that a
session receives the bootstrap, because the sandbox has no model login. If
that check fails in any coder, this ADR is revisited before code depends on
it.

## Decision

1. **The command is `dg agent-pack`**, with `list`, `install`, `uninstall`,
   `update` and `check`. The name leaves room for packs aimed at other apps
   later. The engine in `internal/tooling/agentpack/` has no coder-specific
   code outside its three installers.
2. **Each upstream pack goes through three stages:** fetch at a pinned tag and
   commit, build into a tree devgeta owns, then install that tree through each
   coder's own system. The build layer is
   [ADR-0069](ADR-0069-pack-customization-only-through-a-build-time-override-layer.md).
3. **Pins live in `configs/agent-packs/<pack>/pack.yaml`:** the repo, the tag
   and the full 40-character **commit** SHA. Both upstreams use annotated
   tags, whose own hash is not the commit's. A pin sits next to the overrides
   it was checked against. Coder pins stay in `constants.PinnedVersion`.
4. **Upstream packs are fetched at install time, not embedded.** The clone is
   kept read-only and byte-exact under the devgeta data directory, and a
   cached clone at the same SHA is reused. The cost is a network dependency
   the first time.
5. **One built tree per pack, at `<data>/devgeta/agent-packs/<pack>/`, and all
   three coders point at it:**
   - **Claude Code:** `<data>/devgeta/agent-packs/.claude-plugin/marketplace.json`
     declares a `devgeta` marketplace that lists each built pack. devgeta runs
     `claude plugin marketplace add` and `claude plugin install <pack>@devgeta
-s user`. Those calls record the install, so `claude plugin list` and
     `/plugin` show it. They also write `extraKnownMarketplaces` and
     `enabledPlugins` into `settings.json`, the same keys that decision 6
     renders. A session loads from the declared folder, not from Claude's
     cache copy, so the plugin's `version` is left as upstream wrote it.
   - **OpenCode:** an absolute folder path in `opencode.json` `plugins`. For
     Superpowers that is the tree root; for Ponytail it is
     `.opencode/plugins/`. Ponytail's commands need no copying.
   - **Pi:** `pi install <tree>`, removed with `pi remove <tree>`.
6. **`agent_packs:` state is the source of truth for what a pack added to a
   coder's config file.** `dg configure claude` renders `extraKnownMarketplaces`
   and `enabledPlugins` from that state, and `dg configure opencode` renders
   `plugins` and `instructions` from it. So `dg configure --force` keeps the
   packs instead of disabling them. Where a coder CLI also writes those keys
   (Claude Code, decision 5), it writes the same values, and the next render
   replaces them with identical ones.
7. **The `devgeta` pack is embedded in the binary,** not fetched. It carries
   `DEVGETA.md` and goes to each coder in the way that coder can load it:
   - Claude Code: a plugin can't carry a `CLAUDE.md`, so the file is copied to
     `~/.claude/DEVGETA.md`, and `@DEVGETA.md` is added to
     `~/.claude/CLAUDE.md` when no line already imports it.
   - OpenCode: an `instructions` entry pointing at the built copy.
   - Pi: a small package whose extension appends the file to the system
     prompt.

   All three get the same bytes, and a test checks it.

8. **Where packs go:**
   - `dg agent-pack install` without `--coder` targets every coder devgeta has
     configured.
   - `dg install` installs `devgeta`, `superpowers` and `ponytail` only into
     the coders it installed in that run, never into one that was already
     there. It prints the `dg agent-pack install` command for each coder it
     skipped this way.
   - `dg install --only agent-packs` is an explicit request, so it installs
     the default packs into every configured coder, like
     `dg agent-pack install`.
   - `dg install --skip agent-packs` installs no packs.
9. **What a coder can't load is named, never faked.** The output of `install`
   and of `list` says what was skipped and why. A coder that isn't installed
   is skipped with a reason; that is not a failure.
10. **Another install of the same upstream is a conflict.** For example,
    Superpowers from `claude-plugins-official`, or `pi install git:…`. devgeta
    stops, prints the command that removes the other copy, and installs
    nothing. It never removes the other copy itself.
11. **Install is all-or-nothing across the selected coders.** Build once, then
    install into each coder. If one fails, undo the coders already done in
    this run. State is written once, at the end.
12. **`check <pack> [--ref <tag>]` installs nothing.** It builds into a temp
    directory and runs the build checks. It also runs `claude plugin validate`
    when Claude Code is present, and prints the Pi version when Pi is present.
    It is a general command, so a user can try a newer tag before devgeta
    ships it.

## Consequences

- Upstream's hooks, bootstraps and commands work from a tree devgeta controls,
  in every coder that can load them. The bootstrap reaching a session is still
  unchecked.
- A version bump is one folder's diff plus a devgeta release. No coder
  auto-updates a local-folder source, so devgeta's pin is the only one.
- Pi isn't pinned, so a Pi release can break the Pi side. `check` prints the
  Pi version it ran against.
- Installing needs `git` and the network the first time. Some hooks need
  `node` or `bash`. Anything missing is named.
- `dg configure --force` keeps re-rendering `settings.json` and
  `opencode.json` whole. Hand edits to those files are still lost, as they
  are today.
- `@DEVGETA.md` comes back on every install or update. Opting out means
  uninstalling the `devgeta` pack.
