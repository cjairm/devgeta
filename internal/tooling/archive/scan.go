// Phase 1 of `dg archive`: walk the source tree once, apply skip rules, and
// produce the deterministic entry list, byte total, and report that the
// write phase and --dry-run both consume (cycle doc 2026-09-13-dg-archive.md
// §5). Scan never reads file contents.
package archive

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// EntryKind classifies a kept archive entry.
type EntryKind int

const (
	KindFile EntryKind = iota
	KindDir
	KindSymlink
	KindHardLink
)

// Entry is one file, directory, symlink, or additional hard-link occurrence
// that will be written to the archive.
type Entry struct {
	Path       string // relative to the scan root, forward-slash separated
	Kind       EntryKind
	Size       int64 // 0 for directories, symlinks, and hard-link occurrences
	Mode       os.FileMode
	ModTime    time.Time
	LinkTarget string // symlink target, or the first entry's Path for a hard link
}

// SkippedEntry is a directory or file a skip rule matched.
type SkippedEntry struct {
	Path string
	Rule string
	Size int64 // only populated when ScanOptions.ComputeSkipSize is set
}

// WindowsIssue names a kept entry whose name cannot be created on Windows.
// The entry is still archived unchanged — renaming would alter the user's
// files (cycle doc 2026-09-13-dg-archive.md §5).
type WindowsIssue struct {
	Path   string
	Reason string
}

// ScanOptions controls Scan's skip-rule and reporting behavior.
type ScanOptions struct {
	// NoSkip disables every skip rule: everything under root is archived.
	NoSkip bool
	// ComputeSkipSize walks each skipped tree to report its size. Only worth
	// the extra I/O in --dry-run, since it costs a walk of exactly what a
	// real run avoids.
	ComputeSkipSize bool
}

// ScanResult is the outcome of walking root once.
type ScanResult struct {
	Entries             []Entry
	TotalBytes          int64
	Skipped             []SkippedEntry
	Unreadable          []string
	Special             []string
	ICloudOnly          []string
	WindowsIncompatible []WindowsIssue
}

// Scan walks root and returns its kept entries and report. It never reads
// file contents, so it is fast even on large trees.
func Scan(root string, opts ScanOptions) (*ScanResult, error) {
	result := &ScanResult{}
	seen := map[[2]uint64]string{}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if path == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if err != nil {
			result.Unreadable = append(result.Unreadable, rel)
			return nil
		}

		if !opts.NoSkip {
			if rule, skip := Match(path); skip {
				size := int64(0)
				if opts.ComputeSkipSize {
					size = treeSize(path)
				}
				result.Skipped = append(
					result.Skipped,
					SkippedEntry{Path: rel, Rule: rule, Size: size},
				)
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}

		info, statErr := d.Info()
		if statErr != nil {
			result.Unreadable = append(result.Unreadable, rel)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if reason := WindowsIncompatibleReason(d.Name()); reason != "" {
			result.WindowsIncompatible = append(
				result.WindowsIncompatible,
				WindowsIssue{Path: rel, Reason: reason},
			)
		}

		mode := info.Mode()
		switch {
		case d.IsDir():
			result.Entries = append(
				result.Entries,
				Entry{Path: rel, Kind: KindDir, Mode: mode, ModTime: info.ModTime()},
			)
		case mode&os.ModeSymlink != 0:
			target, readErr := os.Readlink(path)
			if readErr != nil {
				result.Unreadable = append(result.Unreadable, rel)
				return nil
			}
			result.Entries = append(
				result.Entries,
				Entry{
					Path:       rel,
					Kind:       KindSymlink,
					Mode:       mode,
					ModTime:    info.ModTime(),
					LinkTarget: target,
				},
			)
		case mode&(os.ModeSocket|os.ModeNamedPipe|os.ModeDevice|os.ModeCharDevice) != 0:
			result.Special = append(result.Special, rel)
		case mode.IsRegular():
			if accessErr := unix.Access(path, unix.R_OK); accessErr != nil {
				result.Unreadable = append(result.Unreadable, rel)
				return nil
			}
			dataless, _ := isDataless(path)
			if dataless {
				result.ICloudOnly = append(result.ICloudOnly, rel)
				return nil
			}
			entry := Entry{
				Path:    rel,
				Kind:    KindFile,
				Size:    info.Size(),
				Mode:    mode,
				ModTime: info.ModTime(),
			}
			if dev, ino, nlink, identErr := statIdentity(path); identErr == nil && nlink > 1 {
				key := [2]uint64{dev, ino}
				if firstPath, ok := seen[key]; ok {
					entry.Kind = KindHardLink
					entry.LinkTarget = firstPath
					entry.Size = 0
				} else {
					seen[key] = rel
				}
			}
			result.Entries = append(result.Entries, entry)
			if entry.Kind == KindFile {
				result.TotalBytes += entry.Size
			}
		default:
			result.Special = append(result.Special, rel)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return result, nil
}

// treeSize sums the size of every regular file under path — a directory's
// own size if path is a directory, or path's own size if it's a file.
func treeSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, infoErr := d.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
