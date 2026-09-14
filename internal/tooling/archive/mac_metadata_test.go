//go:build darwin

package archive

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWriteWithMacMetadataIncludesXattrsAsPaxRecords(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tagged.txt")
	writeFile(t, path, []byte("hello"))

	const xattrName = "com.apple.metadata:_kMDItemUserTags"
	tagValue := []byte("bplist00arbitrary-test-value")
	if err := unix.Setxattr(path, xattrName, tagValue, 0); err != nil {
		t.Fatalf("Setxattr: %v", err)
	}

	destDir := t.TempDir()
	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	result, err := Write(root, destDir, "myarchive", scan, WriteOptions{MacMetadata: true})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	entries := readBackTar(t, result.ArchivePath, false)
	entry, ok := entries["tagged.txt"]
	if !ok {
		t.Fatal("expected tagged.txt in archive")
	}
	if entry.header.PAXRecords == nil {
		t.Fatal("expected PAX records on tagged.txt")
	}
	val, ok := entry.header.PAXRecords["SCHILY.xattr."+xattrName]
	if !ok {
		t.Fatalf(
			"expected SCHILY.xattr.%s PAX record, got %+v",
			xattrName,
			entry.header.PAXRecords,
		)
	}
	if val != string(tagValue) {
		t.Errorf("xattr value = %q, want %q", val, tagValue)
	}

	if _, ok := entries["._tagged.txt"]; ok {
		t.Error("must not write an AppleDouble ._ entry")
	}
}

func TestWriteWithoutMacMetadataOmitsXattrs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tagged.txt")
	writeFile(t, path, []byte("hello"))

	const xattrName = "com.apple.metadata:_kMDItemUserTags"
	if err := unix.Setxattr(path, xattrName, []byte("value"), 0); err != nil {
		t.Fatalf("Setxattr: %v", err)
	}

	destDir := t.TempDir()
	scan, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	result, err := Write(root, destDir, "myarchive", scan, WriteOptions{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	entries := readBackTar(t, result.ArchivePath, false)
	entry, ok := entries["tagged.txt"]
	if !ok {
		t.Fatal("expected tagged.txt in archive")
	}
	if _, ok := entry.header.PAXRecords["SCHILY.xattr."+xattrName]; ok {
		t.Error("expected no xattr PAX record without --mac-metadata")
	}
}
