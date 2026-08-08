# Release Guide

## Overview

This guide explains how to create new releases of devgeta using our automated GitHub Actions workflow. Releases are triggered by pushing version tags and automatically build binaries for all supported platforms.

## Prerequisites

- Write access to the devgeta repository
- Git configured with your GitHub credentials
- Clean working directory (all changes committed)

## Release Process

### 1. Prepare for Release

Before creating a release, ensure:

- All intended changes are merged to `main`
- The **full** suite is passing: `go test ./...` — this is the one place the whole
  suite is mandatory (day-to-day work runs targeted tests, so this run is the only
  one that sees the entire tree). Never tag on a partial run.
- Build succeeds locally: `go build -o devgeta main.go`
- Documentation is up to date

### 2. Determine Version Number

See [CLAUDE.md section 9](../../CLAUDE.md#9-versioning--tagging) for which bump to use (PATCH, MINOR, or MAJOR).

### 3. Write the Release Notes

Every release is tagged through `devgeta task release`, which requires a
message file and refuses an empty one. Start from the template:

```bash
cp docs/guides/RELEASE-NOTES-TEMPLATE.md /tmp/release-notes.txt
$EDITOR /tmp/release-notes.txt
```

That one file becomes the squashed commit message, the annotated tag's
message, **and the GitHub release page body** — the workflow reads the notes
back out of the tag (see [Release Workflow Details](#release-workflow-details)).

This is the only thing that puts content on a release page. GitHub's
auto-generated bullet list is built from **merged pull requests**, and devgeta
tags straight from `main`, so a release whose tag carries no message shows
nothing but a compare link.

### 4. Create and Push Release Tag

```bash
# Ensure you're on main branch with a clean tree and latest changes
git checkout main
git pull origin main

# Squash, tag, and push in one step (replace with your version)
devgeta task release v0.2.0 --message-file /tmp/release-notes.txt --push
```

Without `--push`, nothing is pushed and the command prints the exact
`git push` to run once you've reviewed the result.

**Important**: Tags must start with `v` to trigger the release workflow, and
the version must match `vMAJOR.MINOR.PATCH` exactly — `devgeta task release`
enforces both.

**Do not** run `git tag` / `git push --tags` by hand. That skips the squash
CLAUDE.md §9 requires, and a bare `git tag <version>` creates a _lightweight_
tag with no message at all — which is exactly how a release page ends up
empty. Don't push the commits first either: the squash counts commits ahead of
`origin/main`, so after a push that count is 0 and the squash is skipped with
no error.

Both failures have already happened. Pushing before tagging is how **v1.9.0
landed on a bare merge commit with 22 loose commits in `main`** — the tool
reported "no unpushed commits", skipped the squash, and tagged HEAD as-is, with
no error and no warning. A bare `git tag` is how a release page publishes empty;
deleting the tag to retry does not delete the release, so it lingers as a
permanent draft (see [Workflow Fails](#workflow-fails) for the correct retry
order: delete the release first, then the tag).

### 5. Monitor the Release Workflow

Once you push the tag, GitHub Actions automatically:

1. **Builds binaries** for all supported platforms:
   - `devgeta-darwin-amd64` (macOS Intel)
   - `devgeta-darwin-arm64` (macOS Apple Silicon)
   - `devgeta-linux-amd64` (Linux Intel)
   - `devgeta-linux-arm64` (Linux ARM)

2. **Creates a GitHub Release** with:
   - The annotated tag's message as the release body, followed by GitHub's
     "Full Changelog" compare link
   - All four binaries attached
   - Tag reference

**Monitor progress**:

- Visit: https://github.com/cjairm/devgeta/actions
- Look for the "Release" workflow
- Typical build time: 2-3 minutes

### 6. Verify the Release

After the workflow completes:

1. **Check the release page**:
   - Visit: https://github.com/cjairm/devgeta/releases
   - Verify your new release appears
   - Confirm all four binaries are attached

2. **Test the installation script**:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/cjairm/devgeta/main/install.sh | bash
   ```

3. **Verify the installed version**:
   ```bash
   devgeta --version
   ```

### 7. Update Documentation (Optional)

Consider updating:

- `README.md` with new features or changes
- `CHANGELOG.md` with detailed release notes
- Any relevant documentation in `docs/`

## Release Workflow Details

### Workflow File Location

`.github/workflows/release.yml`

### Workflow Trigger

```yaml
on:
  push:
    tags:
      - "v*"
```

The workflow triggers automatically when you push any tag starting with `v`.

### Build Process

The workflow:

1. Checks out the code (full history, so the annotated tag's message is available)
2. Sets up Go 1.25
3. Builds binaries for all platforms using cross-compilation
4. Reads the release notes out of the annotated tag
5. Creates a GitHub release with those notes and all binaries attached

### Binary Naming Convention

Binaries follow this pattern:

```
devgeta-{OS}-{ARCH}
```

**Examples**:

- `devgeta-darwin-amd64`
- `devgeta-linux-arm64`

## Troubleshooting

### Workflow Fails

If the GitHub Actions workflow fails:

1. **Check workflow logs**:
   - Go to: https://github.com/cjairm/devgeta/actions
   - Click on the failed workflow run
   - Review the logs for error messages

2. **Common issues**:
   - **Build errors**: Fix compilation errors locally first
   - **Test failures**: Ensure `go test ./...` passes
   - **Permission errors**: Verify repository has proper permissions

3. **Re-trigger workflow**:

   Deleting a tag does **not** delete the release that points at it — GitHub
   demotes that release to a **draft** instead, and it stays in the releases
   list forever. Delete the release first, then the tag:

   ```bash
   # Delete the release GitHub already created (leaves the tag alone)
   gh release delete v0.2.0 --yes

   # Now delete the tag locally and remotely
   git tag -d v0.2.0
   git push origin :refs/tags/v0.2.0

   # Fix the issue, then re-run the normal release flow so the new tag is
   # annotated with its notes again (a bare `git tag` would strand the
   # release page empty)
   devgeta task release v0.2.0 --message-file /tmp/release-notes.txt --push
   ```

   Leftover drafts from a re-tag are harmless but confusing. List and remove
   them with:

   ```bash
   gh api repos/cjairm/devgeta/releases --jq '.[] | select(.draft) | .tag_name'
   gh release delete <tag> --yes
   ```

### Release Not Appearing

If the release doesn't appear on GitHub:

1. **Check workflow status**: Ensure it completed successfully
2. **Check permissions**: Workflow needs `contents: write` permission
3. **Wait a moment**: Release creation can take a few seconds after workflow completes

### Binary Download Fails

If users report download failures:

1. **Verify binary exists**: Check the release page for all four binaries
2. **Check binary permissions**: Ensure binaries are marked as assets
3. **Test download URL**:
   ```bash
   curl -fsSL https://github.com/cjairm/devgeta/releases/download/v0.2.0/devgeta-darwin-amd64
   ```

## Best Practices

### Pre-Release Checklist

- [ ] All changes merged to `main`
- [ ] Full suite passing: `go test ./...` (not a targeted subset)
- [ ] Local build succeeds: `go build -o devgeta main.go`
- [ ] Version number determined (semantic versioning)
- [ ] CHANGELOG.md updated (if applicable)
- [ ] Documentation reviewed and updated

### Version Numbering Guidelines

See [CLAUDE.md section 9](../../CLAUDE.md#9-versioning--tagging) for the full versioning policy, bump decision table, and rules.

### Release Frequency

- **Patch releases** (v0.1.1, v0.1.2): As needed for bug fixes
- **Minor releases** (v0.2.0, v0.3.0): When new features are added
- **Major releases** (v1.0.0, v2.0.0): For significant changes or milestones

## Fallback: Manual Release

**Not the normal path.** Use this only when the workflow itself cannot run (for
example it is broken and a build must ship anyway), or to re-upload assets to a
release that already exists. Tag with `devgeta task release` either way — these
steps replace the workflow's build and upload, not the tagging.

### Build Binaries Locally

```bash
# macOS amd64
GOOS=darwin GOARCH=amd64 go build -o devgeta-darwin-amd64 -ldflags="-s -w" .

# macOS arm64
GOOS=darwin GOARCH=arm64 go build -o devgeta-darwin-arm64 -ldflags="-s -w" .

# Linux amd64
GOOS=linux GOARCH=amd64 go build -o devgeta-linux-amd64 -ldflags="-s -w" .

# Linux arm64
GOOS=linux GOARCH=arm64 go build -o devgeta-linux-arm64 -ldflags="-s -w" .

# Make executable
chmod +x devgeta-*
```

### Create Release via GitHub CLI

```bash
# Create release and upload binaries
gh release create v0.2.0 \
  devgeta-darwin-amd64 \
  devgeta-darwin-arm64 \
  devgeta-linux-amd64 \
  devgeta-linux-arm64 \
  --title "v0.2.0" \
  --notes "Release notes here"
```

### Create Release via GitHub Web UI

1. Go to: https://github.com/cjairm/devgeta/releases/new
2. Choose the existing annotated tag — do not create one here, the web UI makes
   a lightweight tag
3. Add release title and notes
4. Drag and drop all four binary files
5. Click "Publish release"

## Quick Reference

### Create New Release (Standard)

```bash
# 1. Prepare
git checkout main
git pull origin main

# 2. Write the release notes (this file becomes the release page body)
cp docs/guides/RELEASE-NOTES-TEMPLATE.md /tmp/release-notes.txt
$EDITOR /tmp/release-notes.txt

# 3. Squash, tag, then push — always in that order, because the squash counts
#    commits ahead of origin/main and pushing first makes that count 0
devgeta task release v0.2.0 --message-file /tmp/release-notes.txt --push

# 4. Wait 2-3 minutes for workflow

# 5. Test
curl -fsSL https://raw.githubusercontent.com/cjairm/devgeta/main/install.sh | bash
devgeta --version
```

Without `--push`, review the result and run the `git push origin main --tags`
the command prints. Never tag by hand — a bare `git tag` is unannotated, and
the release page is built from the tag's annotation.

### Delete a Release (recovery only)

Order matters: delete the release first, or GitHub leaves it behind as a
permanent draft. See [Re-trigger workflow](#workflow-fails) for the full retry.

```bash
# 1. Delete the release (GitHub CLI, or the web UI at
#    https://github.com/cjairm/devgeta/releases)
gh release delete v0.2.0 --yes

# 2. Delete the tag locally, then remotely
git tag -d v0.2.0
git push origin :refs/tags/v0.2.0
```

## Support

If you encounter issues with releases:

- **Check workflow logs**: https://github.com/cjairm/devgeta/actions
- **Review release guide**: This document
- **File an issue**: https://github.com/cjairm/devgeta/issues

## Resources

- **GitHub Actions Documentation**: https://docs.github.com/en/actions
- **Semantic Versioning**: https://semver.org/
- **Go Cross-Compilation**: https://go.dev/doc/install/source#environment
- **GitHub Releases**: https://docs.github.com/en/repositories/releasing-projects-on-github
