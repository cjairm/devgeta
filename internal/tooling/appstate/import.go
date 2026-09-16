// `dg import`: read a bundle back into a live app directory, all or
// nothing. This is the destructive direction, so the order of the checks is
// the design — each one exists to fail before the step after it can do
// damage (cycle doc 2026-09-15-dg-export-import.md §5, Step 5):
//
//	running check      → a live process would corrupt what we write
//	bundle identity    → a Chrome bundle passes every path rule Brave has
//	manifest present   → without it nothing can be checked at all
//	directory writable → verify writes beside the bundle; a USB drive may not allow it
//	verify             → refuse a damaged transfer before touching good bytes
//	member gate        → the allowlist bound what we packed, not what is here
//	profile/group gate → refuse rather than silently restore less than asked
//	--force gate       → an import into a profile with state needs consent
//	back up everything → before the first write, so any failure rolls all of it back
package appstate

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/tooling/archive"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/sys/unix"
)

// BackupSuffix and AbsentSuffix mark what an import replaced. They stay on
// disk after a successful run — that is ADR-0045's recoverability promise,
// and with no per-import state written anywhere they are also the only undo
// a crash mid-write leaves behind. One suffix, documented, so the undo is a
// `find` and a `mv` rather than a support conversation.
const (
	BackupSuffix = ".dg-import-backup"
	AbsentSuffix = ".dg-import-absent"
)

var importSuffixes = files.BackupSuffixes{Backup: BackupSuffix, Absent: AbsentSuffix}

// ImportOptions controls one import.
type ImportOptions struct {
	// Force allows an import into a profile that already has state. The
	// state is backed up rather than destroyed, but overwriting a live app
	// directory is still the user's decision to make.
	Force            bool
	OnVerifyProgress archive.ProgressFunc
}

// ImportResult names what a successful import restored, and where the
// backups it left behind are.
type ImportResult struct {
	Profiles []string
	Groups   []string
	Files    int
	// Backups are the destination paths that now have a backup or absent
	// marker beside them.
	Backups []string
	// CreatedProfiles are the profiles this import had to create because the
	// bundle named them and the machine did not have them (ADR-0046). It is
	// a change to the browser the user did not explicitly ask for, so the
	// command layer reports it.
	CreatedProfiles []string
}

// restoreTarget is one (profile, group, group path) the bundle carries. It
// is the backup unit as well as the selection unit: a group path is what
// gets renamed aside and replaced wholesale, which is both far fewer
// backups than one per file and the only way a restored Sessions/ does not
// end up as the bundle's files mixed in with the machine's old ones.
type restoreTarget struct {
	Profile   string
	Group     string
	GroupPath string
}

// memberInfo is one tar member, resolved against the adapter's allowlist.
type memberInfo struct {
	Profile   string
	Rest      string // path under the profile root, forward-slash separated
	Group     string
	GroupPath string
	IsDir     bool
}

