/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cjairm/devgeta/internal/tooling/archive"
	"github.com/cjairm/devgeta/pkg/progress"
	"github.com/cjairm/devgeta/pkg/promptui"
	"github.com/cjairm/devgeta/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	archiveDryRunFlag      bool
	archiveGzipFlag        bool
	archiveNoSkipFlag      bool
	archiveNoVerifyFlag    bool
	archiveMacMetadataFlag bool
	archiveYesFlag         bool
)

// archiveGOOS, archiveNow, and archiveFreeBytes are indirections over
// runtime.GOOS, time.Now, and the destination's free space so tests can
// exercise the --mac-metadata refusal, the output filename, and the
// short-on-space warning without depending on the actual OS, wall clock, or
// the size of whatever disk the suite happens to run on.
var (
	archiveGOOS      = runtime.GOOS
	archiveNow       = time.Now
	archiveFreeBytes = archive.FreeBytes
)

// archiveProgressOut is where the write and verify meters draw. Stderr, so a
// redirected stdout stays clean; a seam so tests can run both phases without
// drawing into the test log.
var archiveProgressOut io.Writer = os.Stderr

// Padded to a common width so the write and verify bars start in the same
// column as one phase follows the other.
const (
	writeMeterLabel  = "Writing  "
	verifyMeterLabel = "Verifying"
)

var archiveCmd = &cobra.Command{
	Use:   "archive <source> <destination-dir>",
	Short: "Pack a folder into one compressed archive for a machine move",
	Long: `Writes <source>'s contents into one .tar.zst (or .tar.gz with --gzip) on
<destination-dir>, skipping only folders proven regenerable (node_modules,
virtualenvs, build caches — see ADR-0041), then verifies the archive matches
the source.

This is a one-shot archive for moving files to a new machine — not an
incremental backup.

Compress:
  dg archive ~/Documents /Volumes/SSD              # .tar.zst, verified
  dg archive ~/Documents /Volumes/SSD --dry-run    # scan and report only
  dg archive ~/Documents /Volumes/SSD --gzip --yes # .tar.gz, no prompt

Decompress — plain tar, devgeta not required. Extract into a directory of
its own: a bare "tar -xf" unpacks into the CURRENT directory, and -C fails
if the directory is not already there.
  mkdir -p NAME && tar --zstd -xf NAME.tar.zst -C NAME
  tar -xzf NAME.tar.gz -C NAME                 # a --gzip archive
  zstd -d NAME.tar.zst -c | tar -x -C NAME     # if your tar lacks --zstd

Inspect and check, also without devgeta:
  tar --zstd -tf NAME.tar.zst                  # list without extracting
  tar --zstd -xf NAME.tar.zst some/one/file    # pull out one path
  shasum -a 256 -c NAME.tar.zst.sha256         # the archive file is intact
  cd ~/restored && shasum -a 256 -c NAME.sha256  # every extracted file is intact
`,
	Args: cobra.ExactArgs(2),
	RunE: runArchive,
}

var archiveVerifyCmd = &cobra.Command{
	Use:   "verify <archive-file>",
	Short: "Re-check an archive against its manifest",
	Args:  cobra.ExactArgs(1),
	RunE:  runArchiveVerify,
}

func init() {
	archiveCmd.Flags().
		BoolVar(&archiveDryRunFlag, "dry-run", false, "Scan and report without writing anything")
	archiveCmd.Flags().
		BoolVar(&archiveGzipFlag, "gzip", false, "Write a .tar.gz instead of the default .tar.zst")
	archiveCmd.Flags().
		BoolVar(&archiveNoSkipFlag, "no-skip", false, "Disable every skip rule; archive everything")
	archiveCmd.Flags().
		BoolVar(&archiveNoVerifyFlag, "no-verify", false, "Skip the post-write verification pass")
	archiveCmd.Flags().BoolVar(
		&archiveMacMetadataFlag,
		"mac-metadata",
		false,
		"Include extended attributes as pax records (macOS only)",
	)
	archiveCmd.Flags().
		BoolVar(&archiveYesFlag, "yes", false, "Skip the confirmation prompt")
	archiveCmd.AddCommand(archiveVerifyCmd)
	rootCmd.AddCommand(archiveCmd)
}

