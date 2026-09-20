package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// buildFixture creates: content.txt, sub/file.txt, link.txt -> content.txt (symlink),
// hardlink.txt -> content.txt (hard link).
func buildFixture(t *testing.T) (root string, rootContent, subContent []byte) {
	t.Helper()
	root = t.TempDir()
	rootContent = []byte("hello world")
	subContent = []byte("nested file content")

	writeFile(t, filepath.Join(root, "content.txt"), rootContent)
	writeFile(t, filepath.Join(root, "sub", "file.txt"), subContent)
	if err := os.Symlink("content.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if err := os.Link(
		filepath.Join(root, "content.txt"),
		filepath.Join(root, "hardlink.txt"),
	); err != nil {
		t.Fatalf("Link: %v", err)
	}
	return root, rootContent, subContent
}

type tarEntry struct {
	header  *tar.Header
	content []byte
}

func readBackTar(t *testing.T, archivePath string, gzipCompressed bool) map[string]tarEntry {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("Open(%q): %v", archivePath, err)
	}
	defer func() { _ = f.Close() }()

	var r io.Reader
	if gzipCompressed {
		gz, gerr := gzip.NewReader(f)
		if gerr != nil {
			t.Fatalf("gzip.NewReader: %v", gerr)
		}
		defer func() { _ = gz.Close() }()
		r = gz
	} else {
		dec, derr := zstd.NewReader(f)
		if derr != nil {
			t.Fatalf("zstd.NewReader: %v", derr)
		}
		defer dec.Close()
		r = dec
	}

	entries := map[string]tarEntry{}
	tr := tar.NewReader(r)
	for {
		hdr, terr := tr.Next()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			t.Fatalf("tar Next: %v", terr)
		}
		var content bytes.Buffer
		if hdr.Typeflag == tar.TypeReg {
			if _, cerr := io.Copy(&content, tr); cerr != nil {
				t.Fatalf("reading tar content for %q: %v", hdr.Name, cerr)
			}
		}
		entries[hdr.Name] = tarEntry{header: hdr, content: content.Bytes()}
	}
	return entries
}