// writeMemberFile is the single point where an import touches the
// destination's file contents. It is a package variable for the same reason
// cmd/archive.go has archiveGOOS and archiveFreeBytes: the rollback path is
// otherwise reachable only through a real I/O error, and a guarantee that
// cannot be tested is not one.
var writeMemberFile = func(dest string, mode os.FileMode, r io.Reader) error {
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Import restores bundlePath into porter's profiles.
func Import(
	porter Porter,
	bundlePath string,
	sel Selection,
	opts ImportOptions,
) (*ImportResult, error) {
	if err := refuseIfRunning(porter); err != nil {
		return nil, err
	}
	if err := refuseForeignBundle(porter.Name(), bundlePath); err != nil {
		return nil, err
	}
	if err := refuseUnverifiableBundle(bundlePath); err != nil {
		return nil, err
	}
	if _, err := archive.Verify(bundlePath, archive.VerifyOptions{
		OnProgress: opts.OnVerifyProgress,
	}); err != nil {
		return nil, fmt.Errorf(
			"%s does not match its manifest, so it was damaged in transfer or "+
				"altered after it was written: %w\n"+
				"copy it from the drive again — nothing has been changed",
			filepath.Base(bundlePath),
			err,
		)
	}

	groups := porter.StateGroups()
	if err := CheckAllowlist(porter.Name(), groups); err != nil {
		return nil, err
	}
	roots, err := porter.StateRoots()
	if err != nil {
		return nil, fmt.Errorf("finding %s's profiles: %w", porter.Name(), err)
	}

	targets, err := inspectBundle(bundlePath, porter.Name(), groups)
	if err != nil {
		return nil, err
	}

	// A bundle from another machine names profile directories that machine
	// allocated. Chromium hands them out from a counter, so a directory like
	// "Profile 5" cannot be produced on a fresh machine by any sequence of
	// UI actions — refusing here would dead-end the case this feature exists
	// for. An adapter that can register a profile creates the missing ones
	// instead (ADR-0046); one that cannot leaves roots untouched and the
	// refusal below still fires.
	roots, created, registryBackups, err := ensureBundleProfiles(porter, bundlePath, targets, roots, sel.Profiles)
	if err != nil {
		return nil, withRollback(err, registryBackups)
	}
	sort.Strings(created)

	profiles, err := selectBundleProfiles(porter.Name(), targets, roots, sel.Profiles)
	if err != nil {
		return nil, withRollback(err, registryBackups)
	}
	selectedGroups, err := selectBundleGroups(porter.Name(), groups, targets, sel.Groups)
	if err != nil {
		return nil, err
	}

	var destPaths []string
	for _, t := range targets {
		if profiles[t.Profile] && selectedGroups[t.Group] {
			destPaths = append(
				destPaths,
				filepath.Join(roots[t.Profile], filepath.FromSlash(t.GroupPath)),
			)
		}
	}
	if err := refuseUnlessForced(porter.Name(), destPaths, opts.Force); err != nil {
		return nil, err
	}

	// Everything below this line changes the destination. Every path is
	// backed up before the first byte is written, so any failure can put
	// all of them back — CLAUDE.md §4's complete-or-fully-rolled-back rule,
	// which for a profile of many files is only satisfiable this way.
	// The registry write already happened, and its backup leads the list so
	// a rollback puts the profile list back along with the files.
	backedUp := registryBackups
	done, err := backupAll(destPaths)
	backedUp = append(backedUp, done...)
	if err != nil {
		return nil, withRollback(err, backedUp)
	}

	count, err := extractSelected(
		bundlePath,
		porter.Name(),
		roots,
		groups,
		profiles,
		selectedGroups,
	)
	if err != nil {
		return nil, withRollback(err, backedUp)
	}

	return &ImportResult{
		Profiles:        sortedSetKeys(profiles),
		Groups:          sortedSetKeys(selectedGroups),
		Files:           count,
		Backups:         settleBackups(backedUp),
		CreatedProfiles: created,
	}, nil
}

// settleBackups is the committed case: the import succeeded, so the absent
// markers have no one left to serve and are removed, while real backups
// stay — that is ADR-0045's recoverability promise, which is about paths
// that were REPLACED.
//
// A marker records "there was nothing here" so the rollback of an added
// path can be a delete. Once nothing can roll back, keeping it would
// scatter files a live app does not expect through its own profile
// directory — and on a new machine, which is the case this feature exists
// for, every path is an added one.
//
// A marker left behind by a crash is harmless litter and documented; it is
// the same suffix as everything else this command leaves.
func settleBackups(backedUp []string) []string {
	var replaced []string
	for _, p := range backedUp {
		if _, err := os.Lstat(p + BackupSuffix); err == nil {
			replaced = append(replaced, p)
			continue
		}
		// Not actionable: a marker that cannot be removed is litter beside
		// a file that was imported correctly, and failing the import over
		// it would be worse than leaving it.
		_ = os.Remove(p + AbsentSuffix)
	}
	return replaced
}

// refuseForeignBundle rejects a bundle written for a different app before
// anything reads its bytes.
//
// Every Chromium browser — Chrome, Edge, Vivaldi, Brave — uses the same
// profile schema, so Bookmarks, Preferences, Sessions/ and History are the
// same file names under the same Default / Profile N directories in all of
// them. A Chrome bundle therefore passes every path rule below, and would
// be written straight into the Brave profile.
//
// A name is not proof, and this does not pretend otherwise: a hand-built tar
// under the right name still passes. It stops the realistic accident — two
// bundles on one drive and the wrong path typed — and the member gate is
// what carries the actual safety guarantees.
func refuseForeignBundle(app, bundlePath string) error {
	base := filepath.Base(bundlePath)
	name, ok := strings.CutSuffix(base, BundleExt)
	if !ok {
		return fmt.Errorf(
			"%s is not a devgeta state bundle: `dg import %s` reads a %s written by `dg export %s`",
			base,
			app,
			BundleExt,
			app,
		)
	}
	prefix := BundlePrefix(app)
	if strings.HasPrefix(name, prefix) {
		return nil
	}
	return fmt.Errorf(
		"%s was not written for %s: `dg import %s` only accepts a bundle named %s<date>%s\n"+
			"every Chromium browser uses the same profile file names, so a bundle from "+
			"another one would restore cleanly and wrongly\n"+
			"if you renamed the bundle, rename it and its .sha256 back to the names the export gave them",
		base,
		app,
		app,
		prefix,
		BundleExt,
	)
}

// refuseUnverifiableBundle covers the two ways the verify step fails for a
// reason that is not the bundle's contents.
func refuseUnverifiableBundle(bundlePath string) error {
	manifestPath := strings.TrimSuffix(bundlePath, BundleExt) + ".sha256"
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf(
			"%s has no %s beside it, so it cannot be checked against what was exported\n"+
				"copy the manifest into the same directory as the bundle — the check is not skipped",
			filepath.Base(bundlePath),
			filepath.Base(manifestPath),
		)
	}
	// archive.Verify writes <bundle>.sha256 beside the bundle on success —
	// the archive's own checksum, a different file from the manifest it
	// read, so nothing is clobbered. But an import commonly runs straight
	// off a USB drive, and on a write-protected one that write fails even
	// though the bundle is sound. Refuse with the real reason rather than
	// let a write error read as corruption.
	dir := filepath.Dir(bundlePath)
	if err := unix.Access(dir, unix.W_OK); err != nil {
		return fmt.Errorf(
			"%s is not writable, and verifying the bundle writes its checksum there\n"+
				"copy %s and its .sha256 to a writable location and import from there",
			dir,
			filepath.Base(bundlePath),
		)
	}
	return nil
}

