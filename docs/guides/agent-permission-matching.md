# Agent permission matching — how the patterns actually resolve

devgeta ships one permission policy to two AI coding agents, hand-maintained in
two formats and kept in step by `internal/apps/opencode/permissions_test.go`
(CLAUDE.md §12). That test compares the two configs as **strings**. It cannot
know what a pattern matches at runtime, and this page is about the gap between
those two things.

Everything marked **verified** below was measured against **opencode 1.18.15**
with `--print-logs --log-level DEBUG`, which prints the exact string matched and
the exact rule that won. Re-measure before trusting any of it against a newer
release; none of this is contractual upstream behavior.

---

## 1. The one that bites: `~/`-anchored patterns never match

**Verified.** OpenCode evaluates `read` and `edit` against a path **relative to
the project directory**. A file in the home directory arrives at the matcher
looking like this:

```
evaluated permission=read pattern=../../../../../../.claude/RTK.md
```

A rule written `~/.claude/**` is compared against that string and does not
match, so it never fires. The same holds for `edit`: a rule `~/.claude/*.md`
did not stop a write to `~/.claude/projects/<slug>/memory/probe.md` — the match
fell through to the `*` catch-all and the write succeeded.

Both namespaces were probed directly; every `~/`-anchored pattern tested failed
to fire. On that evidence the `~/`-anchored rules in
`configs/opencode/opencode.json.tmpl` — 13 in `read` (`~/.ssh/**`, `~/.aws/**`,
`~/.kube/**`, `~/.netrc`, `~/.git-credentials`, `~/.zsh_history`, …) and 13 in
`edit` (`~/.claude/*.json`, `~/.config/opencode/**`, `~/.zshrc`, …) — are
**dead rules on OpenCode**. They are not enforcing what they appear to enforce.

This is currently **masked**, not exploited: `permission.external_directory`
grants only the scratch directory, so nothing outside the project is reachable
for those rules to protect in the first place. The protection is coming from the
directory gate and from `agent-config-guard.js`, not from the deny lists.

**The consequence to remember:** the moment `external_directory` is widened to
any real directory, that directory is exposed to **both** reads and writes on
OpenCode, because no `~/`-anchored rule underneath it will fire. Widening the
grant is therefore not a read-only decision.

Patterns anchored with `**/` do work — verified in both directions:

```
evaluated permission=read pattern=../../../../../../.claude/.last-cleanup
                          action.pattern=**/.claude/**   action.action=deny
```

So `**/.env`, `**/id_rsa`, `**/.claude/settings.json` and their siblings are
live. Only the `~/`-anchored ones are inert.

---

## 2. `*` crosses `/`

**Verified.** This is not gitignore semantics. Pattern `**/probe/*.md` matched
`probe/sub/deep.md`.

Two consequences:

- You cannot narrow by directory depth. `**/.claude/*.md` is not "top-level
  markdown"; it also matches `~/.claude/projects/<slug>/memory/notes.md`.
  Narrow by **exact filename** instead.
- In `external_directory`, a single `*` grants the **whole subtree**. Verified:
  a request for `…/.claude/projects/<slug>/memory/*` was allowed by the rule
  `…/.claude/*`. Do not read `dir/*` as "direct children only".

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

## 4. The trap this combination sets

Sections 1 and 2 interact badly, and anyone re-anchoring the dead rules to make
them fire needs to know it before they start.

The `edit` floor protects the global Claude config surface by extension and
subdirectory (`~/.claude/*.md`, `~/.claude/agents/**`, …), deliberately shaped
so that `~/.claude/projects/<slug>/memory/` stays writable — memory is data the
agent is meant to write.

Re-anchoring `~/.claude/*.md` to `**/.claude/*.md` would make it fire — and
because `*` crosses `/`, it would then also match every memory file, since
memory files are markdown. Making the floor work would re-break memory, and on
OpenCode no longer-allow carve-out can rescue an individual file without also
re-opening the config files the floor exists to protect (`*` crossing `/` means
any `.md` allow is similarly broad).

**Derived, not probed:** this follows from §1 and §2, both verified, but the
re-anchored configuration itself was not run. Verify before acting on it.

The practical reading: on OpenCode, `agent-config-guard.js` is the layer doing
the real work for the global Claude root, exactly as ADR-0014 §3 intends. The
config floor there is defense-in-depth that currently provides no depth. Closing
that properly is an open decision, not a patch — it needs a shape that expresses
"config files protected, memory writable" under a matcher where `*` crosses `/`.

---

## 5. Probing this yourself

Do not infer behavior from what the agent says; read the log.

```bash
OPENCODE_CONFIG=/path/to/throwaway.json \
  opencode run --print-logs --log-level DEBUG --format json \
  --dir "$PROJECT" -m <model> "<prompt>" > out.json 2> err.log

grep -o "evaluated permission=[a-z_]* pattern=[^ ]* action.pattern=[^ ]* action.action=[a-z]*" err.log
```

`OPENCODE_CONFIG` points OpenCode at a throwaway policy without touching the
real one, so a probe never edits a developer's own config.

Method notes, each of which cost a wasted run when ignored:

- **Deny `bash` in the probe config.** Otherwise the agent may `cat` the file
  and route around the path gate, and the result proves nothing. (Whether bash
  is path-checked at all is untested — worth establishing.)
- **Use decoy files, never real secrets.** If the deny turns out not to hold,
  the agent reads the file and may echo it.
- **Run with stdin closed** (`< /dev/null`). Runs left attached to an
  interactive stdin hung with no output.
- **Always run the control** — the same probe with the rule absent. One run
  proves one direction; without the control you cannot tell an effective rule
  from a no-op, which is precisely the failure this page documents.

---

## Related

- [ADR-0014](../decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)
  — why the guard hook, not path denies, is the protection layer
- [ADR-0015](../decisions/ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md)
  — the scratch directory and the `external_directory` / `additionalDirectories` grant
- `internal/apps/opencode/permissions_test.go` — the string-level parity guard