func runArchive(_ *cobra.Command, args []string) error {
	source := args[0]
	destDir := args[1]

	if archiveMacMetadataFlag && archiveGOOS != "darwin" {
		return fmt.Errorf("--mac-metadata is only supported on macOS")
	}

	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("source %s: %w", source, err)
	}
	if !sourceInfo.IsDir() {
		return fmt.Errorf("source %s is not a directory", source)
	}

	destInfo, err := os.Stat(destDir)
	if err != nil {
		return fmt.Errorf("destination %s: %w", destDir, err)
	}
	if !destInfo.IsDir() {
		return fmt.Errorf("destination %s is not a directory", destDir)
	}

	inside, err := isSubPath(source, destDir)
	if err != nil {
		return err
	}
	if inside {
		return fmt.Errorf(
			"destination %s is inside source %s: the archive would archive itself",
			destDir,
			source,
		)
	}

	ext := ".tar.zst"
	if archiveGzipFlag {
		ext = ".tar.gz"
	}
	name := fmt.Sprintf(
		"%s-%s",
		filepath.Base(filepath.Clean(source)),
		archiveNow().Format("2006-01-02"),
	)
	archivePath := filepath.Join(destDir, name+ext)
	if err := refuseIfAnyOutputExists(destDir, name, ext); err != nil {
		return err
	}

	if isFAT, ferr := archive.IsFATFilesystem(destDir); ferr == nil && isFAT {
		return fmt.Errorf(
			"%s is on a FAT filesystem (4 GiB file-size limit): reformat the drive as exFAT",
			destDir,
		)
	}

	utils.PrintInfo(fmt.Sprintf("Scanning %s...", source))
	scanResult, err := archive.Scan(source, archive.ScanOptions{
		NoSkip:          archiveNoSkipFlag,
		ComputeSkipSize: archiveDryRunFlag,
	})
	if err != nil {
		return fmt.Errorf("scanning %s: %w", source, err)
	}
	printScanReport(scanResult)

	if len(scanResult.Unreadable) > 0 || len(scanResult.ICloudOnly) > 0 {
		return refusalForUnreadableOrICloud(scanResult)
	}

	warnIfShortOnSpace(destDir, scanResult.TotalBytes)

	if archiveDryRunFlag {
		utils.PrintInfo("Dry run: nothing written.")
		return nil
	}

	if !archiveYesFlag {
		if !isInteractiveTerminal() {
			return fmt.Errorf("refusing to write without --yes in a non-interactive session")
		}
		if err := promptui.DisplayInstructions(
			"Proceed",
			fmt.Sprintf("About to write %s to %s", name+ext, destDir),
			true,
		); err != nil {
			return fmt.Errorf("aborted: %w", err)
		}
	}

	utils.PrintInfo(fmt.Sprintf("Writing %s...", archivePath))
	writeMeter := progress.New(archiveProgressOut, writeMeterLabel, scanResult.TotalBytes)
	writeMeter.Start()
	result, err := archive.Write(
		source,
		destDir,
		name,
		scanResult,
		archive.WriteOptions{
			Gzip:        archiveGzipFlag,
			MacMetadata: archiveMacMetadataFlag,
			OnProgress:  writeMeter.Add,
		},
	)
	writeMeter.Stop()
	if err != nil {
		return fmt.Errorf("writing archive: %w", err)
	}
	utils.PrintSuccess(fmt.Sprintf("wrote %s", result.ArchivePath))
	utils.PrintSuccess(fmt.Sprintf("wrote %s", result.ManifestPath))
	utils.PrintSuccess(fmt.Sprintf("wrote %s", result.SkipReportPath))
	if len(result.Changed) > 0 {
		utils.PrintWarning(fmt.Sprintf(
			"%d file(s) changed or vanished during the write:",
			len(result.Changed),
		))
		for _, p := range result.Changed {
			utils.PrintWarning("  " + p)
		}
	}

	if !archiveNoVerifyFlag {
		utils.PrintInfo("Verifying archive...")
		if err := verifyWithProgress(result.ArchivePath); err != nil {
			return fmt.Errorf("verification failed: %w", err)
		}
	}

	printRestoreHint(result.ArchivePath)
	return nil
}

