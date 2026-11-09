# Agent packs

An **agent pack** is a set of skills, hooks, commands and rules that devgeta
installs into your AI coders: [Claude Code](claude.md), [OpenCode](opencode.md)
and [Pi](pi.md). `dg agent-pack` installs, updates and removes them.

| Pack          | What it is                                                                                                                  | Where it comes from                                                           |
| ------------- | --------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `superpowers` | Skills (test-driven development, debugging, planning, code review) and a session bootstrap that tells the agent to use them | [obra/superpowers](https://github.com/obra/superpowers), pinned               |
| `ponytail`    | A "lazy senior developer" ruleset: the simplest solution that works, plus review, audit and debt commands                   | [DietrichGebert/ponytail](https://github.com/DietrichGebert/ponytail), pinned |
| `devgeta`     | devgeta's own response and writing rules (`DEVGETA.md`)                                                                     | Ships inside the devgeta binary                                               |

The pinned versions are in `configs/agent-packs/<pack>/pack.yaml` and shown by
`dg agent-pack list`. Each is a tag **and** its full commit, so what you get is
exactly what devgeta was checked against.

## Commands

```bash
dg agent-pack list                                  # pins, installed versions, each coder's state
dg agent-pack install superpowers                   # into every coder that is installed
dg agent-pack install ponytail --coder claude,pi    # only into these coders
dg agent-pack install superpowers --exclude a,b     # leave these skills out (saved)
dg agent-pack uninstall superpowers                 # remove what devgeta installed
dg agent-pack update                                # move every installed pack to this devgeta's pin
dg agent-pack check superpowers --ref v7.0.3        # build and check a newer tag; installs nothing
```

| Command            | Flags                                              | What it does                                                                                                         |
| ------------------ | -------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `list`             |                                                    | Each pack's pinned and installed version, and per coder: installed, not installed, or skipped with the reason.       |
| `install <pack>`   | `--coder claude,opencode,pi`, `--exclude <skills>` | Fetch at the pin, build, and install into each coder. Without `--coder` that is every coder that is installed.       |
| `uninstall <pack>` | `--coder …`                                        | Remove the pack from the coders that have it, deleting only what devgeta installed.                                  |
| `update [pack]`    |                                                    | Move an installed pack (or all of them) to the pin in this devgeta, and rebuild, so changed overrides are picked up. |
| `check <pack>`     | `--ref <tag>`                                      | Build into a temporary folder and run every check. Installs nothing and changes no state.                            |

All of them take `--verbose`. `dg agent-pack --help` and
`dg agent-pack <command> --help` are the source of truth for the flags.

## What install does

1. **Fetch.** The pack's upstream is cloned at its pinned tag, and the commit is
   checked against the pinned commit. A moved tag or a failed download stops
   here and changes nothing. The clone is kept, read-only, and reused next time.
2. **Build.** devgeta copies the clone into a folder it owns, drops any skills
   you excluded, copies the override layers on top, and runs the build checks
   (below). Nothing edits the clone.
3. **Install,** once per coder, through that coder's own system:

| Coder       | How the pack is loaded                                                                                                                                                      | Where devgeta records it                                       |
| ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| Claude Code | A local plugin marketplace named `devgeta` whose folder is the built packs. `claude plugin install <pack>@devgeta -s user`. It shows in `claude plugin list` and `/plugin`. | `settings.json`: `extraKnownMarketplaces` and `enabledPlugins` |
| OpenCode    | An absolute folder path in `opencode.json` `plugins` (the folder with the plugin's `index.js`). The `devgeta` pack is an `instructions` entry instead.                      | `opencode.json`: `plugins` and `instructions`                  |
| Pi          | `pi install <built folder>`, removed with `pi remove`. Pi's own package manager records it.                                                                                 | Pi's `settings.json` `packages`                                |

If one coder fails, the coders already done in this run are undone and
`global_config.yaml` is left as it was. A coder that already had the pack is not
undone (all coders share one built folder, so reinstalling just refreshes it).

A coder that isn't installed is **skipped, with the reason**; that is not a
failure. Anything a coder can't load from a pack is named in the output too.

### Where things live

| Path                                                  | What                                                                                   |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `~/.local/share/devgeta/agent-packs/<pack>/`          | The built pack, which every coder points at                                            |
| `~/.local/share/devgeta/agent-packs/.src/…`           | The read-only upstream clones, one per commit. Kept so a reinstall needs no network    |
| `~/.config/devgeta/agent-packs/<pack>/`               | **Your** overrides (below)                                                             |
| `~/.config/devgeta/global_config.yaml` `agent_packs:` | What devgeta installed, per pack and coder. Uninstall removes only what is listed here |

## `dg install`

`dg install` installs `devgeta`, `superpowers` and `ponytail` **into the coders
that this run installed**. A coder you already had is never touched, even if
`dg install` upgrades it to the pinned version; devgeta prints the commands to
add the packs to it:

```bash
dg agent-pack install devgeta --coder claude
dg agent-pack install superpowers --coder claude
dg agent-pack install ponytail --coder claude
```

| Flag                            | Effect                                                                                                 |
| ------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `dg install --skip agent-packs` | Install no packs.                                                                                      |
| `dg install --only agent-packs` | An explicit request: the three packs go into **every configured coder**, like `dg agent-pack install`. |

A pack that fails to install is reported with the command to retry, and the rest
of `dg install` carries on.

## Excluding skills

```bash
dg agent-pack install superpowers --exclude dispatching-parallel-agents,using-git-worktrees
```

The skills are removed from the built pack, in all three coders. The list is
saved, so `update` keeps it. A new `--exclude` **replaces** the saved list,
`--exclude=` **clears** it, and leaving the flag out keeps it.

Excluding a skill that a kept skill names is refused, with both named, instead of
failing in a session. For example, `using-git-worktrees` is named by
`executing-plans`, `writing-plans` and `subagent-driven-development`, so it can
only be excluded together with them (or after overriding them).

## Overriding a skill

Put files laid out like the pack under `~/.config/devgeta/agent-packs/<pack>/`:

```
~/.config/devgeta/agent-packs/superpowers/skills/using-git-worktrees/SKILL.md
```

Then run `dg agent-pack update superpowers`. devgeta copies its own overrides
(none ship today) and then yours on top, so **yours win**. An override replaces
the file at the same path, or adds a file inside an existing skill's folder. It
can't add a new skill.

An override `SKILL.md` is one of two kinds:

- **A wrapper** contains the text `UPSTREAM.md`. The build keeps upstream's
  original `SKILL.md` beside it, renamed `UPSTREAM.md`, so your file can say
  "read `UPSTREAM.md`, then also do this". A wrapper follows upstream when it
  changes, and it must keep upstream's `name` and `description`.
- **A full replacement** is everything else. It must keep upstream's `name` (so
  other skills still reach it), may change the `description`, and does not follow
  upstream: `dg agent-pack check` lists replaced files whose upstream copy changed,
  so you re-read the change.

`UPSTREAM.md` is always upstream's file, never devgeta's or an earlier layer's.
Every coder treats only `SKILL.md` as a skill, so it never loads as a second one.

### Build checks

A build stops, and deletes its temporary folder, if any of these fail. The folder
that was installed before is never touched.

- every skill you exclude or override exists upstream;
- no kept skill names an excluded one, as `<pack>:<name>` or by its bare name, in
  `SKILL.md` or any other `.md` in its folder;
- an override keeps upstream's skill `name` (and a wrapper its `description`);
- the files the coders need are there: `.claude-plugin/plugin.json`, every hook
  file it declares (`hooks/hooks.json` when it declares none), and a
  `package.json` with `pi` and `main`.

## `DEVGETA.md` and opting out

The `devgeta` pack carries `DEVGETA.md`. Every coder gets the same bytes:

- **Claude Code:** a copy at `~/.claude/DEVGETA.md`, and an `@DEVGETA.md` line in
  `~/.claude/CLAUDE.md`, added **only if no line there already imports it**.
- **OpenCode:** an `instructions` entry pointing at the built copy.
- **Pi:** a package whose extension appends the file to the system prompt.

To change the rules, put your own `DEVGETA.md` in
`~/.config/devgeta/agent-packs/devgeta/` and run `dg agent-pack update devgeta`.

**To opt out, uninstall the pack:** `dg agent-pack uninstall devgeta`. Deleting the
`@DEVGETA.md` line by hand does not stick: the line comes back on the next
install or update. Uninstalling removes `~/.claude/DEVGETA.md`, and removes the
line **only if devgeta added it and it is still exactly `@DEVGETA.md`**. A line you
wrote first, or edited since, stays, and the output says so.

## Ponytail

- **The task ledger (Claude Code).** Ponytail writes one row per task (cost,
  time, tokens) to `~/.config/ponytail/band/<project>.csv` by default. Nothing
  leaves your machine unless you set a webhook. To turn the ledger off:

  ```bash
  echo '{"ledger":"false"}' | claude plugin configure ponytail@devgeta --values-stdin
  ```

  then restart Claude Code.

- **Its default mode (OpenCode).** `PONYTAIL_DEFAULT_MODE` (`lite`, `full`,
  `ultra`, `off`) is read from the environment of OpenCode's **shared background
  service**, not from the terminal that starts a session, the same way
  [the agent-config guard's bypass is](opencode.md#agent-config-guard-devgeta-agent-config-guardserverjs):

  ```bash
  opencode service set env PONYTAIL_DEFAULT_MODE full
  opencode service restart
  ```

  Setting `defaultMode` in `~/.config/ponytail/config.json` works in every coder.

## A pack installed another way

If the same upstream is already there, for example Superpowers from
`claude-plugins-official`, or `pi install git:github.com/obra/superpowers`,
`dg agent-pack install` **stops and prints the command that removes that copy**.
It installs nothing and never removes a copy it didn't install.

| Coder       | What the message tells you to run                                                        |
| ----------- | ---------------------------------------------------------------------------------------- |
| Claude Code | `claude plugin uninstall superpowers@claude-plugins-official -s user`                    |
| OpenCode    | Remove the spec from `plugins` in the config file it names (a project's `opencode.json`) |
| Pi          | `pi remove <the source it names>`                                                        |

Two copies of the same pack would load its skills and hooks twice.

## What the packs need on your PATH

Missing ones are named after an install, as warnings:

| Pack          | Needs  | For                                        |
| ------------- | ------ | ------------------------------------------ |
| `superpowers` | `bash` | its session-start hook in Claude Code      |
| `superpowers` | `node` | the brainstorming skill's visual companion |
| `ponytail`    | `node` | the hooks it runs in Claude Code           |

## Updating and checking a newer version

`dg agent-pack update` moves to the pin in **your** devgeta. Upstream releases
don't reach you until devgeta pins them, because no coder auto-updates a
local-folder pack.

To try a newer upstream tag first:

```bash
dg agent-pack check superpowers --ref v7.0.3
```

It builds that tag with your exclusions and overrides, runs every build check,
runs `claude plugin validate` on the result when Claude Code is installed, prints
the Pi version it ran against when Pi is installed, and lists any file you replace
in full whose upstream copy changed. It installs nothing.

`check` can't tell whether a session receives a pack's bootstrap. After a version
bump, start a session in each coder and look.

## Removing

`dg agent-pack uninstall <pack>` removes the pack from the coders that have it and
leaves everything else in their config alone. When no coder has the pack any more,
its built folder and its `agent_packs:` entry are deleted; the downloaded clone
stays so a reinstall needs no network. When no pack is left in Claude Code,
devgeta's `devgeta` marketplace is removed too.

`dg uninstall claude` and `dg uninstall opencode` delete those coders' config
folders, so they also drop the coder from every pack's record. `dg uninstall pi`
keeps Pi's packages, so the record stays and `dg agent-pack list` shows Pi as not
installed. If Pi's settings no longer list a package devgeta recorded, `list` says
so and prints the command that puts it back.

## `--force` keeps your packs

`dg configure claude --force` and `dg configure opencode --force` re-render
`settings.json` and `opencode.json` whole. They render the pack entries from
`agent_packs:`, so the packs stay enabled. Pi's `settings.json` is not rendered by
devgeta. Hand edits to `settings.json` and `opencode.json` are lost on `--force`,
as they always were; `dg agent-pack install` and `uninstall` also re-render
`opencode.json`, so the same goes for those changes.

## Limits

- **Pi is not pinned.** Its installer always takes the latest release, so a Pi
  release can break the Pi side. `dg agent-pack check` prints the version it ran
  against next to the one devgeta last checked.
- **Fetching needs `git` and the network** the first time a commit is used.
- **Packs are for coders only.** `dg agent-pack` can later install packs for other
  apps, but none ship.
