// Phase 3 of `dg archive`, and the standalone `dg archive verify`: re-read an
// archive from the destination, decompress it, and hash every regular-file
// entry against its manifest — the same set of paths, the same hashes
// (cycle doc 2026-09-13-dg-archive.md §5). On success it also writes
// <archive>.sha256 holding the hash of the archive file itself.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// VerifyOptions controls the verify phase's reporting.
type VerifyOptions struct {
	// OnProgress, when non-nil, is called with the number of archive bytes
	// read since the last call. Summed, it converges on the archive file's
	// size on disk — the compressed size, not the source total.
	OnProgress ProgressFunc
}

// VerifyResult is the outcome of a successful Verify.
type VerifyResult struct {
	// ArchiveHash is the hex SHA-256 of the archive file's own bytes.
	ArchiveHash string
	// ArchiveHashPath is where ArchiveHash was written.
	ArchiveHashPath string
}

// Verify re-reads archivePath against its sibling manifest (the same base
// name with .sha256 in place of .tar.zst/.tar.gz) and returns an error
// naming the first mismatch found: a corrupted entry, an entry the manifest
// lists that the archive lacks, or an archive entry the manifest does not
// list.
func Verify(archivePath string, opts VerifyOptions) (*VerifyResult, error) {
	manifestPath, err := manifestPathFor(archivePath)
	if err != nil {
		return nil, err
	}

	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("opening manifest %s: %w", manifestPath, err)
	}
	defer func() { _ = manifestFile.Close() }()
	manifest, err := ReadManifest(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", manifestPath, err)
	}
	wantHashes := make(map[string]string, len(manifest))
	for _, e := range manifest {
		wantHashes[e.Path] = e.Hash
	}

	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = archiveFile.Close() }()

	// Count at the file, before decompression, so progress is measured
	// against the archive's size on disk — a total the caller can stat.
	hasher := sha256.New()
	tee := io.TeeReader(countReads(archiveFile, opts.OnProgress), hasher)

	decompressed, closeDecoder, err := decompressReader(tee, archivePath)
	if err != nil {
		return nil, err
	}
	defer closeDecoder()

	seen := make(map[string]bool, len(manifest))
	tr := tar.NewReader(decompressed)
	for {
		hdr, terr := tr.Next()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			return nil, fmt.Errorf("reading archive %s: %w", archivePath, terr)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		wantHash, ok := wantHashes[hdr.Name]
		if !ok {
			return nil, fmt.Errorf(
				"%s: %q is in the archive but not in the manifest",
				archivePath,
				hdr.Name,
			)
		}
		seen[hdr.Name] = true

		h := sha256.New()
		if _, cerr := io.Copy(h, tr); cerr != nil {
			return nil, fmt.Errorf("reading %q from %s: %w", hdr.Name, archivePath, cerr)
		}
		if gotHash := fmt.Sprintf("%x", h.Sum(nil)); gotHash != wantHash {
			return nil, fmt.Errorf(
				"%s: %q does not match the manifest (hash mismatch)",
				archivePath,
				hdr.Name,
			)
		}
	}

	for _, e := range manifest {
		if !seen[e.Path] {
			return nil, fmt.Errorf(
				"%s: manifest lists %q, which is missing from the archive",
				archivePath,
				e.Path,
			)
		}
	}

	// The tar reader may stop consuming the decompressed stream before its
	// trailing padding; drain whatever is left of the raw file through tee
	// so ArchiveHash covers every byte, matching a plain re-read of the file.
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return nil, fmt.Errorf("hashing %s: %w", archivePath, err)
	}

	archiveHash := fmt.Sprintf("%x", hasher.Sum(nil))
	hashPath := archivePath + ".sha256"
	if err := os.WriteFile(
		hashPath,
		[]byte(fmt.Sprintf("%s  %s\n", archiveHash, filepath.Base(archivePath))),
		0o644,
	); err != nil {
		return nil, err
	}

	return &VerifyResult{ArchiveHash: archiveHash, ArchiveHashPath: hashPath}, nil
}

// manifestPathFor derives the sibling manifest path from an archive path:
// the same base name with .sha256 in place of the compression extension.
func manifestPathFor(archivePath string) (string, error) {
	for _, ext := range []string{".tar.zst", ".tar.gz"} {
		if strings.HasSuffix(archivePath, ext) {
			return strings.TrimSuffix(archivePath, ext) + ".sha256", nil
		}
	}
	return "", fmt.Errorf(
		"%s: unrecognized archive extension (expected .tar.zst or .tar.gz)",
		archivePath,
	)
}

// decompressReader wraps r with the decompressor matching archivePath's
// extension. The returned close func must be called once the caller is done
// reading.
func decompressReader(r io.Reader, archivePath string) (io.Reader, func(), error) {
	if strings.HasSuffix(archivePath, ".tar.gz") {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		return gz, func() { _ = gz.Close() }, nil
	}
	dec, err := zstd.NewReader(r)
	if err != nil {
		return nil, nil, err
	}
	return dec, dec.Close, nil
}