// printRestoreHint prints the extract command for the archive just written.
// The machine that needs it is the new one, where devgeta may not be
// installed and this output is the only place the command appears.
func printRestoreHint(archivePath string) {
	utils.PrintSecondary("Restore with:  " + restoreCommand(filepath.Base(archivePath)))
}

// restoreCommand returns the plain-tar extract command for filename.
//
// It always names a destination directory and creates it first. A bare
// `tar -xf` unpacks into the current directory, which for an archive of a
// whole home folder means hundreds of entries detonating over whatever the
// user happened to be standing in — and `-C` alone fails if the directory
// does not already exist. Handing over a command that is safe to paste
// anywhere costs a few more characters and removes both traps.
func restoreCommand(filename string) string {
	dir := strings.TrimSuffix(strings.TrimSuffix(filename, ".tar.gz"), ".tar.zst")
	extract := "tar --zstd -xf "
	if strings.HasSuffix(filename, ".tar.gz") {
		extract = "tar -xzf "
	}
	return fmt.Sprintf("mkdir -p %s && %s%s -C %s", dir, extract, filename, dir)
}

func runArchiveVerify(_ *cobra.Command, args []string) error {
	return verifyWithProgress(args[0])
}

// verifyWithProgress re-reads the archive against its manifest, metered
// against the archive's own size on disk — this pass reads the compressed
// bytes back off the drive, not the source.
func verifyWithProgress(archivePath string) error {
	var total int64
	if info, err := os.Stat(archivePath); err == nil {
		total = info.Size()
	}
	meter := progress.New(archiveProgressOut, verifyMeterLabel, total)
	meter.Start()
	vr, err := archive.Verify(archivePath, archive.VerifyOptions{OnProgress: meter.Add})
	meter.Stop()
	if err != nil {
		return err
	}
	utils.PrintSuccess(fmt.Sprintf("verified: %s", vr.ArchiveHash))
	return nil
}

// archiveOutputPaths lists every file a full run writes into destDir. The
// three the write phase renames into place, plus the archive's own checksum
// written by the verify phase.
func archiveOutputPaths(destDir, name, ext string) []string {
	archivePath := filepath.Join(destDir, name+ext)
	return []string{
		archivePath,
		filepath.Join(destDir, name+".sha256"),
		filepath.Join(destDir, name+".skipped.txt"),
		archivePath + ".sha256",
	}
}

// refuseIfAnyOutputExists aborts before anything is created if a run would
// land on top of a file already on the drive. Checking every output rather
// than just the archive matters because the write phase renames its
// .partial files into place unconditionally: a leftover manifest or skip
// report from an earlier run would otherwise be replaced without a word.
//
// `dg export` shares this check, since it writes the same four files
// through the same writer — which is why the refusal names devgeta's rule
// rather than one of the two commands.
func refuseIfAnyOutputExists(destDir, name, ext string) error {
	var existing []string
	for _, path := range archiveOutputPaths(destDir, name, ext) {
		if _, err := os.Stat(path); err == nil {
			existing = append(existing, filepath.Base(path))
		}
	}
	if len(existing) == 0 {
		return nil
	}
	// Listed by name under one directory line: four absolute paths run
	// together on one line are unreadable, and the shared prefix is noise.
	noun := "file"
	if len(existing) > 1 {
		noun = "files"
	}
	return fmt.Errorf(
		"%s already has %d %s from an earlier run:\n  %s\n"+
			"devgeta never overwrites on the destination — remove them, or write to a different destination",
		destDir,
		len(existing),
		noun,
		strings.Join(existing, "\n  "),
	)
}

