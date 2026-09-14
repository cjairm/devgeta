package archive

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeFixtureArchive(t *testing.T) (result *WriteResult) {
	t.Helper()
	root, _, _ := buildFixture(t)
	destDir := t.TempDir()
	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	result, err = Write(root, destDir, "myarchive", scan, WriteOptions{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	return result
}

func TestVerifySucceedsOnIntactArchive(t *testing.T) {
	result := writeFixtureArchive(t)

	vr, err := Verify(result.ArchivePath)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	raw, err := os.ReadFile(result.ArchivePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(raw))
	if vr.ArchiveHash != want {
		t.Errorf("ArchiveHash = %q, want %q", vr.ArchiveHash, want)
	}

	hashFileContent, err := os.ReadFile(vr.ArchiveHashPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", vr.ArchiveHashPath, err)
	}
	if !bytes.Contains(hashFileContent, []byte(want)) {
		t.Errorf(
			"hash sidecar file %q = %q, want it to contain %q",
			vr.ArchiveHashPath,
			hashFileContent,
			want,
		)
	}
}

func TestVerifyDetectsFlippedByte(t *testing.T) {
	result := writeFixtureArchive(t)

	f, err := os.OpenFile(result.ArchivePath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	mid := info.Size() / 2
	buf := make([]byte, 1)
	if _, err := f.ReadAt(buf, mid); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	buf[0] ^= 0xFF
	if _, err := f.WriteAt(buf, mid); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := Verify(result.ArchivePath); err == nil {
		t.Fatal("expected Verify to detect a flipped byte")
	}
}

func TestVerifyDetectsEntryMissingFromArchive(t *testing.T) {
	result := writeFixtureArchive(t)

	// Append a bogus manifest entry naming a path the archive does not have.
	f, err := os.OpenFile(result.ManifestPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteString(
		fmt.Sprintf(
			"%s  %s\n",
			"0000000000000000000000000000000000000000000000000000000000000000"[:64],
			"ghost.txt",
		),
	); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := Verify(result.ArchivePath); err == nil {
		t.Fatal("expected Verify to detect a manifest entry missing from the archive")
	}
}

func TestVerifyDetectsEntryNotInManifest(t *testing.T) {
	result := writeFixtureArchive(t)

	manifest, err := func() ([]ManifestEntry, error) {
		f, ferr := os.Open(result.ManifestPath)
		if ferr != nil {
			return nil, ferr
		}
		defer func() { _ = f.Close() }()
		return ReadManifest(f)
	}()
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(manifest) == 0 {
		t.Fatal("expected a non-empty manifest")
	}
	trimmed := manifest[1:] // drop one entry the archive still contains

	f, err := os.Create(result.ManifestPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := WriteManifest(f, trimmed); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := Verify(result.ArchivePath); err == nil {
		t.Fatal("expected Verify to detect an archive entry the manifest does not list")
	}
}

func TestVerifyRejectsUnrecognizedExtension(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notanarchive.zip")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Verify(path); err == nil {
		t.Fatal("expected Verify to reject an unrecognized archive extension")
	}
}
