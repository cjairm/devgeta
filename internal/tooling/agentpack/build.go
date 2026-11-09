package agentpack

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/pkg/logger"
)

// wrapperMarker is the text that makes an override SKILL.md a wrapper: it
// points the agent at upstream's original, kept beside it (ADR-0069 §4).
const (
	wrapperMarker = "UPSTREAM.md"
	upstreamFile  = "UPSTREAM.md"
)

// BuildOptions describes one build of one pack.
type BuildOptions struct {
	Manifest Manifest
	// Source is the fetched, read-only clone. It is never edited.
	Source string
	// Exclude lists skill folders to drop from the built tree.
	Exclude []string
	// Layers are override folders laid out like the pack, copied on top in
	// order, so a later layer wins: devgeta's, then the user's.
	Layers []string
}

// BuildResult describes a finished build.
type BuildResult struct {
	// Dir is the built tree. Empty for Check, which keeps nothing.
	Dir string
	// ReplacedHashes maps each fully replaced upstream file to the hash of
	// upstream's copy, so `check` can flag a replacement upstream has outrun.
	ReplacedHashes map[string]string
}

// Builder turns a fetched clone into the tree devgeta installs.
type Builder struct {
	// Root is paths.Paths.App.AgentPacks; the tree for a pack is Root/<pack>.
	Root string
	// rename is os.Rename unless a test needs to make the swap fail.
	rename func(oldPath, newPath string) error
}

func (b *Builder) doRename(oldPath, newPath string) error {
	if b.rename != nil {
		return b.rename(oldPath, newPath)
	}
	return os.Rename(oldPath, newPath)
}

// Build builds the pack beside its current tree and swaps it in. Any failure
// deletes the new tree and leaves the old one exactly as it was (ADR-0069 §8).
func (b *Builder) Build(o BuildOptions) (BuildResult, error) {
	if err := os.MkdirAll(b.Root, 0o755); err != nil {
		return BuildResult{}, fmt.Errorf("failed to create %s: %w", b.Root, err)
	}
	tmp, err := os.MkdirTemp(b.Root, ".build-"+o.Manifest.Name+"-")
	if err != nil {
		return BuildResult{}, fmt.Errorf("failed to create a build directory: %w", err)
	}
	res, err := buildInto(o, tmp)
	if err != nil {
		if rmErr := removeTree(tmp); rmErr != nil {
			logger.L().Warnw("could not delete a failed build", "dir", tmp, "err", rmErr)
		}
		return BuildResult{}, fmt.Errorf("could not build %s: %w", o.Manifest.Name, err)
	}
	final := filepath.Join(b.Root, o.Manifest.Name)
	if err := b.swap(tmp, final); err != nil {
		return BuildResult{}, err
	}
	res.Dir = final
	return res, nil
}

// Check runs a full build and every build check into a temp tree and deletes
// it, so `dg agent-pack check` can try a tag without touching what is installed.
// inspect, when set, is shown the built tree before it is deleted (for
// `claude plugin validate`); an error from it fails the check.
func (b *Builder) Check(o BuildOptions, inspect func(dir string) error) (BuildResult, error) {
	if err := os.MkdirAll(b.Root, 0o755); err != nil {
		return BuildResult{}, fmt.Errorf("failed to create %s: %w", b.Root, err)
	}
	tmp, err := os.MkdirTemp(b.Root, ".check-"+o.Manifest.Name+"-")
	if err != nil {
		return BuildResult{}, fmt.Errorf("failed to create a build directory: %w", err)
	}
	defer func() {
		if rmErr := removeTree(tmp); rmErr != nil {
			logger.L().Warnw("could not delete a check build", "dir", tmp, "err", rmErr)
		}
	}()
	res, err := buildInto(o, tmp)
	if err != nil {
		return BuildResult{}, fmt.Errorf("could not build %s: %w", o.Manifest.Name, err)
	}
	if inspect != nil {
		if err := inspect(tmp); err != nil {
			return BuildResult{}, err
		}
	}
	return res, nil
}

