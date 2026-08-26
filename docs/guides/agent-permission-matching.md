# Agent permission matching — how the patterns actually resolve

devgeta ships one permission policy to two AI coding agents, hand-maintained in
two formats and kept in step by `internal/apps/opencode/permissions_test.go`
(CLAUDE.md §12). That test compares the two configs as **strings**. It cannot
know what a pattern matches at runtime, and this page is about the gap between
those two things.

Everything marked **verified** below was measured, not reasoned about:

| Agent       | Version               | How the result was read                                                                  |
| ----------- | --------------------- | ---------------------------------------------------------------------------------------- |
| OpenCode    | 1.18.23 (was 1.18.15) | `--print-logs --log-level DEBUG` prints the matched string and the winning rule          |
| Claude Code | 2.1.245               | no rule-level log exists; measured by **outcome** in `-p` mode, always against a control |

Re-measure before trusting any of it against a newer release; none of this is
contractual upstream behavior. Section 6 has both procedures.

**The one-line summary, because the two agents are opposites on both axes:**

|                                         | OpenCode           | Claude Code              |
| --------------------------------------- | ------------------ | ------------------------ |
| `~/…` reaches the home directory        | **no** — dead rule | **yes**                  |
| `**/…` reaches outside the project root | **yes**            | **no**                   |
| `*` crosses `/`                         | **yes**            | **no** (gitignore-style) |

So neither spelling is portable. A rule that must protect the home directory on
both agents has to ship in **both** spellings, and
`TestHomeAnchoredDeniesHaveGlobstarTwins` fails the build if one is missing.

---

## 1. `~/`-anchored patterns are dead on OpenCode — and live on Claude Code

**Verified, both agents.** OpenCode evaluates `read` and `edit` against a path it
has already relativized. A file in the home directory arrives at the matcher
looking like this (1.18.23; on 1.18.15 the same file arrived as
`../../../../../../.claude/RTK.md` — the spelling changed, the conclusion did
not):

```
message=evaluated permission=read pattern=Users/jair.mendez/.dgprobe/creds \
        action.permission=read action.pattern=* action.action=allow
```

That run had `"~/.dgprobe/**": "deny"` in the config. The rule is not in the
line: the match fell through to the `*` catch-all and the read succeeded. The
control with `"**/.dgprobe/**": "deny"` instead:

```
message=evaluated permission=read pattern=Users/jair.mendez/.dgprobe/creds \
        action.permission=read action.pattern=**/.dgprobe/** action.action=deny
```

Same file, same directory grant, one rule spelling apart. The same pair holds for
an exact filename (`~/.dgprobe/.dgprobe_netrc` fell through to `*`;
`**/.dgprobe_netrc` denied), and for `edit` as well as `read`.

**Claude Code is the mirror image.** There, `~/` is the spelling that works and
`**/` is the one that silently does not. Three runs against the same decoy file
at `$HOME/.dgprobe/creds`, from a project directory under `/private/tmp`:

| Deny rule in the throwaway settings | Outcome                                                                                               |
| ----------------------------------- | ----------------------------------------------------------------------------------------------------- |
| _none_ (control)                    | read succeeded                                                                                        |
| `Read(~/.dgprobe/**)`               | `<tool_use_error>File is in a directory that is denied by your permission settings.</tool_use_error>` |
| `Read(**/.dgprobe/**)`              | **read succeeded** — the rule never applied                                                           |

The third row is not a malformed pattern: the identical rule denied
`nested/.dgprobe/creds` _inside_ the project. `**/` on Claude Code anchors at the
working directory and reaches downward only, so it cannot see the home
directory — even with the home directory granted through
`additionalDirectories`.

**What this means for the shipped config.** Both spellings ship, side by side,
for every home-directory rule:

```jsonc
"~/.ssh/**":  "deny",   // live on Claude Code, inert on OpenCode
"**/.ssh/**": "deny",   // live on OpenCode, project-scoped on Claude Code
```

Deleting either half removes real protection from one agent. This was nearly
shipped as a _replacement_ of the `~/` rules — which reads like a fix and is a
regression on Claude Code.