// inspectBundle walks every member once, rejecting the whole bundle on the
// first bad one, and returns what it carries. A bundle holding something it
// should not is not one to trust the rest of, so a failing member is never
// skipped.
func inspectBundle(bundlePath, app string, groups []apps.StateGroup) ([]restoreTarget, error) {
	reader, closeBundle, err := openBundle(bundlePath)
	if err != nil {
		return nil, err
	}
	defer closeBundle()

	seen := map[restoreTarget]bool{}
	var targets []restoreTarget
	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filepath.Base(bundlePath), err)
		}
		info, err := classifyMember(hdr.Name, hdr.Typeflag, app, groups)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(bundlePath), err)
		}
		target := restoreTarget{
			Profile:   info.Profile,
			Group:     info.Group,
			GroupPath: info.GroupPath,
		}
		if !seen[target] {
			seen[target] = true
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%s holds no state to restore", filepath.Base(bundlePath))
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Profile != targets[j].Profile {
			return targets[i].Profile < targets[j].Profile
		}
		if targets[i].Group != targets[j].Group {
			return targets[i].Group < targets[j].Group
		}
		return targets[i].GroupPath < targets[j].GroupPath
	})
	return targets, nil
}

// classifyMember is the member gate. It answers where a member would land
// and refuses every member that could land anywhere else.
//
// The allowlist it checks against is the adapter's WHOLE allowlist, not the
// run's selection: --group and --profile decide what gets written, so a
// member outside the selection is simply left alone, while a member outside
// the allowlist rejects the bundle. Checking the selection instead would
// mean narrowing a full bundle with --group bookmarks refused it for
// carrying the groups the user chose not to restore.
func classifyMember(
	name string,
	typeflag byte,
	app string,
	groups []apps.StateGroup,
) (memberInfo, error) {
	// A link member is rejected outright rather than resolved. One written
	// early redirects a later, perfectly relative member outside the root,
	// which is exactly the escape "reject .. and absolute paths" does not
	// catch — and none of the allowlisted state is a link, so nothing
	// legitimate is lost.
	if typeflag != tar.TypeReg && typeflag != tar.TypeDir {
		return memberInfo{}, fmt.Errorf(
			"member %q is a link or a special file; a bundle may hold only "+
				"regular files and directories",
			name,
		)
	}

	clean := strings.TrimSuffix(name, "/")
	if clean == "" || strings.HasPrefix(clean, "/") || path.Clean(clean) != clean {
		return memberInfo{}, fmt.Errorf(
			"member %q is not a plain relative path inside the bundle",
			name,
		)
	}

	profile, rest, found := strings.Cut(clean, "/")
	if !found || profile == "" || rest == "" {
		return memberInfo{}, fmt.Errorf(
			"member %q has no path under a profile directory",
			name,
		)
	}

	if why, denied := DeniedPath(rest); denied {
		return memberInfo{}, fmt.Errorf("member %q is on the denylist: %s", name, why)
	}

	for _, group := range groups {
		for _, groupPath := range group.Paths {
			if rest == groupPath || strings.HasPrefix(rest, groupPath+"/") {
				return memberInfo{
					Profile:   profile,
					Rest:      rest,
					Group:     group.Name,
					GroupPath: groupPath,
					IsDir:     typeflag == tar.TypeDir,
				}, nil
			}
		}
	}
	return memberInfo{}, fmt.Errorf(
		"member %q is not in any of %s's state groups",
		name,
		app,
	)
}

