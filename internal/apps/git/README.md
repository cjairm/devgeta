# Git App

Installs and configures Git with devgeta integration.

## Recovering Lost Commits

Git rarely deletes commits immediately. A `reset --hard`, bad rebase, or force-push makes commits **unreferenced** — they survive ~90 days before GC.

### 1. Check reflog

```bash
git reflog --date=iso              # current branch
git reflog --date=iso --all        # all refs including remotes
```

Look for the line **before** the reset/rebase.

### 2. Scan dangling commits (if reflog isn't enough)

```bash
git fsck --no-reflogs --lost-found
```

List them readable, newest first:

```bash
for c in $(git fsck --no-reflogs 2>/dev/null | awk '/dangling commit/{print $3}'); do
  echo "$(git show -s --format='%ci %h %an | %s' $c)"
done | sort -r | head -30
```

### 3. Inspect before trusting

```bash
git show <hash>            # full diff
git show --stat <hash>     # files changed
git log --oneline <hash>   # chain behind it
```

### 4. Pin it (prevents GC)

```bash
git branch recovered <hash>
```

### 5. Restore

```bash
git reset --hard <hash>                      # move branch to commit
git push --force-with-lease origin <branch>  # restore remote
```

## Special Cases

**Staged but never committed (`git add` only):**

```bash
git fsck --lost-found                # writes blobs to .git/lost-found/other/
git show <blob-hash>                 # inspect contents
```

You get file contents but not names.

**Never staged:** Unrecoverable from git. Check editor undo/local history.

## Prevention

```bash
git push --force-with-lease    # refuses if remote changed
git branch backup              # before risky rebases
```

## Common Workflows

### Create a clean branch

```bash
git fetch origin
git checkout -b <branch> origin/main
git add . && git commit -m "feat: description"
git push -u origin <branch>
```

### Re-sync with main (preserving uncommitted work)

```bash
git reset --soft <commit-before-your-work>
git stash
git merge main
# resolve conflicts if any, then:
git stash pop
git restore --staged .   # optional: unstage
```

### Re-sync with main (preserving committed work)

When your work is already **committed**, replay it on top of the updated main.

Suppose your history is:

```
295769d  <-- main (old base)
   \
    ... your commits ...  <-- HEAD (feat/your-branch)
```

**Primary: rebase.** One command — linear history, no merge commit. Best for a
personal branch with a few clean commits. Conflicts are resolved per commit, so
they can recur across commits.

```bash
git switch feat/your-branch
git rebase main          # replays your commits on top of latest main
```

**Alternative: wip branch + merge.** Reach for this when conflicts are messy
(e.g. `package.json`, `package-lock.json`) and you'd rather resolve them once,
when you must keep the original commit SHAs (shared branch), or when you want an
explicit backup branch instead of relying on the reflog.

```bash
git switch -c wip/your-branch       # pin your commits on a temp branch
git switch feat/your-branch         # back to your working branch
git reset --hard <old-base>         # e.g. 295769d — drop to main's old base
git merge main                      # fast-forward to latest main
git merge wip/your-branch           # replay your commits; resolve conflicts once
```

Either way, if the branch was already pushed, update the remote with
`git push --force-with-lease`. Clean up the temp branch at the end:

```bash
git branch -d wip/your-branch
```

### Squash merge into clean branch

```bash
git fetch origin
git checkout -b <branch> origin/main
git merge --squash origin/<source-branch>
git commit -m "feat: combined description"
git push -u origin <branch>
```

## Diagnosing Branch Divergence

Symptom: `git pull` fails or says "Already up to date" but the expected files
(e.g. a PR's content) aren't present. Usually the local branch and the remote
branch **share a name but have different histories** — the local one is often
just a copy of `main` under a different name.

### 1. Compare the two tips

```bash
git rev-parse HEAD                  # local tip
git rev-parse origin/<branch>       # remote tip (after fetch)
```

Different hashes → divergent histories.

### 2. Check for unique commits on each side

```bash
git fetch origin <branch>
git log --oneline origin/main..HEAD              # unique LOCAL commits
git log --oneline HEAD..origin/<branch>          # unique REMOTE commits
```

If "unique local commits" is empty, the local branch has no work of its own —
it's safe to point it at the remote.

### 3. Check upstream tracking

```bash
git branch -vv                      # lists tracking branch per local branch
git rev-parse --abbrev-ref @{upstream}   # errors if no upstream is set
```

No upstream explains why a bare `git pull` fails. `git pull origin HEAD`
resolves `origin/HEAD` (usually `main`), which is why it pulls the wrong ref
and reports "Already up to date."

### 4. Align local branch to the real remote branch

Only when step 2 confirms no unique local work:

```bash
git fetch origin <branch>
git reset --hard origin/<branch>                 # adopt the remote history
git branch --set-upstream-to=origin/<branch>     # future `git pull` just works
```
