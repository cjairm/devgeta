// Phase 2 of `dg archive`: stream a Scan's entries into one compressed tar
// file, hashing each file's bytes as they are copied so the checksum
// manifest costs no extra read of the source (ADR-0040). Outputs are written
// to .partial files and only renamed into place once everything, including
// the manifest and skip report, has synced successfully — a failure at any
// point deletes every .partial file it created (cycle doc
// 2026-09-13-dg-archive.md §5).
package archive

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/klauspost/compress/zstd"
)

// WriteOptions controls the write phase's compression and metadata.
type WriteOptions struct {
	// Gzip writes a .tar.gz instead of the default .tar.zst, for zero-install
	// restores on Windows' command-line tar.
	Gzip bool
	// MacMetadata includes each entry's extended attributes as
	// SCHILY.xattr.<name> pax records instead of leaving them out. Darwin
	// only — the command layer refuses this flag on any other OS.
	MacMetadata bool
}

// WriteResult names the three files Write produced.
type WriteResult struct {
	ArchivePath    string
	ManifestPath   string
	SkipReportPath string
	Manifest       []ManifestEntry
	// Changed lists files whose size or mtime changed between scan and
	// write, or that disappeared entirely — the manifest reflects only what
	// was actually archived.
	Changed []string
}

// syncWriteCloser is the subset of *os.File that Write needs: a normal
// factory returns *os.File directly, and tests substitute a writer that
// fails partway through to exercise the cleanup-on-failure path against a
// real file on disk.
type syncWriteCloser interface {
	io.Writer
	io.Closer
	Sync() error
}

type archiveWriterFactory func(path string) (syncWriteCloser, error)

func createFile(path string) (syncWriteCloser, error) {
	return os.Create(path)
}

// Write streams scan.Entries into destDir/name.tar.zst (or .tar.gz with
// opts.Gzip), plus a sha256 manifest and a skip report, and atomically
// renames all three into place on success.
func Write(
	sourceRoot, destDir, name string,
	scan *ScanResult,
	opts WriteOptions,
) (*WriteResult, error) {
	return write(sourceRoot, destDir, name, scan, opts, createFile)
}

func write(
	sourceRoot, destDir, name string,
	scan *ScanResult,
	opts WriteOptions,
	newWriter archiveWriterFactory,
) (result *WriteResult, err error) {
	ext := ".tar.zst"
	if opts.Gzip {
		ext = ".tar.gz"
	}
	archivePath := filepath.Join(destDir, name+ext)
	manifestPath := filepath.Join(destDir, name+".sha256")
	skipReportPath := filepath.Join(destDir, name+".skipped.txt")
	partials := []string{
		archivePath + ".partial",
		manifestPath + ".partial",
		skipReportPath + ".partial",
	}

	defer func() {
		if err != nil {
			for _, p := range partials {
				_ = os.Remove(p)
			}
		}
	}()

	archiveFile, ferr := newWriter(partials[0])
	if ferr != nil {
		return nil, ferr
	}
	manifest, changed, streamErr := streamArchive(archiveFile, sourceRoot, scan.Entries, opts)
	if streamErr != nil {
		_ = archiveFile.Close()
		return nil, streamErr
	}
	if serr := archiveFile.Sync(); serr != nil {
		_ = archiveFile.Close()
		return nil, serr
	}
	if cerr := archiveFile.Close(); cerr != nil {
		return nil, cerr
	}

	if werr := writeManifestFile(partials[1], manifest); werr != nil {
		return nil, werr
	}
	if werr := writeSkipReportFile(partials[2], scan.Skipped); werr != nil {
		return nil, werr
	}

	finals := []string{archivePath, manifestPath, skipReportPath}
	for i, partial := range partials {
		if rerr := os.Rename(partial, finals[i]); rerr != nil {
			return nil, rerr
		}
	}

	return &WriteResult{
		ArchivePath:    archivePath,
		ManifestPath:   manifestPath,
		SkipReportPath: skipReportPath,
		Manifest:       manifest,
		Changed:        changed,
	}, nil
}