// selectBundleProfiles resolves --profile against what the bundle carries
// and what this machine has.
//
// A bundle profile the destination does not have is a refusal, not a
// directory devgeta creates: an app's profile registry is its own (for
// Brave it is Local State, which is denied), so a profile directory the app
// was never told about is one it never shows.
func selectBundleProfiles(
	app string,
	targets []restoreTarget,
	roots map[string]string,
	wanted []string,
) (map[string]bool, error) {
	inBundle := map[string]bool{}
	for _, t := range targets {
		inBundle[t.Profile] = true
	}
	available := sortedSetKeys(inBundle)

	selected := map[string]bool{}
	if len(wanted) == 0 {
		selected = inBundle
	} else {
		for _, name := range wanted {
			name = strings.TrimSpace(name)
			if !inBundle[name] {
				return nil, fmt.Errorf(
					"the bundle holds no profile %q\n\nProfiles in the bundle:\n  %s",
					name,
					strings.Join(available, "\n  "),
				)
			}
			selected[name] = true
		}
	}

	var missing []string
	for _, key := range sortedSetKeys(selected) {
		if _, ok := roots[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		// Deliberately not "create the profile first". Chromium names a
		// profile directory from a counter it keeps itself, so a user cannot
		// produce "Profile 5" on a fresh machine at all — that advice sent
		// people to a dead end (ADR-0046). What is actionable is restoring
		// the profiles that do exist, so the message hands over that exact
		// line.
		var here []string
		for key := range roots {
			here = append(here, key)
		}
		sort.Strings(here)

		usable := intersectSorted(sortedSetKeys(selected), here)
		msg := fmt.Sprintf(
			"the bundle has %s, which this machine does not have\n"+
				"  in the bundle: %s\n"+
				"  on this machine: %s",
			quotedList(missing),
			strings.Join(sortedSetKeys(selected), ", "),
			strings.Join(here, ", "),
		)
		if len(usable) > 0 {
			msg += fmt.Sprintf(
				"\n\nrestore the ones that exist with:\n  --profile %s",
				strings.Join(usable, ","),
			)
		}
		return nil, errors.New(msg)
	}
	return selected, nil
}

// selectBundleGroups resolves --group against the adapter's allowlist and
// what the bundle carries.
//
// The default is every group the bundle holds, including the opt-in ones: a
// group's export default has no say on the way in, because the export
// already decided what was worth carrying. A bundle written with
// --group history restores its history rather than silently dropping it.
func selectBundleGroups(
	app string,
	groups []apps.StateGroup,
	targets []restoreTarget,
	wanted []string,
) (map[string]bool, error) {
	inBundle := map[string]bool{}
	for _, t := range targets {
		inBundle[t.Group] = true
	}
	if len(wanted) == 0 {
		return inBundle, nil
	}

	known := map[string]bool{}
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		known[g.Name] = true
		names = append(names, g.Name)
	}

	selected := map[string]bool{}
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
		// A real group the bundle does not carry is a refusal naming it,
		// rather than an import that quietly restores less than it was
		// asked for.
		if !inBundle[name] {
			return nil, fmt.Errorf(
				"the bundle does not carry the %q group\n\nGroups in the bundle:\n  %s",
				name,
				strings.Join(sortedSetKeys(inBundle), "\n  "),
			)
		}
		selected[name] = true
	}
	return selected, nil
}