// warnIfShortOnSpace reports the destination's free space, and flags the case
// where it is smaller than the source. This warns rather than refuses: the
// archive is compressed, and by how much is not knowable until it is written,
// so a refusal keyed on the uncompressed total would block runs that fit
// comfortably. Running out mid-write is already safe — every output is a
// .partial until the end, and a failure deletes them.
func warnIfShortOnSpace(destDir string, sourceBytes int64) {
	free, err := archiveFreeBytes(destDir)
	if err != nil {
		return
	}
	utils.PrintInfo(fmt.Sprintf("%s free on %s", progress.FormatBytes(free), destDir))
	if warning := shortOnSpaceWarning(free, sourceBytes); warning != "" {
		utils.PrintWarning(warning)
	}
}

// shortOnSpaceWarning returns the warning for a destination smaller than the
// source, or "" when there is room. Kept separate from the printing so the
// threshold is testable without a disk of a particular size.
func shortOnSpaceWarning(free, sourceBytes int64) string {
	if free >= sourceBytes {
		return ""
	}
	return fmt.Sprintf(
		"the source is %s uncompressed and only %s is free; compression usually covers the gap, "+
			"but if it does not, the run is discarded and nothing already on the drive is touched",
		progress.FormatBytes(sourceBytes),
		progress.FormatBytes(free),
	)
}

// isSubPath reports whether child is parent itself or nested inside it.
func isSubPath(parent, child string) (bool, error) {
	parentAbs, err := filepath.Abs(parent)
	if err != nil {
		return false, err
	}
	childAbs, err := filepath.Abs(child)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(parentAbs, childAbs)
	if err != nil {
		return false, err
	}
	return rel == "." ||
		(rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))), nil
}

func printScanReport(scan *archive.ScanResult) {
	kept := 0
	for _, e := range scan.Entries {
		if e.Kind == archive.KindFile {
			kept++
		}
	}
	utils.PrintInfo(
		fmt.Sprintf("%d file(s) to archive, %s", kept, progress.FormatBytes(scan.TotalBytes)),
	)
	for _, s := range scan.Skipped {
		if s.Size > 0 {
			utils.PrintSecondary(
				fmt.Sprintf("  skip %s (%s, %s)", s.Path, s.Rule, progress.FormatBytes(s.Size)),
			)
		} else {
			utils.PrintSecondary(fmt.Sprintf("  skip %s (%s)", s.Path, s.Rule))
		}
	}
	for _, p := range scan.Special {
		utils.PrintWarning("  special file, skipped: " + p)
	}
	for _, issue := range scan.WindowsIncompatible {
		utils.PrintWarning(
			fmt.Sprintf("  %s will not extract on Windows: %s", issue.Path, issue.Reason),
		)
	}
	for _, p := range scan.Unreadable {
		utils.PrintWarning("  unreadable: " + p)
	}
	for _, p := range scan.ICloudOnly {
		utils.PrintWarning("  iCloud-only (not downloaded): " + p)
	}
}

func refusalForUnreadableOrICloud(scan *archive.ScanResult) error {
	var sb strings.Builder
	if len(scan.Unreadable) > 0 {
		fmt.Fprintf(
			&sb,
			"%d unreadable file(s); fix permissions and re-run.\n",
			len(scan.Unreadable),
		)
	}
	if len(scan.ICloudOnly) > 0 {
		fmt.Fprintf(
			&sb,
			"%d iCloud-only file(s) not downloaded; use Finder's \"Download Now\" and re-run.\n",
			len(scan.ICloudOnly),
		)
	}
	return fmt.Errorf("%s", strings.TrimSuffix(sb.String(), "\n"))
}
