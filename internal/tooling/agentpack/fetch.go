package agentpack

import (
	"fmt"
	"os"
	"path/filepath"
)

// GitClient is the part of the git app wrapper the fetch needs. The real
// *git.Git satisfies it; tests give it a wrapper over a mocked executor.
type GitClient interface {
	CloneTag(url, tag, dstPath string) error
	HeadIn(dir string) (string, error)
}

// Fetcher keeps one read-only clone per pinned commit under Root/.src.
type Fetcher struct {
	Git GitClient
	// Root is paths.Paths.App.AgentPacks.
	Root string
}

// srcDir is the cache location for a pack at a commit. The SHA is in the name,
// so a pin bump can never reuse a clone of the old commit.
func (f *Fetcher) srcDir(name, sha string) string {
	return filepath.Join(f.Root, ".src", name+"@"+sha)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Fetch returns the path of a read-only clone of the manifest's pinned tag,
// cloning it first when no clone at that commit is cached.
//
// The clone goes into a temp directory beside the cache and is renamed in only
// after its HEAD has been checked against the pinned SHA, so a moved tag or a
// failed download never leaves something the next run would trust.
func (f *Fetcher) Fetch(m Manifest) (string, error) {
	if m.Embedded() {
		return "", fmt.Errorf("pack %s is embedded and has nothing to fetch", m.Name)
	}
	if cache := f.srcDir(m.Name, m.SHA); isDir(cache) {
		return cache, nil
	}
	dir, _, err := f.clone(m, m.SHA, "install "+m.Name)
	return dir, err
}

// FetchRef is Fetch for a tag devgeta does not pin, such as `dg agent-pack
// check --ref v7.0.3`. There is no SHA to check against, so it returns the
// commit the tag resolved to, and caches the clone under it like any other.
func (f *Fetcher) FetchRef(m Manifest) (string, string, error) {
	if m.Embedded() {
		return "", "", fmt.Errorf("pack %s is embedded and has nothing to fetch", m.Name)
	}
	return f.clone(m, "", "check "+m.Name)
}

// clone clones m's tag into a temp dir and moves it into the cache. When
// wantSHA is set, HEAD must equal it; when it is empty, whatever HEAD is names
// the cache entry. retry is the `dg agent-pack` command to suggest after a
// network failure.
func (f *Fetcher) clone(m Manifest, wantSHA, retry string) (string, string, error) {
	srcRoot := filepath.Join(f.Root, ".src")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		return "", "", fmt.Errorf("failed to create %s: %w", srcRoot, err)
	}
	tmp, err := os.MkdirTemp(srcRoot, ".fetch-"+m.Name+"-")
	if err != nil {
		return "", "", fmt.Errorf("failed to create a temp directory for %s: %w", m.Name, err)
	}
	defer func() {
		// Whatever is left in tmp is a failed, discarded or already-moved attempt.
		_ = removeTree(tmp)
	}()

	clone := filepath.Join(tmp, "clone")
	if err := f.Git.CloneTag(m.Repo, m.Tag, clone); err != nil {
		return "", "", fmt.Errorf(
			"failed to fetch %s %s from %s: %w\n"+
				"Check your network connection, then run: dg agent-pack %s",
			m.Name, m.Tag, m.Repo, err, retry,
		)
	}
	head, err := f.Git.HeadIn(clone)
	if err != nil {
		return "", "", fmt.Errorf("failed to read the commit of %s %s: %w", m.Name, m.Tag, err)
	}
	if wantSHA != "" && head != wantSHA {
		return "", "", fmt.Errorf(
			"%s %s resolves to commit %s, but devgeta pins %s. "+
				"The tag was moved upstream or the pin is wrong; nothing was installed",
			m.Name, m.Tag, head, wantSHA,
		)
	}
	cache := f.srcDir(m.Name, head)
	if isDir(cache) {
		return cache, head, nil // the same commit is already cached
	}
	// Rename first, lock after: a directory can't be renamed into another
	// parent once its own write bit is gone (the ".." entry must be updated).
	if err := os.Rename(clone, cache); err != nil {
		// Another run may have cached the same commit between our check and now.
		if isDir(cache) {
			return cache, head, nil
		}
		return "", "", fmt.Errorf("failed to cache the %s clone: %w", m.Name, err)
	}
	if err := makeReadOnly(cache); err != nil {
		// The SHA was checked, but a clone we could not lock is not one to keep.
		if rmErr := removeTree(cache); rmErr != nil {
			return "", "", fmt.Errorf(
				"failed to make the %s clone read-only (%v) and could not remove it: %w",
				m.Name, err, rmErr,
			)
		}
		return "", "", fmt.Errorf("failed to make the %s clone read-only: %w", m.Name, err)
	}
	return cache, head, nil
}