// swap replaces final with tmp. A directory can't be renamed over a non-empty
// one, so the old tree goes aside first, the new one goes in, and only then is
// the old one deleted. If the second rename fails the old tree is renamed back.
func (b *Builder) swap(tmp, final string) error {
	old := ""
	if _, err := os.Lstat(final); err == nil {
		old = tmp + ".old"
		if err := b.doRename(final, old); err != nil {
			_ = removeTree(tmp)
			return fmt.Errorf("failed to move the previous %s tree aside: %w", filepath.Base(final), err)
		}
	}
	if err := b.doRename(tmp, final); err != nil {
		if old != "" {
			if restoreErr := b.doRename(old, final); restoreErr != nil {
				_ = removeTree(tmp)
				return fmt.Errorf(
					"failed to move the new %s tree into place (%v) and could not put the previous tree back (%v); "+
						"the previous tree is at %s",
					filepath.Base(final), err, restoreErr, old,
				)
			}
		}
		_ = removeTree(tmp)
		return fmt.Errorf("failed to move the new %s tree into place: %w", filepath.Base(final), err)
	}
	if old != "" {
		if err := removeTree(old); err != nil {
			// The new tree is live; a stale copy beside it is only clutter.
			logger.L().Warnw("could not delete the previous tree", "dir", old, "err", err)
		}
	}
	return nil
}

// buildInto does the whole build into dst: copy, exclude, overlay, wrap, check.
func buildInto(o BuildOptions, dst string) (BuildResult, error) {
	upstreamSkills, err := listSkills(o.Source)
	if err != nil {
		return BuildResult{}, fmt.Errorf("failed to list upstream's skills: %w", err)
	}
	known := make(map[string]bool, len(upstreamSkills))
	for _, s := range upstreamSkills {
		known[s] = true
	}

	excluded := map[string]bool{}
	var excludeList []string
	for _, name := range o.Exclude {
		if !known[name] {
			return BuildResult{}, fmt.Errorf(
				"cannot exclude %q: %s has no such skill (it has: %s)",
				name, o.Manifest.Name, strings.Join(upstreamSkills, ", "),
			)
		}
		if !excluded[name] {
			excluded[name] = true
			excludeList = append(excludeList, name)
		}
	}

	if err := copyTree(o.Source, dst); err != nil {
		return BuildResult{}, fmt.Errorf("failed to copy upstream: %w", err)
	}
	if o.Manifest.Embedded() {
		// An embedded pack's source folder holds its own pack.yaml, which is
		// devgeta's pin and not part of what coders load.
		if err := os.Remove(filepath.Join(dst, manifestFile)); err != nil && !os.IsNotExist(err) {
			return BuildResult{}, fmt.Errorf("failed to drop %s from the built tree: %w", manifestFile, err)
		}
	}
	for _, name := range excludeList {
		if err := os.RemoveAll(filepath.Join(dst, "skills", name)); err != nil {
			return BuildResult{}, fmt.Errorf("failed to remove skill %s: %w", name, err)
		}
	}

	ov := &overlay{
		source:   o.Source,
		dst:      dst,
		known:    known,
		excluded: excluded,
		skillMD:  map[string]bool{},
		replaced: map[string]bool{},
	}
	for _, layer := range o.Layers {
		if err := ov.apply(layer); err != nil {
			return BuildResult{}, err
		}
	}
	wrappers, err := ov.finishSkills()
	if err != nil {
		return BuildResult{}, err
	}

	if err := checkNoDanglingReferences(dst, excludeList); err != nil {
		return BuildResult{}, err
	}
	required := checkRequiredFiles
	if o.Manifest.Embedded() {
		required = checkEmbeddedFiles
	}
	if err := required(dst); err != nil {
		return BuildResult{}, err
	}

	hashes := map[string]string{}
	for rel := range ov.replaced {
		if wrappers[rel] {
			continue // a wrapper follows upstream, so there is nothing to re-read
		}
		h, err := hashFile(filepath.Join(o.Source, filepath.FromSlash(rel)))
		if err != nil {
			return BuildResult{}, fmt.Errorf("failed to hash upstream's %s: %w", rel, err)
		}
		hashes[rel] = h
	}
	return BuildResult{ReplacedHashes: hashes}, nil
}

