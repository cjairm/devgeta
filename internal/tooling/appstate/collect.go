// Phase 1 of `dg export`: resolve a profile and group selection against an
// adapter's allowlist and turn it into the *archive.ScanResult the writer
// consumes (cycle doc 2026-09-15-dg-export-import.md §5, Step 3).
//
// This is deliberately NOT archive.Scan of a root. Scan is where ADR-0041's
// skip rules live — "skip only on proof, when in doubt keep" — and app state
// inverts that premise (ADR-0045). So collection walks only the paths an
// adapter names, and Scan is reused one allowlisted directory at a time
// rather than reimplemented.
package appstate

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/tooling/archive"
)

// Porter is what this package needs of an exportable app: the StatePorter
// contract, plus the name every refusal has to print. Every StatePorter is
// an apps.App, so the name is always there — it is spelled out here rather
// than taking an apps.App so a test fake stays a handful of lines.
type Porter interface {
	Name() string
	apps.StatePorter
}

// Selection narrows what a run moves. An empty field means the default:
// every profile the adapter reports, and every default-on group.
type Selection struct {
	Profiles []string
	Groups   []string
}

// PathReport is one allowlisted path of one group of one profile, as
// resolved on this machine. Exists is false for a path the profile does not
// have, which is normal rather than an error — a fresh profile has no
// Sessions/ yet.
type PathReport struct {
	// Path is the path inside the bundle: "<profile key>/<path>".
	Path   string
	Exists bool
	Bytes  int64
}

// GroupReport is one group resolved for one profile. Every group the adapter
// declares gets one, selected or not: `--dry-run` has to show what did NOT
// move, which is the half of the answer a user cannot get any other way.
type GroupReport struct {
	Name     string
	Why      string
	Default  bool
	Selected bool
	Paths    []PathReport
	Bytes    int64
}

// ProfileReport is one profile's groups, in the adapter's declared order.
type ProfileReport struct {
	Key    string
	Root   string
	Groups []GroupReport
}

// Plan is a resolved selection: the entries to write, the base they are
// written relative to, and the report --dry-run prints.
type Plan struct {
	// Base is archive.Write's sourceRoot: the directory every profile root
	// is a direct child of. An entry's Path always starts with a profile
	// key, so Base + Path resolves to the real file.
	Base string
	// Scan holds only the selected groups' entries and their byte total.
	Scan     *archive.ScanResult
	Profiles []ProfileReport
	// Registry is each selected profile's display name and avatar, when the
	// adapter can report them. It is written beside the bundle rather than
	// into it, because it describes the profile directories rather than
	// living in one (ADR-0046). An adapter that is not a registrar leaves it
	// empty, and the export writes no sidecar.
	Registry map[string]apps.StateProfileInfo
}

// SelectedBytes is how many source bytes the plan would write.
func (p *Plan) SelectedBytes() int64 { return p.Scan.TotalBytes }

// FileCount is how many regular files the plan would write.
func (p *Plan) FileCount() int {
	n := 0
	for _, e := range p.Scan.Entries {
		if e.Kind == archive.KindFile {
			n++
		}
	}
	return n
}