// refuseUnlessForced is the consent gate. `dg archive` never writes over
// anything; an import writes into a live app directory by definition, so
// the honest equivalent is a recoverable overwrite the user asked for.
func refuseUnlessForced(app string, destPaths []string, force bool) error {
	if force {
		return nil
	}
	var existing []string
	for _, p := range destPaths {
		if _, err := os.Lstat(p); err == nil {
			existing = append(existing, p)
		}
	}
	if len(existing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%s already has state at:\n  %s\n"+
			"re-run with --force to replace it — each path is renamed aside to a "+
			"%s sibling first, so the import can be undone",
		app,
		strings.Join(existing, "\n  "),
		BackupSuffix,
	)
}

// backupAll renames every destination path aside before any of them is
// written. It returns what it managed to back up even on failure, so the
// caller can roll back a partial pass.
func backupAll(destPaths []string) ([]string, error) {
	var done []string
	for _, p := range destPaths {
		if err := files.BackupPath(p, importSuffixes); err != nil {
			return done, fmt.Errorf("backing up %s: %w", p, err)
		}
		done = append(done, p)
	}
	return done, nil
}

// withRollback restores every backup and folds any restore failure into the
// returned error. A restore is a rename between siblings on one filesystem,
// so it is about as reliable as an undo gets — but if one still fails, the
// user is told which paths are sitting where rather than left to find out.
func withRollback(cause error, backedUp []string) error {
	var stuck []string
	for _, p := range backedUp {
		if err := files.RestorePath(p, importSuffixes); err != nil {
			stuck = append(stuck, p)
		}
	}
	if len(stuck) == 0 {
		return fmt.Errorf("%w\nnothing was changed: every path was restored", cause)
	}
	return fmt.Errorf(
		"%w\nand the rollback could not restore %s — the original content is in the "+
			"matching %s sibling, move it back by hand",
		cause,
		quotedList(stuck),
		BackupSuffix,
	)
}

// extractSelected writes the selected members into their profiles. Members
// outside the selection are skipped rather than refused — the bundle was
// already validated in full by inspectBundle.
func extractSelected(
	bundlePath, app string,
	roots map[string]string,
	groups []apps.StateGroup,
	profiles, selectedGroups map[string]bool,
) (int, error) {
	// The live profile may already contain a symlink, so containment is
	// checked against the root's resolved path rather than its spelling.
	realRoots := map[string]string{}
	for key := range profiles {
		real, err := filepath.EvalSymlinks(roots[key])
		if err != nil {
			return 0, fmt.Errorf("resolving %s: %w", roots[key], err)
		}
		realRoots[key] = real
	}

	reader, closeBundle, err := openBundle(bundlePath)
	if err != nil {
		return 0, err
	}
	defer closeBundle()

	count := 0
	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return count, fmt.Errorf("reading %s: %w", filepath.Base(bundlePath), err)
		}
		info, err := classifyMember(hdr.Name, hdr.Typeflag, app, groups)
		if err != nil {
			return count, err
		}
		if !profiles[info.Profile] || !selectedGroups[info.Group] {
			continue
		}

		dest := filepath.Join(roots[info.Profile], filepath.FromSlash(info.Rest))
		mode := hdr.FileInfo().Mode().Perm()
		if info.IsDir {
			if err := os.MkdirAll(dest, mode|0o700); err != nil {
				return count, err
			}
			if err := containedIn(realRoots[info.Profile], dest); err != nil {
				return count, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return count, err
		}
		if err := containedIn(realRoots[info.Profile], filepath.Dir(dest)); err != nil {
			return count, err
		}
		if err := writeMemberFile(dest, mode|0o600, tr); err != nil {
			return count, fmt.Errorf("writing %s: %w", dest, err)
		}
		count++
	}
}