On OpenCode this protection was previously **masked**, not exploited:
`permission.external_directory` grants only the scratch directory, so nothing
outside the project was reachable for those rules to protect. The consequence to
remember is unchanged and now has a live floor underneath it: widening
`external_directory` is not a read-only decision, but the `**/`-anchored rules
now fire underneath it.

---

## 2. `*` crosses `/` on OpenCode, and does not on Claude Code

**Verified, both agents, same fixture.** Pattern `**/probe/*.md`, file
`probe/sub/deep.md`:

- **OpenCode** — matched:

  ```
  message=evaluated permission=read pattern=…/probe/proj/probe/sub/deep.md \
          action.permission=read action.pattern=**/probe/*.md action.action=deny
  ```

- **Claude Code** — did **not** match; the read succeeded. The positive control
  in the same settings, `probe/top.md`, was denied, so the rule was live.

Consequences, and they differ by agent:

- On OpenCode you cannot narrow by directory depth. `**/.claude/*.md` is not
  "top-level markdown"; it also matches
  `~/.claude/projects/<slug>/memory/notes.md`. Narrow by **exact filename**
  instead.
- On Claude Code the same pattern _is_ "one segment", which is why the
  `~/.claude/*.json` / `*.sh` / `*.md` trio does the right thing there.
- In OpenCode's `external_directory`, a single `*` grants the **whole subtree**.
  Verified: a request for `…/.claude/projects/<slug>/memory/*` was allowed by the
  rule `…/.claude/*`. Do not read `dir/*` as "direct children only".

---

## 3. The two agents resolve conflicts differently

A rule present in both configs with the same action can still behave
differently, because the resolution orders are not the same:

|             | Conflict resolution                                                                        | Negation                                           |
| ----------- | ------------------------------------------------------------------------------------------ | -------------------------------------------------- |
| Claude Code | deny → ask → allow; **first match in that order wins**, rule specificity is not a tiebreak | none                                               |
| OpenCode    | **longest matching pattern wins**, regardless of order in the file                         | none, but a _longer allow_ re-opens a shorter deny |

So the "broad deny plus a narrower carve-out" shape works on OpenCode and is
**impossible to express** on Claude Code. That asymmetry is why the global
Claude root is enumerated surface-by-surface in both configs rather than swept
with a blanket deny and a carve-out — see
[ADR-0014](../decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)'s
memory amendment.

The OpenCode allowlist inversion, for reference (verified):

```jsonc
"read": {
  "*":                    "allow",
  "**/.claude/**":        "deny",   // 13 chars
  "**/.claude/CLAUDE.md": "allow"   // 20 chars -> wins
}
```

---

## 4. The global Claude root: why it is enumerated by filename

Sections 1 and 2 interact badly, and this is where they land.