// Collect resolves sel against porter's allowlist and walks only what it
// names. Sizes are computed for every group, including the ones this run
// will not write, because that report is what makes `--group extensions`
// a decision the user can make rather than guess at — and walking a tree
// without reading it is cheap next to copying it, which is the cost
// ADR-0045 actually set out to avoid.
func Collect(porter Porter, sel Selection) (*Plan, error) {
	groups := porter.StateGroups()
	// The denylist is checked at run time and not only in the test suite:
	// the guarantee ADR-0045 makes is about what leaves the machine, so the
	// run itself has to refuse, not just the build.
	if err := CheckAllowlist(porter.Name(), groups); err != nil {
		return nil, err
	}

	roots, err := porter.StateRoots()
	if err != nil {
		return nil, fmt.Errorf("finding %s's profiles: %w", porter.Name(), err)
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf(
			"%s has no profiles on this machine — nothing to export",
			porter.Name(),
		)
	}

	base, err := sharedBase(porter.Name(), roots)
	if err != nil {
		return nil, err
	}
	profileKeys, err := selectProfiles(porter.Name(), roots, sel.Profiles)
	if err != nil {
		return nil, err
	}
	selected, err := selectGroups(porter.Name(), groups, sel.Groups)
	if err != nil {
		return nil, err
	}

	plan := &Plan{Base: base, Scan: &archive.ScanResult{}}
	for _, key := range profileKeys {
		report := ProfileReport{Key: key, Root: roots[key]}
		for _, group := range groups {
			groupReport := GroupReport{
				Name:     group.Name,
				Why:      group.Why,
				Default:  group.Default,
				Selected: selected[group.Name],
			}
			for _, rel := range group.Paths {
				pathReport, entries, err := collectPath(key, roots[key], rel)
				if err != nil {
					return nil, err
				}
				groupReport.Paths = append(groupReport.Paths, pathReport)
				groupReport.Bytes += pathReport.Bytes
				if !groupReport.Selected {
					continue
				}
				plan.Scan.Entries = append(plan.Scan.Entries, entries...)
				plan.Scan.TotalBytes += pathReport.Bytes
			}
			report.Groups = append(report.Groups, groupReport)
		}
		plan.Profiles = append(plan.Profiles, report)
	}
	plan.Registry, err = collectRegistry(porter, profileKeys)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// collectRegistry reads the display names of the profiles this run selected,
// for adapters that have a registry to read. It is narrowed to the selected
// keys on purpose: a sidecar naming profiles the bundle does not carry would
// invite an import to create them empty.
func collectRegistry(porter Porter, profileKeys []string) (map[string]apps.StateProfileInfo, error) {
	registrar, ok := porter.(apps.StateProfileRegistrar)
	if !ok {
		return nil, nil
	}
	all, err := registrar.ReadProfileRegistry()
	if err != nil {
		return nil, fmt.Errorf("reading %s's profile names: %w", porter.Name(), err)
	}
	out := map[string]apps.StateProfileInfo{}
	for _, key := range profileKeys {
		if info, found := all[key]; found {
			out[key] = info
		}
	}
	return out, nil
}

// collectPath turns one allowlisted path into its bundle entries. rel is
// relative to the profile root; every returned entry's Path is prefixed with
// key, which is what keeps two profiles' identically-named files apart —
// archive.Entry.Path becomes the tar member name verbatim, so without the
// prefix both profiles' "Bookmarks" are one member and the second wins.
func collectPath(key, root, rel string) (PathReport, []archive.Entry, error) {
	bundlePath := path.Join(key, rel)
	report := PathReport{Path: bundlePath}
	full := filepath.Join(root, filepath.FromSlash(rel))

	info, err := os.Lstat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return report, nil, nil
		}
		return report, nil, fmt.Errorf("reading %s: %w", full, err)
	}

	switch {
	case info.IsDir():
		// archive.Scan returns nothing for the root itself (its walk
		// callback returns early on path == root), so the directory's own
		// entry is emitted here and Scan supplies what is inside it.
		entries := []archive.Entry{{
			Path:    bundlePath,
			Kind:    archive.KindDir,
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
		}}
		scanned, scanErr := archive.Scan(full, archive.ScanOptions{NoSkip: true})
		if scanErr != nil {
			return report, nil, fmt.Errorf("reading %s: %w", full, scanErr)
		}
		for _, entry := range scanned.Entries {
			childRel := path.Join(rel, entry.Path)
			if !collectable(entry.Kind) {
				continue
			}
			if _, denied := DeniedPath(childRel); denied {
				continue
			}
			entry.Path = path.Join(key, childRel)
			entries = append(entries, entry)
			if entry.Kind == archive.KindFile {
				report.Bytes += entry.Size
			}
		}
		report.Exists = true
		return report, entries, nil

	case info.Mode().IsRegular():
		// Scan is not reused for a single file: handed a file as its root it
		// returns an empty result, so the two most important default-on
		// groups (Bookmarks, Preferences) would export empty.
		report.Exists = true
		report.Bytes = info.Size()
		return report, []archive.Entry{{
			Path:    bundlePath,
			Kind:    archive.KindFile,
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
		}}, nil

	default:
		// A symlink, socket or device where a group expected a file or a
		// directory. Dropped rather than packed: import rejects any bundle
		// carrying a link member, so packing one would produce a bundle that
		// cannot be imported.
		return report, nil, nil
	}
}

// collectable reports whether an entry kind may go into a bundle. Only files
// and directories: archive.Scan reports symlinks and hard links too, and a
// link written early in a tar redirects a later, perfectly relative member
// outside the root — the escape the import's path rules cannot catch, so the
// two sides agree that a bundle never contains one.
func collectable(kind archive.EntryKind) bool {
	return kind == archive.KindFile || kind == archive.KindDir
}

// sharedBase returns the one directory every profile root is a direct child
// of, which archive.Write receives as sourceRoot. It enforces the contract
// StatePorter states, because a root that does not sit at base/<key> would
// make every entry path resolve to a file that is not there.
func sharedBase(app string, roots map[string]string) (string, error) {
	var base string
	for _, key := range sortedKeys(roots) {
		clean := filepath.Clean(roots[key])
		parent := filepath.Dir(clean)
		if filepath.Join(parent, key) != clean {
			return "", fmt.Errorf(
				"%s's profile %q is at %s, whose directory name does not match the profile key",
				app,
				key,
				clean,
			)
		}
		if base == "" {
			base = parent
			continue
		}
		if parent != base {
			return "", fmt.Errorf(
				"%s's profiles are not all under one directory (%s and %s):"+
					" one bundle can only hold profiles that share a parent",
				app,
				base,
				parent,
			)
		}
	}
	return base, nil
}

// selectProfiles resolves --profile against what the adapter reports,
// defaulting to every profile. An unknown key is an error that lists the
// real ones rather than an empty bundle.
func selectProfiles(app string, roots map[string]string, wanted []string) ([]string, error) {
	available := sortedKeys(roots)
	if len(wanted) == 0 {
		return available, nil
	}
	seen := map[string]bool{}
	var keys []string
	for _, name := range wanted {
		name = strings.TrimSpace(name)
		if _, ok := roots[name]; !ok {
			return nil, fmt.Errorf(
				"%s has no profile %q\n\nProfiles on this machine:\n  %s",
				app,
				name,
				strings.Join(available, "\n  "),
			)
		}
		if !seen[name] {
			seen[name] = true
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// selectGroups resolves --group against the adapter's allowlist, defaulting
// to the default-on groups.
func selectGroups(app string, groups []apps.StateGroup, wanted []string) (map[string]bool, error) {
	known := map[string]bool{}
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		known[g.Name] = true
		names = append(names, g.Name)
	}

	selected := map[string]bool{}
	if len(wanted) == 0 {
		for _, g := range groups {
			selected[g.Name] = g.Default
		}
		return selected, nil
	}
	for _, name := range wanted {
		name = strings.TrimSpace(name)
		if !known[name] {
			return nil, fmt.Errorf(
				"%s has no state group %q\n\nGroups:\n  %s",
				app,
				name,
				strings.Join(names, "\n  "),
			)
		}
		selected[name] = true
	}
	return selected, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
