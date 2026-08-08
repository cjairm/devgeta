# ADR-0020: A pane's command is exec'd at pane creation, not typed into the pane

**Status:** ACCEPTED (amended 2026-08-07 — see [Amendment](#amendment--2026-08-07-the-typed-form-is-the-un-aliased-command-too))
**Date:** 2026-08-07
**Deciders:** cjairm
**Related:** [ADR-0011](ADR-0011-agent-prompt-as-launch-argument.md), [ADR-0016](ADR-0016-inconclusive-tool-probe-fails-open.md)

---

## Context

`dg wt create --prompt '<text>'` silently loses the prompt when the text is long.
The window is created, the pane looks fine, and the AI coder sits at an empty
session — but the command line it was supposed to run was cut off partway and the
Enter never arrived, so nothing ran at all. `dg wt create` reports success.

### How a pane gets its command today

`buildWindowPanes` (`internal/tooling/worktree/worktree.go`) creates the window,
then splits a pane per layout entry, then **types** each pane's command into that
pane with `tmux send-keys` (`SendKeysToWindow` / `SendKeysToWindowInSession` /
`SendKeysToPane`, `internal/apps/tmux/tmux.go`). The pane is running an
interactive shell; devgeta writes a command line into it and presses Enter.

`--prompt` rides inside that typed command line. ADR-0011 established that the
prompt is a **launch argument** (`cc '<text>'`, `oc --prompt '<text>'`) rather
than keystrokes sent to a booted TUI — which removed the readiness race, but left
the whole command, prompt included, going into the pane as typed input.

### What actually breaks

`tmux send-keys` writes into the pane's pty. On macOS/BSD the tty input queue is
capped at 1024 bytes; when a write overflows it, the terminal driver **discards
the excess** — including the trailing Enter. tmux is not told, and exits 0.

Measured on this machine (macOS 25.6, tmux on a throwaway server, command sent to
a freshly created window):

| Command length (bytes) | Result                                    |
| ---------------------- | ----------------------------------------- |
| 1020                   | ran                                       |
| 1023                   | ran                                       |
| 1024                   | **lost** — 1024 bytes on screen, no Enter |
| 1025                   | **lost**                                  |
| 1030                   | **lost**                                  |

The boundary is exact and it is the whole write, Enter included: 1023 bytes of
command plus one Enter fills the queue precisely; one more byte and everything
past 1024 is dropped. A 1233-byte send left exactly 1024 bytes sitting on the
pane's command line, unexecuted, with `tmux send-keys` reporting success.

Two further measurements shape the decision:

- **The cap is a property of the pty, not of `send-keys`.** Routing the same text
  through `load-buffer` + `paste-buffer` loses it identically (1528 bytes → command
  never ran). Nothing that delivers a command as terminal _input_ escapes this.
- **The same text passed as a process argument is unaffected.** A 4029-byte
  command handed to `tmux new-window` as a shell-command ran intact. `ARG_MAX` on
  macOS is 1,048,576 bytes — a thousand times the headroom, and the payload never
  touches a terminal.

The cap is also timing-dependent, which is why this went unnoticed: a shell that
is already idle and draining its input can absorb more than 1024 bytes. A shell
that is still starting — exactly the case in `buildWindowPanes`, which sends the
command immediately after creating the window — cannot. So the bug is not merely
"long prompts fail", it is "long prompts fail depending on machine load", which
is worse.

### Which paths are exposed

Every `send-keys` path carries the cap, but they are not equally at risk:

| Path                                               | Command sent                                                                 | Risk today                       |
| -------------------------------------------------- | ---------------------------------------------------------------------------- | -------------------------------- |
| `buildWindowPanes` — pane launch                   | `cc '<prompt>'` / `--pane` value                                             | **Real.** User text, unbounded.  |
| `ensureWindow` repair (worktree.go:2112)           | `cc` / `oc`, bare                                                            | None today — short, no prompt.   |
| `launchReviewInLiveWindow` (worktree.go:2052/2074) | `ReviewCommand`, fixed prompt (renamed since — see the 2026-08-07 amendment) | None today — fixed short string. |

This table describes the code as it stood when this ADR was written. The two
send-keys paths no longer send `cc`/`oc`: see the 2026-08-07 amendment below.

The bounded ones are bounded by accident, not by construction. `dg task review-run`
is unaffected on a different ground: it execs the reviewer as a child process
(`ExecCommand` with `OnStdoutLine`), so its `--note` text never goes near a pty.

### Why commands are typed in the first place

Not arbitrarily. `Command()` returns a **devgeta shell alias** — `cc` and `oc`,
defined in `configs/templates/devgeta.zsh.tmpl` (`alias oc=opencode`,
`alias cc="CLAUDE_CODE_NO_FLICKER=1 claude"`). Aliases only exist inside an
interactive shell, so the command had to be typed at one. That is a genuine
constraint any fix has to answer, and it is measured below.

## Decision

**A pane's command is executed as the pane's process at creation time. devgeta
does not type commands into panes it is creating.**

Four parts:

### 1. Commands are passed to tmux as shell-commands, not keystrokes

The command travels as process arguments — no pty, no 1024-byte cap, ~1 MiB of
headroom. This makes the failure impossible rather than unlikely, which is what
CLAUDE.md §4 asks for when a class of mistake keeps being available.

**All three** of the tmux commands that create a pane accept a shell-command, and
all three are in scope. Each is verified to carry a long command and to leave its
pane alive when combined with part 2's trailing `exec`:

| tmux command   | devgeta wrapper                         | Creates                               | Verified           |
| -------------- | --------------------------------------- | ------------------------------------- | ------------------ |
| `new-window`   | `CreateWindow`, `CreateWindowInSession` | a window and its first pane           | 4029 bytes, intact |
| `split-window` | `SplitWindow`                           | an additional pane                    | 2500 bytes, intact |
| `new-session`  | `CreateSessionWithWindow`               | a session, its window, its first pane | 2500 bytes, intact |

`new-session` is easy to overlook because it reads as session setup rather than
pane setup, but it creates the **first pane of the first window in a new repo
session** — the common path the first time a worktree is created for a repo. If
it alone kept typing its command afterward, the bug would survive in exactly the
case a user hits most often.

The rule is therefore stated on the property, not on a list of function names:
**any tmux call that brings a pane into existence carries that pane's command.**
All three wrappers gain the command parameter; none of them is allowed to create
a pane and have the caller type into it afterward.

### 2. The pane keeps a shell when the command exits, and keeps its directory

Exec'ing the command directly changes pane lifetime: today a coder that quits
drops you at a shell prompt in that pane; a bare exec'd command would close the
pane, and the last pane closing takes the window with it. Both verified.

So the pane command ends in `; exec <shell>` — verified to leave the window alive
after the command exits. Today's behavior is preserved exactly; nobody loses a
window because they quit their coder.

**The trailing `exec` must run in the same shell that ran the pane's command, not
after a nested one.** This is not a stylistic choice — it decides whether a
directory change in the command survives. Measured, with `cd api` as the command
and the pane's working directory read from the process itself:

| Construction                                        | Pane ends up in |
| --------------------------------------------------- | --------------- |
| Today (`send-keys` into an interactive shell)       | `wtroot/api`    |
| `<shell> -ic '<command>; exec <shell> -i'` (same)   | `wtroot/api` ✅ |
| `<shell> -ic '<command>'; exec <shell> -i` (nested) | `wtroot` ❌     |

The nested form silently drops the `cd`, because it happened in a child process
that then exited. Since `--pane 'cd api && make dev'` is the documented example
of what that flag is for (ADR-0011), the nested form would break the flag's
headline use case while still appearing to work — the pane opens, the command
runs, only the final directory is wrong. The command and its trailing `exec`
therefore go into one shell invocation.

(Note for implementation: `#{pane_current_path}` is not a usable check here — it
did not track the directory change in any of the three cases above, including
today's. Read the pane process's own working directory instead.)

### 3. What replaces the alias, per pane kind

tmux runs a shell-command through a **non-interactive** shell (measured: flags
`569X`, no `i`), which does not read `.zshrc`. `alias cc` there exits 1 — **the
aliases are gone.** Something has to replace them.

#### The launch target is an absolute path, resolved at preflight

An earlier draft of this ADR said the non-interactive shell still gets the right
`PATH` because `~/.zshenv` is read by every zsh invocation, and it verified that
on this machine. **That reasoning does not generalize and is not the rule.**
`.zshenv` is a zsh feature with no equivalent in bash — a non-interactive,
non-login bash reads only `$BASH_ENV`, which is unset by default. Worse, the two
shells involved need not be the same one: the preflight probe runs `$SHELL -i -c`
(`internal/commands/shell_lookup.go`), while tmux runs its own `default-shell`
option non-interactively. So the check and the launch could disagree on both the
shell **and** its `PATH`, and a bash or custom-shell user could pass preflight and
then get `command not found` inside a pane devgeta just told them was fine.

Verified that this is a real gap, not a theoretical one: with `PATH` reduced to
the system default, `command -v claude` finds nothing.

So devgeta does not rely on the pane's `PATH` at all:

- The preflight probe already runs `command -v -- <name>` in the user's
  interactive shell. `command -v` **prints the resolved absolute path** (verified:
  `/Users/…/.local/bin/claude`), and today that path is thrown away in favor of a
  found/not-found verdict.
- Keep it. The pane execs that absolute path. Whatever `PATH` the pane's shell
  ends up with is then irrelevant, for every shell, on both platforms.

This also tightens the property `ensureToolInstalled`'s doc comment already
insists on — that the check must probe exactly what the pane will run — from
"the same token" to "the same resolved file."

#### The probe must be binary-only and path-shaped

`command -v` does **not** always print a path, and the token devgeta probes today
is exactly a case where it does not. Measured:

| Probed token             | `command -v` prints                          |
| ------------------------ | -------------------------------------------- |
| `claude` (the binary)    | `/Users/…/.local/bin/claude` — a path        |
| `cc` (the devgeta alias) | `alias cc='CLAUDE_CODE_NO_FLICKER=1 claude'` |
| a shell function         | the bare function name                       |
| a shell builtin          | the bare builtin name                        |

So "run the current probe and exec its stdout" would hand a pane the literal text
`alias cc='…'` to execute. Two rules prevent that, and both are part of this
decision:

1. **Probe the binary, not the launch alias.** The probed token becomes `claude` /
   `opencode`, because the binary is now what the pane execs. This is the same
   "probe exactly what will run" rule as before, following the launch token as it
   changes — not a relaxation of it.
2. **Accept only a path-shaped answer.** The probe's result is used as an
   executable path only if it begins with `/`. Alias text, a function name, a
   builtin name, and empty output are all "no path", never something to exec.
3. **Shell-quote the path when building the command.** "Begins with `/`" is a
   shape check, not a safety check: `/Users/Jane Doe/.local/bin/claude` passes it
   and would then be split into two words, and a path containing shell
   metacharacters would be interpreted rather than run. The resolved path is
   embedded with the existing `shellSingleQuote` helper, which already handles
   embedded single quotes via the close/escape/reopen trick.

   The general rule this instance follows: **every fragment devgeta assembles
   into a pane's shell-command string is shell-quoted.** Nothing about moving
   from `send-keys` to a tmux shell-command removes the need for it: tmux still
   hands a single-argument shell-command to a shell, so there is still no Go-side
   shell parser in between.

   Stating that rule and then applying it ad hoc has already failed three times
   during this ADR's review — the resolved path, the interactive wrapper, and the
   shell executable were each missed separately. So the rule is discharged here
   **as a closed list**: these are every value that reaches a pane's command
   string, and there are no others.

   | Interpolated value                           | Treatment                                                 |
   | -------------------------------------------- | --------------------------------------------------------- |
   | Resolved binary path                         | quoted as one literal word                                |
   | Opening prompt                               | quoted as one literal word (ADR-0011 already does this)   |
   | Shell executable, outer invocation           | **validated** (absolute path) then quoted                 |
   | Shell executable, trailing `exec`            | **validated then quoted — same value, second site**       |
   | Assembled inner script (fallback / `--pane`) | quoted as a whole when nested in another shell's `-c`     |
   | Env-var prefix (`CLAUDE_CODE_NO_FLICKER=1`)  | devgeta-owned constant, no interpolation                  |
   | `--pane` value, _within_ its script          | deliberately unquoted — it is a command line (ADR-0011)   |
   | Worktree directory                           | not interpolated — tmux takes it as its own `-c` argument |

   The shell executable earns its two rows because it is interpolated **twice**
   in one recipe and both sites are equally capable of breaking. Its value comes
   from `$SHELL` or tmux's `default-shell` — neither of which devgeta controls.

   **Absolute-path validation is not enough for it.** `/bin/zsh` that was
   uninstalled, or a `$SHELL` pointing at a directory or a non-executable file,
   is still an absolute path and would sail through a shape check into both
   interpolation sites. So the shell is chosen by a concrete, ordered resolution
   rather than a single value plus a hope:

   1. `$SHELL`
   2. tmux's `default-shell`
   3. `/bin/sh`

   A candidate is usable only if it is an absolute path **and** stats as an
   existing, executable regular file. The first usable candidate wins, and
   `/bin/sh` is the floor because POSIX requires it to exist — so the resolution
   cannot come back empty and no create is ever blocked on this.

   One honest consequence: falling all the way to `/bin/sh` means the
   interactive-shell fallback cannot reproduce today's alias behavior, because
   `/bin/sh` is not the user's shell and never had the `cc`/`oc` aliases. That
   combination — no resolved binary path **and** no usable user shell — is a
   badly broken environment, and the pane will say so itself when the command
   fails to resolve. This is deliberately not turned into a pre-emptive block:
   ADR-0011 already prefers a launch that fails visibly in the pane over devgeta
   refusing to build the window, and ADR-0016 already refuses to let an unproven
   environment guess block a create.

   The only deliberate exception in the table is the `--pane` value, and it is
   narrow: unquoted **inside** its own script, where being a command line is the
   point, while the script containing it is still quoted as a whole.

`ShellCommandLookupFn` therefore needs a new capability, not just a new caller:
it currently discards stdout apart from its status marker, so returning a path is
an API change (a resolved-path variant alongside the existing three-valued
result). The marker stays the proof that the lookup ran, exactly as ADR-0016
requires; the path is read from the output _before_ the marker.

#### The no-path fallback keeps today's interactive-shell semantics

**Every** non-path outcome — inconclusive probe, alias/function/builtin text,
empty output — yields no path. What the pane does then has to be chosen with
care, because the obvious answer is wrong:

> Falling back to the bare command name in tmux's non-interactive shell would
> **not** be "today's behavior." Today the command is typed into an _interactive_
> shell, where the `cc`/`oc` alias exists and `.zshrc` has repaired `PATH`.
> A bare name in a non-interactive shell has neither. That fallback would turn an
> inconclusive probe — the case ADR-0016 exists to make harmless — into a likely
> `command not found`, which is precisely the fail-open guarantee inverted.

So the fallback launches the **alias-based recipe through an interactive shell**,
still exec'd at pane creation:

```
<shell> -ic '<alias recipe>; exec <shell> -i'
```

This is the same construction the `--pane` kind already uses, so it is one
mechanism serving two purposes rather than a special case. Verified end to end in
tmux: a 3000-byte argument delivered this way runs intact, and the window is
still alive after the command exits.

The two properties that matter are both preserved, and they are independent:

- **The truncation fix holds in every case.** Resolved-path or fallback, the
  command is process arguments, never terminal input. The 1024-byte cap is gone
  regardless of what the probe managed to answer.
- **ADR-0016 fail-open holds in every case.** A probe that could not answer costs
  the pane nothing but the interactive shell it already had today. Nothing blocks
  a create, and a non-path answer is never executed as a path.

The absolute path is therefore an **optimization** — it buys determinism and a
faster startup when available — not a load-bearing requirement. Reading it as a
requirement is what produced the broken fallback above.

One behavior change falls out of rule 1 and is accepted deliberately: today the
check probes the `cc`/`oc` alias, so a coder installed outside devgeta (whose
alias was never written into `devgeta.zsh`) correctly fails preflight. Probing
the binary means that case now passes — and launches fine, because the pane execs
the binary rather than the alias. The check gets _more_ accurate, but anyone
relying on it to detect "devgeta never configured this tool" loses that signal.

> **Amended 2026-08-07.** "Launches fine" held only for panes devgeta creates;
> the send-keys paths still typed the alias, so that install passed preflight and
> then failed in the pane. The typed form is the un-aliased command now, which
> retires that half of this consequence. The lost signal remains.

#### The probe's result must reach the launch, so the check is not error-only

"The check probes exactly what the pane will exec" is only a real guarantee if
the two share one answer. Today's API cannot express that: `Pane.check` is
`func() error`, `Layout.EnsureInstalled()` returns `error`, and it is called at a
different point in the flow (`worktree.go`) from where the pane's command is
built. A resolved path produced inside the check has nowhere to go.

Left there, an implementation has only two options, and both break the guarantee
this decision rests on:

- **Re-probe at launch.** Two probes, up to two 5-second timeouts per pane, and —
  because they are separate observations of a changing system — two answers that
  can disagree. The check would then have verified something other than what ran,
  which is precisely the property the ADR claims to strengthen.
- **Drop the path** and launch the bare name, silently reducing every pane to the
  fallback and making the resolution work pointless.

So the check-to-launch boundary changes with the rest of the decision: **the
probe runs once per pane, and its result is carried on the resolved layout rather
than re-derived at build time.** The pane's final command is then a function of
that carried result. `Pane.check`'s error-only signature and
`Layout.EnsureInstalled`'s error-only return both give way to something that
returns a resolution (a path, or an explicit "no path" that selects the fallback
above).

This fits where the layout model already went: `check` and `prompt` are
constructor-set fields on `Pane` precisely so a pane's behaviors cannot fall out
of step with the pane they describe (`layout.go`). The resolved launch recipe is
the same kind of state and belongs in the same place — one probe, one recipe,
fixed at resolution time.

This ADR does not fix the exact Go signatures; it fixes the requirement they have
to satisfy: **one probe per pane per create, and the command that runs is built
from that probe's answer.** An implementation that re-probes, or that resolves a
path and then launches something else, has not implemented this decision.

#### The two pane kinds

- **devgeta-owned commands** (the AI coder panes, the nvim pane) exec the
  resolved absolute path plus the recipe's env prefix — `CLAUDE_CODE_NO_FLICKER=1
/Users/…/claude '<prompt>'`, not `cc '<prompt>'`. The env-var part of the recipe
  moves into one Go constant that **also renders the alias line in
  `devgeta.zsh`**, so the single-source-of-truth property the alias indirection
  was protecting is kept — it just stops being enforced by "type it at a shell
  that happens to define it."
- **User-authored `--pane '<command>'` values** run through an **interactive**
  shell (verified: `zsh -ic 'alias cc'` resolves the alias), because that value is
  a command line the user wrote for their own shell and may use their own aliases
  and functions. Running it non-interactively would silently change what their
  command means. Absolute-path resolution does not apply here — devgeta is not
  parsing the user's command line, and does not need to.

  **The wrapper still has to be quoted, and this is not the same as quoting the
  value.** ADR-0011 keeps a `--pane` value unquoted _as a command line_, which is
  about how the inner shell reads it. But the assembled script is embedded inside
  `<shell> -ic '…'`, so a single quote anywhere in the user's command closes that
  wrapper early. Quote the **whole inner script** with the close/escape/reopen
  trick when embedding it; that leaves every character intact for the inner
  shell, so the value keeps its command-line meaning. Measured with
  `--pane` value `printf %s "it's fine"`:

  | Embedding                           | Result                      |
  | ----------------------------------- | --------------------------- |
  | Naive `<shell> -ic '<script>'`      | breaks — wrapper ends early |
  | Inner script escaped, then embedded | runs, prints `it's fine`    |

  So the ADR's two quoting rules operate at different levels and both apply:
  devgeta-assembled **fragments** are quoted as literal words (the path, the
  prompt), and the assembled **script** is quoted as a whole when it is nested
  inside another shell's `-c`.

This asymmetry is the same one ADR-0011 already drew, for the same reason: a
prompt is inert devgeta-owned data, a `--pane` value is a user's own command
line. The two are not the same kind of thing and should not be forced into one
rule.

#### Every devgeta-owned launch, including the review window

"devgeta-owned commands" is not only the built-in layouts. `LaunchReviewInRepo`
has a **create** branch — the no-live-window case — that builds its own one-pane
layout from `ReviewCommand` (renamed since — see the 2026-08-07 amendment),
i.e. `oc --agent <reviewer> --prompt '<text>'`, and
sends it through `createWindowWithLayout` like any other pane. It carries the
`oc` alias, so once created panes stop being interactive it breaks exactly the
same way a coder pane would, and it is a create path, so exec applies to it.

It converts with the rest: same resolved-executable recipe, same verification.
Two details make it easy to miss, which is why it is called out here rather than
left to "devgeta-owned commands" in general:

- It builds its pane as a **bare `Pane{Command: reviewCmd}` struct literal**,
  not through `coderPane`, so it inherits nothing a constructor might later add.
  Routing it through the coder-pane constructor is the change that makes this
  structural instead of remembered — `layout.go` already argues for
  constructor-built panes on the same grounds.
- Its preflight is `(&OpenCodeCoder{}).EnsureInstalled()`, which under the rules
  above now probes the `opencode` binary rather than the `oc` alias — the
  behavior change noted at the end of the probe section applies here too.

`launchReviewInLiveWindow`, the same function's live-window branch, splits into
**two** sub-branches, and only one of them is out of scope here:

- **Reuse an idle shell pane** — that pane already exists, so exec cannot apply.
  Covered by part 4.
- **Split a new pane** (`SplitWindow`, then `ActivePaneID`, then
  `SendKeysToPane(newPaneID, reviewCmd)`) — this **creates** a pane and then
  types a devgeta-owned command into it. It is a create path, so this part
  applies to it in full: the review command is passed at pane creation, exactly
  like every other created pane.

Verified that tmux supports this: `split-window` takes a shell-command, and it
carries a 2500-byte command intact while the new pane survives the command
exiting. So `SplitWindow` gains an optional shell-command, mirroring the change
to the window-creating calls — the wrapper extension CLAUDE.md's
route-through-the-wrapper rule asks for, rather than a raw `exec.Command` at the
call site.

Splitting-then-typing is the easiest case to misfile, because the function is
named for the _window_ already being live. What decides the rule is whether the
**pane** is new, not the window.

### 4. Any remaining send-keys refuses to truncate

Some paths send into a pane that **already exists** and is running a live shell.
No pane is created there, so exec cannot apply and they keep `send-keys`.

The test for membership is **"is this pane new?"**, not "is this window already
live?" — using window liveness put the review split-branch on the wrong side of
this line in an earlier draft.

Because "which paths?" has now been answered wrongly twice from memory, it is
answered here **mechanically instead**: this is every non-test call site of the
`SendKeys*` wrappers, and which part governs each.

| Call site (`internal/tooling/worktree/worktree.go`)     | Pane        | Governed by        |
| ------------------------------------------------------- | ----------- | ------------------ |
| `buildWindowFromLayout`'s `sendKeys` (:525)             | **created** | part 3 → exec      |
| `retargetWindowAfterMove` (:1741)                       | existing    | part 4 → send-keys |
| `launchReviewInLiveWindow`, idle-shell reuse (:2052)    | existing    | part 4 → send-keys |
| `launchReviewInLiveWindow`, after `SplitWindow` (:2074) | **created** | part 3 → exec      |
| `ensureWindow` repair (:2112)                           | existing    | part 4 → send-keys |
| `createWindowWithLayout`'s `sendKeys` (:2159)           | **created** | part 3 → exec      |

`retargetWindowAfterMove` is the one an earlier draft missed: `dg wt move` sends
`cd <newPath>` into each idle pane of the moved worktree's window. It is also the
send-keys path with the **most realistic** shot at the 1024-byte limit, because
its payload is derived from a filesystem path rather than a fixed devgeta string
— a deep enough worktree location reaches it on its own. (It already quotes that
path via `cdCommand` → `shellSingleQuote`.)

Verification should re-derive this table from the code rather than trust it:
if a future call site appears and the table is not updated, the table is what is
wrong.

They must not be left able to truncate silently. The tmux wrapper rejects any
`send-keys` payload over 1023 bytes with an error naming the limit, instead of
handing it to the pty to be cut in half. All three paths send short strings
today, so this changes no working behavior — it converts a future silent
data loss into a loud failure.

**No chunked sending.** Splitting a payload to sneak under the cap keeps the
data flowing through the terminal, depends on the reader draining between chunks,
and reintroduces exactly the timing-dependent silent loss this ADR removes.

**What these paths type is the un-aliased command, not `cc`/`oc`** — see the
amendment below. The delivery mechanism is unchanged: still `send-keys`, still
into a live interactive shell, with no `exec` and no resolved path.

## Amendment — 2026-08-07: the typed form is the un-aliased command too

This amends an **accepted** decision rather than restating what it always said.
As written, part 3 flipped the preflight probe from the `cc`/`oc` alias to the
binary and left part 4's send-keys paths typing the alias.

**What changed.** `AICoder.Command()` — the form devgeta types into a pane that
already exists — is now the coder's **un-aliased command**: `opencode`, and
`CLAUDE_CODE_NO_FLICKER=1 claude` (the env prefix spelled out, since the alias
definition used to supply it). Everything derived from it follows:
`PromptCommand`, the interactive-form launch builder, and the reviewer's typed
command. Both send-keys paths — `ensureWindow`'s repair branch and
`launchReviewInLiveWindow`'s idle-shell reuse — therefore send the binary.

**Why.** "The check verifies what the launch runs" was the property part 3 claimed
to strengthen, and after the probe flip it held only on the exec paths. A user with
the coder on `PATH` but no devgeta alias — installed outside devgeta, the shell
feature flag off, or a shell that predates `dg configure` — **passed** preflight
and then got `cc: command not found` in the pane, where before the flip they were
refused with an actionable message. The typed form now resolves with or without
devgeta.zsh having been sourced, so the invariant holds on every path.

**What this retires.** The consequence recorded at the end of part 3's probe
section — that probing the binary lets a coder installed outside devgeta pass
preflight, "and launches fine, because the pane execs the binary rather than the
alias" — was only true for created panes. That gap is now **gone**, not merely
documented: the same install launches correctly on the typed paths too. What
remains of that consequence is only the lost signal: a failure here no longer
means "devgeta never configured this tool."

**What does not change.**

- **`devgeta.zsh` still ships the alias**, still rendered from the same
  `pkg/constants` recipe. Users type `cc`/`oc` themselves; devgeta no longer does.
- **The create-path interactive fallback keeps `-ic`.** Its justification is no
  longer alias expansion but the one part 3's fallback section already gave: a
  bare name in tmux's non-interactive shell has no `.zshrc` PATH repair (and bash
  has no unconditional `.zshenv` equivalent), and when the probe's non-path answer
  was alias text or a function name, the user's own definition exists only there.
- **The delivery mechanism.** These are still `send-keys` into a live interactive
  shell, governed by part 4's 1023-byte guard.
- **The negative consequence "a user who redefined `cc`/`oc` themselves no longer
  changes what devgeta launches"** now applies to the typed paths as well.

**Naming note, unrelated to the decision above.** Part 3 and the Context table
refer to `ReviewCommand`, the reviewer's typed command as this ADR named it at
the time. That name never shipped: the review path's command construction was
split into `lookupBuiltinReviewer` (resolves the reviewer registered under a
key) and `reviewCommandFor` (renders its typed command), both in `layout.go`.
The narrative above is left as written — it describes the code as it stood —
but a reader grepping for `ReviewCommand` should look for those two instead.

## Consequences

### Positive

- **The truncation cannot recur on the create path.** It is not made less likely;
  the payload stops being terminal input, so the limit no longer applies.
- **Pane launch stops depending on interactive-shell state.** The coder panes get
  a deterministic command instead of one whose meaning depends on a `.zshrc`
  having been read. This is the same argument ADR-0011 made against depending on
  a coder's TUI rendering, applied one layer down.
- **A failed launch is visible.** A command that cannot start reports through the
  pane's process rather than sitting unexecuted on a command line.
- **No readiness timing anywhere.** The command is the pane's process from the
  first instant; there is no window during which the pane is not ready to receive
  what devgeta is sending.
- **Faster pane startup for devgeta-owned panes** — one non-interactive shell
  instead of a full interactive `.zshrc`.
- **When the probe resolves a path, launching stops depending on the pane's
  `PATH` at all, for every shell.** That closes a gap present in the current
  send-keys design too: bash and custom-shell users have no `.zshenv` to repair
  `PATH`, and the probe's shell need not be the shell tmux launches. When it does
  not resolve, the interactive-shell fallback leaves the pane exactly as
  well-off as it is today — never worse.

### Negative

- **devgeta owns the launch recipe in Go, not only in `devgeta.zsh`.** The alias
  stays (users type `cc` themselves), but it is generated from the Go constant
  rather than being the source. One more generated thing to keep honest, pinned
  by a test against the embedded config.
- **An absolute path is resolved per launch, so it can go stale within a pane's
  life.** A tool upgraded in place while a pane is open keeps running the binary
  the pane started. That already matches how any running process behaves, but it
  is a change from a `PATH` lookup performed at each launch.
- **A user who redefined `cc`/`oc` themselves no longer changes what devgeta
  launches.** Arguably correct — devgeta should launch what it verified is
  installed — but it is a behavior change for anyone doing it.
- **`; exec <shell>` means the pane command is a compound line.** It has to be
  assembled carefully, and the shell it re-execs has to be chosen (the user's
  `SHELL`, falling back to tmux's `default-shell`).
- **Two shell invocation modes to hold in mind** (non-interactive for
  devgeta-owned, interactive for `--pane`). Justified above, but it is a
  distinction a reader must learn.
- **Repair still cannot carry a long command.** It sends into a live pane by
  nature. Bounded by the guard in part 4 rather than fixed; a repair that ever
  needs to carry a prompt needs its own decision.

### Neutral

- `dg task review-run` is unaffected — it never used a pty.
- Existing window/pane rollback, pane-0 reselection, and the `pane-base-index`
  handling are untouched; only how a command reaches a pane changes.
- Tests get **stronger**: an exec'd command is an argument in a mocked
  `CommandParams`, so a test can assert the exact command a pane will run. Typed
  input could only be asserted as "some `send-keys` happened."

## Alternatives Considered

### Chunked `send-keys`

Split the command into sub-1024-byte writes.

Rejected, and explicitly ruled out for this work. It keeps the payload in the
terminal input path, so correctness depends on the pane's reader draining the
queue between chunks — the same load-dependent condition that produced the bug.
It would make the failure rarer and harder to reproduce without removing it.

### `load-buffer` + `paste-buffer`

Load the command into a tmux buffer and paste it into the pane.

Rejected on measurement: a 1528-byte command delivered this way never ran. tmux
buffers still arrive as pane input, so the pty cap applies unchanged.

### Write the prompt to a file and launch `cc "$(cat /path)"`

Keeps typing, but shrinks the typed line to a fixed length.

Rejected: it fixes only `--prompt`, leaving `--pane` and every future `send-keys`
caller exposed to the same cap; it introduces a temp-file lifecycle (where it
lives, who deletes it, what happens if the pane starts before it is written); and
the coder receives a prompt that came from a command substitution, so a failed
read produces an empty prompt rather than an error. It treats the symptom.

### Use `zsh -ic` for every pane, including devgeta's own

One uniform rule, and aliases keep working everywhere.

Rejected as the default: it makes every pane launch depend on the user's
`.zshrc` running correctly and quickly, which is the coupling ADR-0011 spent its
argument removing. It also runs the user's full interactive startup (plugin
managers, prompt frameworks, tool init) purely to resolve a two-character alias
devgeta itself wrote, and any warning that startup prints lands in the pane. Kept
for `--pane` only, where the user's shell environment is the point.

### Resolve the alias to a real command but keep typing it

Fixes the alias problem without touching the delivery mechanism.

Rejected: it does not address the bug at all. The command still goes through the
pty and is still cut at 1024 bytes.

### Rely on the pane shell's own `PATH` (the first draft's answer)

Launch the bare binary name and let the pane's shell resolve it, on the grounds
that `~/.zshenv` repairs `PATH` for every zsh invocation.

Rejected after review: it is true for zsh and only zsh. bash has no unconditional
equivalent (`$BASH_ENV` is unset by default), and the preflight probe's shell
(`$SHELL -i`) need not be the shell tmux launches (`default-shell`,
non-interactive) — so a supported bash or custom-shell user could pass the
install check and then hit `command not found` in the pane. Resolving an absolute
path at preflight removes the question for every shell instead of answering it
for one.

### Inject `PATH` into the pane with `new-window -e`

tmux can set environment variables on a new pane, so devgeta could pass its own
`PATH` in.

Rejected: it propagates whatever `PATH` `dg` itself happens to have, and
`shell_lookup.go` documents the case where that is precisely the truncated one
(`dg ws` started from a non-login pane). It would spread a possibly-broken `PATH`
rather than sidestep the need for one.
