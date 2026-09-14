/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cjairm/devgeta/internal/tooling/archive"
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

// archiveGOOS and archiveNow are indirections over runtime.GOOS and time.Now
// so tests can exercise the --mac-metadata refusal and the output filename
// without depending on the actual OS or wall clock.
var (
	archiveGOOS = runtime.GOOS
	archiveNow  = time.Now
)

var archiveCmd = &cobra.Command{
	Use:   "archive <source> <destination-dir>",
	Short: "Pack a folder into one compressed archive for a machine move",
	Long: `Writes <source>'s contents into one .tar.zst (or .tar.gz with --gzip) on
<destination-dir>, skipping only folders proven regenerable (node_modules,
virtualenvs, build caches — see ADR-0041), then verifies the archive matches
the source.

This is a one-shot archive for moving files to a new machine — not an
incremental backup. Restore with a plain tar:

  tar --zstd -xf archive.tar.zst   # or: zstd -d archive.tar.zst -c | tar -x
  tar -xzf archive.tar.gz          # with --gzip

Examples:
  dg archive ~/Documents /Volumes/SSD
  dg archive ~/Documents /Volumes/SSD --dry-run
  dg archive ~/Documents /Volumes/SSD --gzip --yes
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
	if _, err := os.Stat(archivePath); err == nil {
		return fmt.Errorf(
			"%s already exists: never overwritten, remove it or archive to a different destination",
			archivePath,
		)
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
	result, err := archive.Write(
		source,
		destDir,
		name,
		scanResult,
		archive.WriteOptions{Gzip: archiveGzipFlag, MacMetadata: archiveMacMetadataFlag},
	)
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
		vr, verr := archive.Verify(result.ArchivePath)
		if verr != nil {
			return fmt.Errorf("verification failed: %w", verr)
		}
		utils.PrintSuccess(fmt.Sprintf("verified: %s", vr.ArchiveHash))
	}

	return nil
}

func runArchiveVerify(_ *cobra.Command, args []string) error {
	vr, err := archive.Verify(args[0])
	if err != nil {
		return err
	}
	utils.PrintSuccess(fmt.Sprintf("verified: %s", vr.ArchiveHash))
	return nil
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
	utils.PrintInfo(fmt.Sprintf("%d file(s) to archive, %d bytes", kept, scan.TotalBytes))
	for _, s := range scan.Skipped {
		if s.Size > 0 {
			utils.PrintSecondary(fmt.Sprintf("  skip %s (%s, %d bytes)", s.Path, s.Rule, s.Size))
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