// containedIn is the last of the path checks, and the only one that can see
// a symlink the destination itself already contains.
func containedIn(realRoot, p string) error {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	if real != realRoot && !strings.HasPrefix(real, realRoot+string(os.PathSeparator)) {
		return fmt.Errorf(
			"%s resolves to %s, which is outside the profile root %s",
			p,
			real,
			realRoot,
		)
	}
	return nil
}

func openBundle(bundlePath string) (io.Reader, func(), error) {
	f, err := os.Open(bundlePath)
	if err != nil {
		return nil, nil, err
	}
	dec, err := zstd.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("reading %s: %w", filepath.Base(bundlePath), err)
	}
	return dec, func() {
		dec.Close()
		_ = f.Close()
	}, nil
}

func sortedSetKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k, on := range set {
		if on {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func quotedList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, fmt.Sprintf("%q", item))
	}
	return strings.Join(quoted, ", ")
}

// ensureBundleProfiles creates and registers every profile the bundle names
// that this machine lacks, and returns the refreshed roots together with the
// registry paths it backed up first.
//
// It gives up quietly in three cases, each leaving the caller's existing
// refusal to fire: an adapter with no registry, a bundle with no registry
// sidecar beside it, and a profile the sidecar does not name. Creating a
// profile devgeta cannot name would leave the user with an unlabelled
// directory they did not ask for, which is worse than being told to narrow
// the run.
func ensureBundleProfiles(
	porter Porter,
	bundlePath string,
	targets []restoreTarget,
	roots map[string]string,
	wanted []string,
) (map[string]string, []string, []string, error) {
	registrar, ok := porter.(apps.StateProfileRegistrar)
	if !ok {
		return roots, nil, nil, nil
	}

	missing := map[string]bool{}
	for _, t := range targets {
		if _, have := roots[t.Profile]; !have {
			missing[t.Profile] = true
		}
	}
	// --profile is a narrowing flag, so it narrows creation too: a run that
	// names one profile must not quietly create the other two.
	if len(wanted) > 0 {
		asked := map[string]bool{}
		for _, name := range wanted {
			asked[name] = true
		}
		for key := range missing {
			if !asked[key] {
				delete(missing, key)
			}
		}
	}
	if len(missing) == 0 {
		return roots, nil, nil, nil
	}

	registry, err := readRegistrySidecar(bundlePath)
	if err != nil {
		return roots, nil, nil, err
	}
	want := map[string]apps.StateProfileInfo{}
	for key := range missing {
		info, named := registry[key]
		if !named {
			return roots, nil, nil, nil
		}
		want[key] = info
	}

	// The registry is written before any state is, so it is backed up before
	// any state is too — a failure anywhere below rolls the profile list back
	// with the files.
	backedUp, err := backupAll(registrar.RegistryPaths())
	if err != nil {
		return roots, nil, backedUp, err
	}
	created, err := registrar.EnsureProfiles(want)
	if err != nil {
		return roots, nil, backedUp, fmt.Errorf(
			"creating the profiles %s does not have yet: %w", porter.Name(), err,
		)
	}

	fresh, err := porter.StateRoots()
	if err != nil {
		return roots, created, backedUp, fmt.Errorf(
			"re-reading %s's profiles after creating them: %w", porter.Name(), err,
		)
	}
	return fresh, created, backedUp, nil
}

// readRegistrySidecar reads the profile names written beside the bundle. A
// missing sidecar is not an error: it is an older bundle, or one copied
// without its siblings, and the caller falls back to refusing a profile it
// cannot name.
func readRegistrySidecar(bundlePath string) (map[string]apps.StateProfileInfo, error) {
	raw, err := os.ReadFile(RegistrySidecarPath(bundlePath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading the profile registry beside the bundle: %w", err)
	}
	return DecodeRegistry(raw)
}

// intersectSorted returns the members of want that are also in have, keeping
// want's order — the profiles a --profile line could actually name.
func intersectSorted(want, have []string) []string {
	present := map[string]bool{}
	for _, h := range have {
		present[h] = true
	}
	var out []string
	for _, w := range want {
		if present[w] {
			out = append(out, w)
		}
	}
	return out
}