// streamArchive writes entries as a tar stream, compressed, into w. A file
// that disappeared since it was scanned is recorded in changed and skipped;
// any other read or write failure is fatal and returned as err.
func streamArchive(
	w io.Writer,
	sourceRoot string,
	entries []Entry,
	opts WriteOptions,
) (manifest []ManifestEntry, changed []string, err error) {
	var compWriter io.WriteCloser
	if opts.Gzip {
		compWriter = gzip.NewWriter(w)
	} else {
		enc, zerr := zstd.NewWriter(
			w,
			zstd.WithEncoderLevel(zstd.SpeedDefault),
			zstd.WithEncoderConcurrency(runtime.GOMAXPROCS(0)),
		)
		if zerr != nil {
			return nil, nil, zerr
		}
		compWriter = enc
	}
	tw := tar.NewWriter(compWriter)

	for _, e := range entries {
		switch e.Kind {
		case KindDir:
			hdr := &tar.Header{
				Name:     e.Path + "/",
				Typeflag: tar.TypeDir,
				Mode:     int64(e.Mode.Perm()),
				ModTime:  e.ModTime.Truncate(time.Second),
			}
			if opts.MacMetadata {
				attachXattrs(hdr, filepath.Join(sourceRoot, filepath.FromSlash(e.Path)))
			}
			if werr := tw.WriteHeader(hdr); werr != nil {
				return nil, nil, werr
			}
		case KindSymlink:
			hdr := &tar.Header{
				Name:     e.Path,
				Typeflag: tar.TypeSymlink,
				Linkname: e.LinkTarget,
				Mode:     int64(e.Mode.Perm()),
				ModTime:  e.ModTime.Truncate(time.Second),
			}
			if opts.MacMetadata {
				attachXattrs(hdr, filepath.Join(sourceRoot, filepath.FromSlash(e.Path)))
			}
			if werr := tw.WriteHeader(hdr); werr != nil {
				return nil, nil, werr
			}
		case KindHardLink:
			hdr := &tar.Header{
				Name:     e.Path,
				Typeflag: tar.TypeLink,
				Linkname: e.LinkTarget,
				Mode:     int64(e.Mode.Perm()),
				ModTime:  e.ModTime.Truncate(time.Second),
			}
			if opts.MacMetadata {
				attachXattrs(hdr, filepath.Join(sourceRoot, filepath.FromSlash(e.Path)))
			}
			if werr := tw.WriteHeader(hdr); werr != nil {
				return nil, nil, werr
			}
		case KindFile:
			entryManifest, vanished, writeErr := writeFileEntry(tw, sourceRoot, e, opts.MacMetadata)
			if writeErr != nil {
				return nil, nil, writeErr
			}
			if vanished {
				changed = append(changed, e.Path)
				continue
			}
			manifest = append(manifest, entryManifest)
		}
	}

	if cerr := tw.Close(); cerr != nil {
		return nil, nil, cerr
	}
	if cerr := compWriter.Close(); cerr != nil {
		return nil, nil, cerr
	}
	return manifest, changed, nil
}

// writeFileEntry archives one regular file. A file that no longer exists is
// reported via vanished=true, err=nil: it is warned about, not fatal. Any
// other failure (permission revoked, read error) is fatal.
func writeFileEntry(
	tw *tar.Writer,
	sourceRoot string,
	e Entry,
	macMetadata bool,
) (entry ManifestEntry, vanished bool, err error) {
	fullPath := filepath.Join(sourceRoot, filepath.FromSlash(e.Path))
	f, openErr := os.Open(fullPath)
	if openErr != nil {
		if os.IsNotExist(openErr) {
			return ManifestEntry{}, true, nil
		}
		return ManifestEntry{}, false, fmt.Errorf("reading %s: %w", e.Path, openErr)
	}
	defer func() { _ = f.Close() }()

	info, statErr := f.Stat()
	if statErr != nil {
		return ManifestEntry{}, false, fmt.Errorf("stat %s: %w", e.Path, statErr)
	}

	hdr := &tar.Header{
		Name:     e.Path,
		Typeflag: tar.TypeReg,
		Size:     info.Size(),
		Mode:     int64(info.Mode().Perm()),
		ModTime:  info.ModTime().Truncate(time.Second),
	}
	if macMetadata {
		attachXattrs(hdr, fullPath)
	}
	if werr := tw.WriteHeader(hdr); werr != nil {
		return ManifestEntry{}, false, werr
	}

	h := sha256.New()
	if _, cerr := io.Copy(tw, io.TeeReader(f, h)); cerr != nil {
		return ManifestEntry{}, false, fmt.Errorf("reading %s: %w", e.Path, cerr)
	}
	return ManifestEntry{Hash: fmt.Sprintf("%x", h.Sum(nil)), Path: e.Path}, false, nil
}

// attachXattrs adds every extended attribute set on fullPath to hdr as a
// SCHILY.xattr.<name> pax record — read by GNU tar and libarchive/bsdtar,
// unlike the AppleDouble "._" files a shell-out to macOS tar would write
// (ADR-0040). A read failure for the attribute list or any single value is
// not fatal to the archive: that attribute is simply left out.
func attachXattrs(hdr *tar.Header, fullPath string) {
	names, err := listXattrs(fullPath)
	if err != nil {
		return
	}
	for _, name := range names {
		value, verr := getXattr(fullPath, name)
		if verr != nil {
			continue
		}
		if hdr.PAXRecords == nil {
			hdr.PAXRecords = map[string]string{}
		}
		hdr.PAXRecords["SCHILY.xattr."+name] = string(value)
	}
}

func writeManifestFile(path string, entries []ManifestEntry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if werr := WriteManifest(f, entries); werr != nil {
		_ = f.Close()
		return werr
	}
	if serr := f.Sync(); serr != nil {
		_ = f.Close()
		return serr
	}
	return f.Close()
}

func writeSkipReportFile(path string, skipped []SkippedEntry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	for _, s := range skipped {
		if _, werr := fmt.Fprintf(f, "%s\t%s\t%d\n", s.Path, s.Rule, s.Size); werr != nil {
			_ = f.Close()
			return werr
		}
	}
	if serr := f.Sync(); serr != nil {
		_ = f.Close()
		return serr
	}
	return f.Close()
}
