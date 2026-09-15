// Phase 2 of `dg export`: hand a collected plan to the archive writer and
// verify what came out. Both the writer and the verifier are reused verbatim
// (ADR-0045), so a bundle is an ordinary .tar.zst with a `shasum -a 256 -c`-able
// manifest beside it — extractable with plain tar on a machine that has never
// had devgeta on it.
package appstate

import (
	"fmt"
	"time"

	"github.com/cjairm/devgeta/internal/tooling/archive"
)

// BundleExt is the bundle's extension. One format, not a choice: a bundle is
// read back by `dg import` on a machine that has devgeta, so the gzip escape
// hatch `dg archive` offers for Windows' built-in tar buys nothing here.
const BundleExt = ".tar.zst"

// bundleInfix separates the app name from the date in a bundle's name. It is
// what keeps a bundle apart from a `dg archive` of a folder that happens to
// be called "brave", and — with the app name in front of it — what `dg
// import` reads back to refuse a bundle written for a different app.
const bundleInfix = "-state-"

// ExportOptions carries the write phase's progress meter. The verify phase
// has its own, because it is metered against the bundle's size on disk —
// which is not known until the write has finished, so the two cannot be
// started together.
type ExportOptions struct {
	OnWriteProgress archive.ProgressFunc
}

// ExportResult names what a successful export wrote and what it moved.
type ExportResult struct {
	BundlePath     string
	ManifestPath   string
	SkipReportPath string
	// ArchiveHashPath holds the bundle file's own checksum, written by the
	// verify phase — a different file from the manifest, which lists the
	// bundle's members. It is empty after WriteBundle alone, since nothing
	// has verified anything yet.
	ArchiveHashPath string
	Plan            *Plan
}

// BundlePrefix is what every bundle written for app is named with. `dg
// import <app>` requires it, because every Chromium browser uses the same
// profile file names: a Chrome bundle passes every path rule Brave's import
// applies, so the name is the only thing standing between it and the live
// profile.
func BundlePrefix(app string) string { return app + bundleInfix }

// BundleName is the base name an export writes: "<app>-state-<YYYY-MM-DD>",
// the same shape as `dg archive`'s "<folder>-<date>".
func BundleName(app string, when time.Time) string {
	return BundlePrefix(app) + when.Format("2006-01-02")
}

// Export collects porter's selected state, writes it as one bundle in
// destDir under name, and verifies the result. It refuses while the app is
// running: this state is SQLite and LevelDB, and copied out from under a
// live process it arrives corrupt (ADR-0045).
//
// Export never overwrites, but it does not check for that either — the
// caller does, before the collection walk, so the refusal costs nothing.
// See cmd/export.go, which shares `dg archive`'s refuseIfAnyOutputExists.
func Export(
	porter Porter,
	destDir, name string,
	sel Selection,
	opts ExportOptions,
) (*ExportResult, error) {
	plan, err := Prepare(porter, sel)
	if err != nil {
		return nil, err
	}
	result, err := WriteBundle(plan, destDir, name, opts)
	if err != nil {
		return nil, err
	}
	verified, err := archive.Verify(result.BundlePath, archive.VerifyOptions{})
	if err != nil {
		return nil, fmt.Errorf("verifying %s: %w", result.BundlePath, err)
	}
	result.ArchiveHashPath = verified.ArchiveHashPath
	return result, nil
}

// Prepare is Export's first half: refuse if the app is running, then
// resolve the selection into a plan. It is separate so the command layer
// can print the plan, check free space and refuse a name collision before
// anything is written — and so `--dry-run` is the same code path minus the
// write.
func Prepare(porter Porter, sel Selection) (*Plan, error) {
	if err := refuseIfRunning(porter); err != nil {
		return nil, err
	}
	return Collect(porter, sel)
}

// WriteBundle is Export's second half: write the plan. Verifying it is the
// caller's next call, because the verify meter is sized from the bundle on
// disk (cmd/archive.go's verifyWithProgress does exactly that, and the
// command layer reuses it).
//
// It refuses a plan with nothing in it, because an empty bundle is not a
// successful export — it is a silent failure the user discovers on the new
// machine.
func WriteBundle(
	plan *Plan,
	destDir, name string,
	opts ExportOptions,
) (*ExportResult, error) {
	if plan.FileCount() == 0 {
		return nil, fmt.Errorf(
			"nothing to export: none of the selected groups has any state in the " +
				"profiles on this machine\n" +
				"run with --dry-run to see every group and what it would move",
		)
	}

	written, err := archive.Write(plan.Base, destDir, name, plan.Scan, archive.WriteOptions{
		OnProgress: opts.OnWriteProgress,
	})
	if err != nil {
		return nil, fmt.Errorf("writing the bundle: %w", err)
	}

	return &ExportResult{
		BundlePath:     written.ArchivePath,
		ManifestPath:   written.ManifestPath,
		SkipReportPath: written.SkipReportPath,
		Plan:           plan,
	}, nil
}

// refuseIfRunning is the check both directions share. A running process is
// proof rather than a guess, so this refuses instead of warning (ADR-0042).
func refuseIfRunning(porter Porter) error {
	running, err := porter.IsRunning()
	if err != nil {
		return fmt.Errorf("checking whether %s is running: %w", porter.Name(), err)
	}
	if running {
		return fmt.Errorf(
			"%s is running: quit it and re-run\n"+
				"its state is SQLite and LevelDB, which copied out from under a live "+
				"process arrives corrupt",
			porter.Name(),
		)
	}
	return nil
}