func TestWriteRoundTripsFixtureWithZstd(t *testing.T) {
	root, rootContent, subContent := buildFixture(t)
	destDir := t.TempDir()

	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	result, err := Write(root, destDir, "myarchive", scan, WriteOptions{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if filepath.Ext(result.ArchivePath) != ".zst" {
		t.Errorf("ArchivePath = %q, want a .tar.zst file", result.ArchivePath)
	}

	entries := readBackTar(t, result.ArchivePath, false)

	rootEntry, ok := entries["content.txt"]
	if !ok {
		t.Fatal("expected content.txt in archive")
	}
	if !bytes.Equal(rootEntry.content, rootContent) {
		t.Errorf("content.txt content = %q, want %q", rootEntry.content, rootContent)
	}
	if rootEntry.header.Typeflag != tar.TypeReg {
		t.Errorf("content.txt Typeflag = %v, want TypeReg", rootEntry.header.Typeflag)
	}

	subEntry, ok := entries["sub/file.txt"]
	if !ok {
		t.Fatal("expected sub/file.txt in archive")
	}
	if !bytes.Equal(subEntry.content, subContent) {
		t.Errorf("sub/file.txt content = %q, want %q", subEntry.content, subContent)
	}

	dirEntry, ok := entries["sub/"]
	if !ok {
		t.Fatal("expected sub/ directory entry in archive")
	}
	if dirEntry.header.Typeflag != tar.TypeDir {
		t.Errorf("sub/ Typeflag = %v, want TypeDir", dirEntry.header.Typeflag)
	}

	linkEntry, ok := entries["link.txt"]
	if !ok {
		t.Fatal("expected link.txt in archive")
	}
	if linkEntry.header.Typeflag != tar.TypeSymlink {
		t.Errorf("link.txt Typeflag = %v, want TypeSymlink", linkEntry.header.Typeflag)
	}
	if linkEntry.header.Linkname != "content.txt" {
		t.Errorf("link.txt Linkname = %q, want %q", linkEntry.header.Linkname, "content.txt")
	}

	hardlinkEntry, ok := entries["hardlink.txt"]
	if !ok {
		t.Fatal("expected hardlink.txt in archive")
	}
	if hardlinkEntry.header.Typeflag != tar.TypeLink {
		t.Errorf("hardlink.txt Typeflag = %v, want TypeLink", hardlinkEntry.header.Typeflag)
	}
	if hardlinkEntry.header.Linkname != "content.txt" {
		t.Errorf("hardlink.txt Linkname = %q, want %q", hardlinkEntry.header.Linkname, "content.txt")
	}

	// mode and mtime, second precision
	srcInfo, err := os.Stat(filepath.Join(root, "content.txt"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if rootEntry.header.Mode != int64(srcInfo.Mode().Perm()) {
		t.Errorf("content.txt Mode = %o, want %o", rootEntry.header.Mode, srcInfo.Mode().Perm())
	}
	if !rootEntry.header.ModTime.Truncate(time.Second).
		Equal(srcInfo.ModTime().Truncate(time.Second)) {
		t.Errorf("content.txt ModTime = %v, want %v", rootEntry.header.ModTime, srcInfo.ModTime())
	}
}

func TestWriteRoundTripsFixtureWithGzip(t *testing.T) {
	root, rootContent, _ := buildFixture(t)
	destDir := t.TempDir()

	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	result, err := Write(root, destDir, "myarchive", scan, WriteOptions{Gzip: true})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if filepath.Ext(result.ArchivePath) != ".gz" {
		t.Errorf("ArchivePath = %q, want a .tar.gz file", result.ArchivePath)
	}

	entries := readBackTar(t, result.ArchivePath, true)
	rootEntry, ok := entries["content.txt"]
	if !ok {
		t.Fatal("expected content.txt in archive")
	}
	if !bytes.Equal(rootEntry.content, rootContent) {
		t.Errorf("content.txt content = %q, want %q", rootEntry.content, rootContent)
	}
}

func TestWriteProducesManifestMatchingContentHashes(t *testing.T) {
	root, rootContent, subContent := buildFixture(t)
	destDir := t.TempDir()

	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	result, err := Write(root, destDir, "myarchive", scan, WriteOptions{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	wantHashes := map[string][]byte{
		"content.txt":  rootContent,
		"sub/file.txt": subContent,
	}
	got := map[string]string{}
	for _, e := range result.Manifest {
		got[e.Path] = e.Hash
	}
	for path, content := range wantHashes {
		hash := got[path]
		if hash == "" {
			t.Fatalf("no manifest entry for %q (manifest: %+v)", path, result.Manifest)
		}
		want := fmt.Sprintf("%x", sha256.Sum256(content))
		if hash != want {
			t.Errorf("manifest hash for %q = %q, want %q", path, hash, want)
		}
	}
	// hard link occurrence must not get its own manifest entry (same bytes as content.txt)
	if _, ok := got["hardlink.txt"]; ok {
		t.Error("hardlink.txt should not have its own manifest entry")
	}

	manifestFile, err := os.Open(result.ManifestPath)
	if err != nil {
		t.Fatalf("Open manifest: %v", err)
	}
	defer func() { _ = manifestFile.Close() }()
	onDisk, err := ReadManifest(manifestFile)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(onDisk) != len(result.Manifest) {
		t.Errorf("on-disk manifest has %d entries, want %d", len(onDisk), len(result.Manifest))
	}
}

func TestWriteRecordsFileThatVanishesBetweenScanAndWrite(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "temp.txt"), []byte("gone soon"))
	writeFile(t, filepath.Join(root, "keep.txt"), []byte("stays"))
	destDir := t.TempDir()

	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "temp.txt")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	result, err := Write(root, destDir, "myarchive", scan, WriteOptions{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	found := false
	for _, p := range result.Changed {
		if p == "temp.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("Changed = %v, want temp.txt listed as vanished", result.Changed)
	}

	entries := readBackTar(t, result.ArchivePath, false)
	if _, ok := entries["temp.txt"]; ok {
		t.Error("vanished file must not appear in the archive")
	}
	if _, ok := entries["keep.txt"]; !ok {
		t.Error("expected keep.txt to still be archived")
	}
}

// failAfterWriter wraps a real syncWriteCloser and fails once more than limit
// bytes have been written, so Write's cleanup-on-failure path can be tested
// against a real partial file on disk.
type failAfterWriter struct {
	inner   syncWriteCloser
	limit   int
	written int
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.written >= w.limit {
		return 0, fmt.Errorf("simulated write failure after %d bytes", w.limit)
	}
	allowed := w.limit - w.written
	if allowed > len(p) {
		allowed = len(p)
	}
	n, err := w.inner.Write(p[:allowed])
	w.written += n
	if err != nil {
		return n, err
	}
	if n < len(p) {
		return n, fmt.Errorf("simulated write failure after %d bytes", w.limit)
	}
	return n, nil
}

func (w *failAfterWriter) Close() error { return w.inner.Close() }
func (w *failAfterWriter) Sync() error  { return w.inner.Sync() }

func TestWriteFailureMidStreamLeavesNoFinalFile(t *testing.T) {
	root, _, _ := buildFixture(t)
	destDir := t.TempDir()

	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	failingFactory := func(path string) (syncWriteCloser, error) {
		f, ferr := os.Create(path)
		if ferr != nil {
			return nil, ferr
		}
		return &failAfterWriter{inner: f, limit: 5}, nil
	}

	_, err = write(root, destDir, "myarchive", scan, WriteOptions{}, failingFactory)
	if err == nil {
		t.Fatal("expected an error from a writer that fails mid-stream")
	}

	remaining, rerr := os.ReadDir(destDir)
	if rerr != nil {
		t.Fatalf("ReadDir(%q): %v", destDir, rerr)
	}
	if len(remaining) != 0 {
		var names []string
		for _, e := range remaining {
			names = append(names, e.Name())
		}
		t.Errorf("expected no files left in destDir after a failed write, found: %v", names)
	}
}

func TestWriteProgressSumsToTheScannedTotal(t *testing.T) {
	root, _, _ := buildFixture(t)
	destDir := t.TempDir()
	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var reported int64
	if _, err := Write(root, destDir, "myarchive", scan, WriteOptions{
		OnProgress: func(n int64) { reported += n },
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// The scan total counts regular-file bytes only, which is exactly what
	// the write phase streams — so the meter can never end short of 100%.
	if reported != scan.TotalBytes {
		t.Errorf("progress reported %d bytes, scan promised %d", reported, scan.TotalBytes)
	}
	if scan.TotalBytes == 0 {
		t.Fatal("fixture should contain file bytes, otherwise this proves nothing")
	}
}

func TestWriteWithoutProgressCallbackStillArchives(t *testing.T) {
	root, rootContent, _ := buildFixture(t)
	destDir := t.TempDir()
	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	result, err := Write(root, destDir, "myarchive", scan, WriteOptions{OnProgress: nil})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	entries := readBackTar(t, result.ArchivePath, false)
	if got := entries["content.txt"].content; !bytes.Equal(got, rootContent) {
		t.Errorf("content.txt = %q, want %q", got, rootContent)
	}
}

func TestWriteProgressCountsFilesLargerThanTheReadBuffer(t *testing.T) {
	// A file several read buffers long proves the count accumulates across
	// reads rather than being recorded once per entry.
	root := t.TempDir()
	big := bytes.Repeat([]byte("a"), readBufferSize*2+1234)
	writeFile(t, filepath.Join(root, "big.bin"), big)

	destDir := t.TempDir()
	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var reported int64
	if _, err := Write(root, destDir, "myarchive", scan, WriteOptions{
		OnProgress: func(n int64) { reported += n },
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if reported != int64(len(big)) {
		t.Errorf("progress reported %d bytes, file is %d", reported, len(big))
	}
}