// overlay copies override layers onto a built tree and remembers what it
// replaced, for the checks that run once every layer is in.
type overlay struct {
	source, dst string
	known       map[string]bool // upstream's skill folders
	excluded    map[string]bool
	skillMD     map[string]bool // skills whose SKILL.md some layer provides
	replaced    map[string]bool // slash paths that exist upstream and were overwritten
}

func (ov *overlay) apply(layer string) error {
	if _, err := os.Stat(layer); err != nil {
		if os.IsNotExist(err) {
			return nil // no overrides in this layer
		}
		return fmt.Errorf("failed to read the override layer %s: %w", layer, err)
	}
	return filepath.WalkDir(layer, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(layer, p)
		if err != nil {
			return err
		}
		if rel == "." || d.IsDir() {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if slash == manifestFile {
			return nil // the pin lives beside the overrides but is not part of the pack
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("override %s in %s is not a regular file", slash, layer)
		}
		if err := ov.checkSkillTarget(slash, layer); err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(ov.source, rel)); err == nil {
			ov.replaced[slash] = true
		}
		return copyFileKeepingMode(p, filepath.Join(ov.dst, rel))
	})
}

// checkSkillTarget rejects an override that would add a skill, resurrect an
// excluded one, or ship its own UPSTREAM.md.
func (ov *overlay) checkSkillTarget(slash, layer string) error {
	parts := strings.Split(slash, "/")
	if len(parts) < 3 || parts[0] != "skills" {
		return nil
	}
	skill, file := parts[1], parts[len(parts)-1]
	if !ov.known[skill] {
		return fmt.Errorf(
			"override %s in %s targets the skill %q, which upstream does not have: "+
				"an override can change a skill but not add one",
			slash, layer, skill,
		)
	}
	if ov.excluded[skill] {
		return fmt.Errorf("the skill %q is both excluded and overridden (%s in %s)", skill, slash, layer)
	}
	if len(parts) == 3 && file == upstreamFile {
		return fmt.Errorf(
			"override %s in %s would replace UPSTREAM.md, which the build always writes from upstream's own SKILL.md",
			slash, layer,
		)
	}
	if len(parts) == 3 && file == "SKILL.md" {
		ov.skillMD[skill] = true
	}
	return nil
}

// finishSkills runs after every layer is copied. For each skill whose SKILL.md
// was overridden it checks the name (and, for a wrapper, the description)
// against upstream's, and writes UPSTREAM.md beside a wrapper. It returns the
// slash paths of the wrappers' SKILL.md files.
func (ov *overlay) finishSkills() (map[string]bool, error) {
	wrappers := map[string]bool{}
	for skill := range ov.skillMD {
		rel := "skills/" + skill + "/SKILL.md"
		final, err := os.ReadFile(filepath.Join(ov.dst, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		upstream, err := os.ReadFile(filepath.Join(ov.source, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		got, err := parseSkillMeta(final)
		if err != nil {
			return nil, fmt.Errorf("the override of skill %q is not a usable SKILL.md: %w", skill, err)
		}
		want, err := parseSkillMeta(upstream)
		if err != nil {
			return nil, fmt.Errorf("upstream's skill %q has no readable frontmatter: %w", skill, err)
		}
		if got.Name != want.Name {
			return nil, fmt.Errorf(
				"the override of skill %q is named %q: it must keep upstream's name %q "+
					"so other skills' calls still reach it",
				skill, got.Name, want.Name,
			)
		}
		if !bytes.Contains(final, []byte(wrapperMarker)) {
			continue // a full replacement may reword its description
		}
		if got.Description != want.Description {
			return nil, fmt.Errorf(
				"the wrapper for skill %q changes its description: a wrapper must keep upstream's "+
					"description (%q), which is how agents decide when to load it",
				skill, want.Description,
			)
		}
		wrappers[rel] = true
		if err := os.WriteFile(
			filepath.Join(ov.dst, "skills", skill, upstreamFile), upstream, 0o644,
		); err != nil {
			return nil, fmt.Errorf("failed to write UPSTREAM.md for skill %q: %w", skill, err)
		}
	}
	return wrappers, nil
}
