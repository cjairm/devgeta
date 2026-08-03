# Release notes template

Copy this into the `--message-file` you pass to `devgeta task release`. That
file becomes **three** things at once, so it is worth writing properly:

1. the squashed commit message,
2. the annotated tag's message,
3. the GitHub release page body (`.github/workflows/release.yml` reads it back
   out of the tag).

Nothing else generates release notes. GitHub's own auto-generated bullet list
is built from **merged pull requests**, and devgeta tags straight from `main`,
so without this file a release page has nothing on it but a compare link.

```bash
cp docs/guides/RELEASE-NOTES-TEMPLATE.md /tmp/release-notes.txt
$EDITOR /tmp/release-notes.txt
devgeta task release v1.2.3 --message-file /tmp/release-notes.txt
```

---

## Structure

Keep the section headings below. Drop any section that has nothing in it —
an empty heading is worse than no heading. Follow CLAUDE.md's communication
style: plain language, lead with the outcome, no filler.

```
<type>(<scope>): <one line, imperative, no trailing period>

<One short paragraph: what a user actually observed, or what they can now do.
Written for someone who has not read the diff. If this is a bug fix, describe
the symptom before the cause.>

- <Change, stated as its effect. Say why it was wrong, not just what moved.>
- <One bullet per user-visible change. Group related edits into one bullet
  rather than listing files.>

## <Optional: a second group, when the release spans distinct areas>

- <...>

Behavior changes: <Anything an existing user could notice without reading the
code — a command that now errors where it used to succeed, output that
disappeared, a default that moved. Omit the line entirely if there are none.>

Migration: <Only when a user must run something by hand. Link the file in
docs/migrations/. Omit otherwise.>
```

The commit trailer (`Co-Authored-By:`) is stripped from the release page
automatically — leave it in the file.

---

## Worked example

```
feat(worktree): stop showing and start cleaning stale worktree entries

Deleted worktrees kept reappearing in `dg wt list` and the `dg ws` dashboard,
failed every diff with "cannot change to '<path>': No such file or directory",
and could not be removed.

- Parse git's "prunable" marker and drop those registrations from the
  enumeration. A registration whose directory is gone is administrative
  debris, not a worktree, and no command can run against its path.
- Resolve a worktree's real path from git rather than recomputing it from the
  configured location, so a worktree anywhere else is no longer invisible to
  every mutation.
- Add `dg wt prune --stale`, which clears git's leftover entries across every
  known repo. It cannot remove a worktree, directory, or branch, so it does
  not prompt.

Behavior changes: `dg wt remove <nonexistent>` now errors instead of silently
succeeding, and worktrees whose directory is gone no longer appear in
`dg wt list` or `dg ws`.
```

---

## What good looks like

| Do                                                                        | Don't                                             |
| ------------------------------------------------------------------------- | ------------------------------------------------- |
| "Deleted worktrees kept reappearing and could not be removed."            | "Fixed worktree bug."                             |
| "`dg wt remove <nonexistent>` now errors instead of silently succeeding." | Leave a behavior change for the user to discover. |
| One bullet per user-visible effect.                                       | One bullet per file touched.                      |
| Say why the old behavior was wrong.                                       | "Refactored `removeByRepo`."                      |
| Drop the empty sections.                                                  | Ship a heading with nothing under it.             |

## Checklist before tagging

- [ ] Subject line is one line and says what changed, not which files
- [ ] Someone who hasn't read the diff can tell what they get
- [ ] Every user-visible change has a bullet
- [ ] `Behavior changes:` is present, or genuinely does not apply
- [ ] `Migration:` links `docs/migrations/` if a manual step is required
- [ ] Version bump matches CLAUDE.md §9 (fix → PATCH, feature/flag → MINOR)