The `edit` floor protects the global Claude config surface, deliberately shaped
so that `~/.claude/projects/<slug>/memory/` stays writable — memory is data the
agent is meant to write (ADR-0014's amendment).

The obvious repair for section 1 — re-anchor `~/.claude/*.md` to
`**/.claude/*.md` so it fires on OpenCode — re-breaks memory, because on OpenCode
`*` crosses `/` and memory files are markdown. **Probed, not assumed** (this
section used to be marked "derived, not probed"; it no longer is). With
`"**/.dgprobe-claude/*.md": "deny"` in the `edit` block and an edit aimed at
`…/.dgprobe-claude/projects/slug/memory/MEMORY.md`:

```
message=evaluated permission=edit \
        pattern=Users/jair.mendez/.dgprobe-claude/projects/slug/memory/MEMORY.md \
        action.permission=edit action.pattern=**/.dgprobe-claude/*.md action.action=deny
```

The memory file was left untouched on disk. So the trap is real, and a longer
allow cannot rescue it: `*` crossing `/` means any `.md` allow is just as broad
and would re-open the config files the floor exists to protect.

**The shape that does work is exact-filename enumeration**, and it is verified in
both directions on both agents. OpenCode, with
`"**/.dgprobe-claude/CLAUDE.md": "deny"`:

```
# the config file — denied
message=evaluated permission=edit pattern=Users/jair.mendez/.dgprobe-claude/CLAUDE.md \
        action.permission=edit action.pattern=**/.dgprobe-claude/CLAUDE.md action.action=deny

# the memory file — falls through to the catch-all, and the write landed
message=evaluated permission=edit \
        pattern=Users/jair.mendez/.dgprobe-claude/projects/slug/memory/MEMORY.md \
        action.permission=edit action.pattern=* action.action=allow
```

Claude Code reaches the same place by the other route: because its `*` does not
cross `/`, the existing `~/.claude/*.md` trio already denies the direct children
and already leaves memory writable. Verified with the same decoy tree —
`Edit(~/.dgprobe-claude/*.md)` denied `…/CLAUDE.md` (file unchanged on disk) and
allowed `…/projects/slug/memory/MEMORY.md` (write landed).

So the floor ships **both** halves, and they are not redundant:

| Spelling                                                      | Live on     | Covers                                                     |
| ------------------------------------------------------------- | ----------- | ---------------------------------------------------------- |
| `~/.claude/*.json`, `*.sh`, `*.md`                            | Claude Code | every direct child, including ones upstream adds later     |
| `**/.claude/<exact filename>`, one per deployed file          | OpenCode    | only the files named — see the known limit below           |
| `~/.claude/agents/**` + `**/.claude/agents/**` (and siblings) | both        | the config-bearing subdirectories, on their own agent each |

The enumerated filenames are exactly what devgeta and Claude Code put at the top
level of `~/.claude/`: `settings.json`, `settings.local.json`, the global
`CLAUDE.md`, `RTK.md` (devgeta's rtk integration), and the seven hook scripts
`internal/apps/claude/claude.go` deploys — `statusline.sh`, `format.sh`,
`task-redirect.sh`, `secret-guard.sh`, `suppression-guard.sh`,
`agent-config-guard.sh`, `agent-state.sh`.

**Known limit, stated rather than papered over:** on OpenCode that list is
default-allow against anything not named — a new direct child upstream adds is
writable until someone adds it here. This is ADR-0014 §4's rejected
enumerate-and-maintain approach, accepted for this one root because §3 already
assigns the default-deny job to `agent-config-guard.js`, which covers surface the
tools have not shipped yet. `TestGlobalClaudeFloorEnumerationStaysCurrent`
derives the `.sh` half from `configs/claude/` so at least devgeta's own scripts
cannot fall out of it silently.

---

## 5. Command denies are friction, not a boundary

`permission.bash` / `Bash(…)` denies raise the bar on an unsophisticated
injection. They do **not** close the `Bash(*)` gap — ADR-0014 §1 and
`docs/apps/claude.md`'s "Known limits" both say so, and this section exists so
nobody reads the newer entries as a stronger claim than the old ones.

The entries added for that purpose are the inline-code-execution one-liners —
`python3 -c *`, `python -c *`, `node -e *`, `node --eval *`, `ruby -e *`,
`perl -e *`, `php -r *` — plus `gh api *`, which is an **ask**, not a deny (see
below). They fire, and they are trivially walked around. Both halves verified:

- **They fire.** Claude Code with `Bash(python3 -c *)` denied:
  `Permission to use Bash with command python3 -c "print(42)" has been denied.`
  The control without the rule ran it and printed `42`. OpenCode, same pair:

  ```
  # control
  message=evaluated permission=bash pattern="python3 -c \"print(42)\"" \
          action.permission=bash action.pattern=* action.action=allow
  # with the rule
  message=evaluated permission=bash pattern="python3 -c \"print(42)\"" \
          action.permission=bash action.pattern="python3 -c *" action.action=deny
  ```

- **They are friction.** With the same rule in place, `printf %s "print(42)" |
python3` ran and printed `42` on **both** agents. OpenCode's log shows why —
  it evaluates each pipeline segment separately, and neither segment matches:

  ```
  message=evaluated permission=bash pattern="printf %s \"print(42)\"" action.pattern=* action.action=allow
  message=evaluated permission=bash pattern=python3                   action.pattern=* action.action=allow
  ```

Building a matcher that chased redirects, `tee`, `sed -i` and heredocs is the
option ADR-0014 §5 already rejected, for the reason this bullet demonstrates. The
real boundary for high-autonomy work is Claude Code's OS sandbox (`/sandbox`,
Seatbelt on macOS).

**`gh api *` is `ask`, not `deny`.** A deny would break a workflow devgeta itself
ships: `configs/shared/skills/receiving-code-review/SKILL.md` tells the agent to
reply to inline review comments with
`gh api repos/{owner}/{repo}/pulls/{pr}/comments/{id}/replies`. `ask` keeps the
human gate on an easy egress path without breaking a shipped skill. Verified that
the pattern matches at all, on both agents, with `gh api rate_limit`: denied when
the rule was a deny, allowed in the control.

Neither config can carry this reasoning inline — both are strict JSON with no
comment syntax, and `permissions_test.go` parses them, so a `//` comment would
break the build. That is why it lives here.

---

## 6. Probing this yourself

Do not infer behavior from what the agent says; read the log where there is one,
and otherwise read the outcome — never the agent's own summary of the outcome.

### OpenCode

```bash
OPENCODE_CONFIG=/path/to/throwaway.json \
  opencode run --print-logs --log-level DEBUG --format json \
  --dir "$PROJECT" -m <provider/model> "<prompt>" > out.json 2> err.log

grep -o "message=evaluated permission=.*" err.log | sort -u
```

`OPENCODE_CONFIG` points OpenCode at a throwaway policy without touching the real
one, so a probe never edits a developer's own config. A home-directory target
also needs an `external_directory` grant, or the directory gate refuses before
the `read`/`edit` matcher is ever consulted — and note the gate evaluates the
**containing directory**, so granting a single file does nothing.

### Claude Code

There is **no** rule-level log. `--debug` / `--debug-file` exist and were checked;
neither prints which permission rule matched, and nothing else does either. So
the probe reads the tool result, which does distinguish the two outcomes
unambiguously:

```bash
claude -p "<prompt>" \
  --settings /path/to/throwaway.json \
  --setting-sources '' \
  --tools "Read" \
  --add-dir "$HOME" \
  --model sonnet \
  --output-format stream-json --verbose \
  < /dev/null > out.jsonl
```

- `--setting-sources ''` loads **none** of user/project/local settings, so the
  developer's real `~/.claude/settings.json` cannot leak into the result.
  `--settings` alone is additive and would leave the real deny list in play.
- `--tools "Read"` removes Bash from the tool set entirely — stronger than
  denying it, and the same purpose: the agent must not be able to `cat` its way
  around a path check. (Add `Edit` — i.e. `--tools "Read Edit"` — when probing
  an edit rule, as the CC-D/CC-E entries in the task report that produced this
  page did; `--tools "Read"` alone leaves no Edit tool for the model to call at
  all.)
- A denied call comes back as
  `<tool_use_error>File is in a directory that is denied by your permission settings.</tool_use_error>`
  for Read/Edit, and
  `Permission to use Bash with command … has been denied.` for Bash. An allowed
  one comes back with the file contents. Read the `tool_result`, not the final
  text — the model's own "DENIED"/"OK" is a summary and has been wrong.

Method notes, each of which cost a wasted run when ignored:

- **Use decoy files, never real secrets.** If the deny turns out not to hold, the
  agent reads the file and may echo it. Probe `~/.ssh/**` with a decoy directory
  of the same _shape_, never with `~/.ssh` itself.
- **Reset the fixture between runs.** An edit probe whose file still carries the
  previous run's edit makes the model answer "DENIED" because `old_string` no
  longer matches. That looks exactly like a working deny and is not one.
- **Run with stdin closed** (`< /dev/null`). Runs left attached to an interactive
  stdin hung with no output.
- **Always run the control** — the same probe with the rule absent. One run
  proves one direction; without the control you cannot tell an effective rule
  from a no-op, which is precisely the failure this page documents.

---

## Related

- [ADR-0014](../decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)
  — why the guard hook, not path denies, is the protection layer
- [ADR-0015](../decisions/ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md)
  — the scratch directory and the `external_directory` / `additionalDirectories` grant
- `internal/apps/opencode/permissions_test.go` — the string-level parity guard,
  plus `TestHomeAnchoredDeniesHaveGlobstarTwins` and
  `TestGlobalClaudeFloorLeavesMemoryWritable`, which encode sections 1, 2 and 4
