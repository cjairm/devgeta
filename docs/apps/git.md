# Git

Devgeta installs [Git](https://git-scm.com) and [`gh`](https://cli.github.com)
as terminal tools, and ships a starter `.gitconfig`. It does **not** set up your
GitHub credentials — do that yourself with [Set up GitHub auth](#set-up-github-auth).

- **Module:** `internal/apps/git/`
- **Config source:** `configs/git/.gitconfig` → `~/.config/git/.gitconfig`
- **Install:** both come with the `terminal` category (`dg install --only terminal`)

## Activate the shipped .gitconfig

Devgeta writes to `~/.config/git/.gitconfig`, but git only reads
`~/.config/git/config` or `~/.gitconfig`. Pull it in and set your identity:

```bash
git config --global include.path ~/.config/git/.gitconfig
git config --global user.name "Your Name"
git config --global user.email "you@example.com"
```

What you get: `main` as the default branch, `pull.rebase`, `push.default =
current`, `push.followTags`, `fetch.prune`, histogram diffs, `diff3` conflict
markers, and the aliases `st co br ci lg last amend unstage`.

## Set up GitHub auth

GitHub stopped accepting account passwords for git over HTTPS in 2021, so an
HTTPS password prompt will always fail. Pick one of these three.

### Option 1 — SSH (recommended)

```bash
ssh-keygen -t ed25519 -C "you@example.com"
cat ~/.ssh/id_ed25519.pub
```

Paste the key at **GitHub → Settings → SSH and GPG keys → New SSH key**, then:

```bash
ssh -T git@github.com                                    # verify
git clone git@github.com:<owner>/<repo>.git              # clone with SSH
git remote set-url origin git@github.com:<owner>/<repo>.git   # convert an HTTPS clone
```

### Option 2 — gh

```bash
gh auth login    # browser flow; say yes to "authenticate Git with your GitHub credentials"
gh auth status   # verify
```

Sets `credential.helper = !gh auth git-credential`, so HTTPS stops prompting.

### Option 3 — personal access token

Create one at **GitHub → Settings → Developer settings → Personal access
tokens**, then:

```bash
git config --global credential.helper osxkeychain   # macOS
git config --global credential.helper store         # Linux — plaintext, see below
```

Clone again, enter your username, paste the token as the password.

`store` writes the token unencrypted to `~/.git-credentials`. For a keyring on
Debian/Ubuntu you have to compile git's helper:

```bash
sudo apt install make gcc libsecret-1-0 libsecret-1-dev libglib2.0-dev
sudo make --directory=/usr/share/doc/git/contrib/credential/libsecret
git config --global credential.helper \
  /usr/share/doc/git/contrib/credential/libsecret/git-credential-libsecret
```

Easier on Linux: use Option 1 or 2.

### Private repos

Auth alone is not access. If a correct credential still fails:

- Your account needs to be added to the repo.
- **SAML SSO orgs:** authorize the key or token for that org — per key at
  Settings → SSH and GPG keys, per token at Developer settings → tokens.

## Branch work

```bash
# new branch off latest main
git fetch origin
git checkout -b <branch> origin/main
git add -p                            # stage hunk by hunk
git commit -m "feat: description"
git push -u origin HEAD

git checkout -b <local> origin/<remote-branch>   # check out an existing remote branch
git cherry-pick <hash> [<hash> ...]              # move specific commits here

# squash another branch into a clean one
git merge --squash origin/<source-branch>
git commit -m "feat: combined description"

# clean up after a merge
git checkout main
git fetch && git remote prune origin && git pull origin main
git branch -d <merged-branch>         # -D to force
```

### Re-sync with main — uncommitted work

```bash
git reset --soft <commit-before-your-work>
git stash
git merge main            # resolve conflicts
git stash pop
git restore --staged .    # optional
```

### Re-sync with main — committed work

```bash
git switch feat/your-branch
git rebase main           # linear history; conflicts resolved per commit
```

If conflicts are messy (lockfiles), or the branch is shared and must keep its
SHAs, merge instead:

```bash
git switch -c wip/your-branch     # pin your commits
git switch feat/your-branch
git reset --hard <old-base>       # main's old base
git merge main                    # fast-forward
git merge wip/your-branch         # replay; resolve conflicts once
git branch -d wip/your-branch
```

Either way, push with `git push --force-with-lease`.

### Rename master to main

```bash
git checkout master
git branch -m master main
git fetch
git branch --unset-upstream
git branch -u origin/main
git remote set-head origin -a
```

## Undo

| Goal                            | Command                        |
| ------------------------------- | ------------------------------ |
| Unstage all, keep changes       | `git reset HEAD`               |
| Unstage one file                | `git restore --staged <file>`  |
| Discard a file's changes        | `git restore <file>`           |
| Undo last commit, keep staged   | `git reset --soft HEAD^`       |
| Undo last commit, discard it    | `git reset --hard HEAD^`       |
| Reword the last commit          | `git commit --amend -m "msg"`  |
| Add staged files to last commit | `git commit --amend --no-edit` |

## Inspect

```bash
git log main..<branch> --oneline     # commits <branch> has that main doesn't
git log --branches --not --remotes   # local commits never pushed
git diff-tree -p <commit>            # one commit's full patch
git branch -vv                       # every branch and what it tracks
git clean -Xfd                       # delete gitignored files
```

For a diff or log between two refs, `devgeta task review-package <base> <head>`
produces far less output — and the agent hooks redirect those commands to it
(see [claude.md](claude.md#command-redirect-pretooluse-hook)).

## Diagnose branch divergence

`git pull` says "Already up to date" but a PR's files aren't there — usually the
local and remote branch share a name but not a history.

```bash
# 1. do the tips differ?
git fetch origin <branch>
git rev-parse HEAD
git rev-parse origin/<branch>

# 2. does either side have unique commits?
git log --oneline origin/main..HEAD        # unique LOCAL
git log --oneline HEAD..origin/<branch>    # unique REMOTE

# 3. is an upstream even set?
git rev-parse --abbrev-ref @{upstream}     # errors if not

# 4. no unique local work → adopt the remote
git reset --hard origin/<branch>
git branch --set-upstream-to=origin/<branch>
```

## Recover lost commits

A `reset --hard`, bad rebase, or force-push leaves commits unreferenced for ~90
days.

```bash
# 1. reflog — look at the line BEFORE the reset
git reflog --date=iso --all

# 2. if that's not enough, scan dangling commits, newest first
for c in $(git fsck --no-reflogs 2>/dev/null | awk '/dangling commit/{print $3}'); do
  echo "$(git show -s --format='%ci %h %an | %s' $c)"
done | sort -r | head -30

# 3. inspect
git show <hash>

# 4. pin it so GC can't take it
git branch recovered <hash>

# 5. restore
git reset --hard <hash>
git push --force-with-lease origin <branch>
```

Staged but never committed: `git fsck --lost-found` writes blobs to
`.git/lost-found/other/` — you get contents, not filenames. Never staged is
unrecoverable; check your editor's local history.

Prevention: `git push --force-with-lease`, and `git branch backup` before a
risky rebase.

## Aliases

Shell-level, from `configs/templates/devgeta.zsh.tmpl`:

| Alias  | Expands to              |
| ------ | ----------------------- |
| `g`    | `git`                   |
| `gcm`  | `git commit -m`         |
| `gcam` | `git commit -a -m`      |
| `gcad` | `git commit -a --amend` |
| `lzg`  | `lazygit`               |

## Uninstall

```bash
dg uninstall git
```

Removes the package, deletes `~/.config/git/`, clears the `git` entry from
`global_config.yaml`.
