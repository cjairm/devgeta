package archive

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestScanOrdersEntriesLexically(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "b.txt"), []byte("b"))
	writeFile(t, filepath.Join(root, "a.txt"), []byte("a"))
	mkdir(t, filepath.Join(root, "z-dir"))
	writeFile(t, filepath.Join(root, "z-dir", "inner.txt"), []byte("inner"))

	result, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var paths []string
	for _, e := range result.Entries {
		paths = append(paths, e.Path)
	}
	want := []string{"a.txt", "b.txt", "z-dir", "z-dir/inner.txt"}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i, p := range want {
		if paths[i] != p {
			t.Errorf("paths[%d] = %q, want %q (full: %v)", i, paths[i], p, paths)
		}
	}
}

func TestScanReportsTotalBytesAndSkips(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.txt"), []byte("hello"))
	writeFile(t, filepath.Join(root, "package.json"), []byte("{}"))
	mkdir(t, filepath.Join(root, "node_modules"))
	writeFile(t, filepath.Join(root, "node_modules", "big.js"), []byte("0123456789"))

	result, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	wantTotal := int64(len("hello") + len("{}"))
	if result.TotalBytes != wantTotal {
		t.Errorf("TotalBytes = %d, want %d", result.TotalBytes, wantTotal)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("Skipped = %v, want exactly one entry", result.Skipped)
	}
	if result.Skipped[0].Path != "node_modules" {
		t.Errorf("Skipped[0].Path = %q, want %q", result.Skipped[0].Path, "node_modules")
	}
	if result.Skipped[0].Rule == "" {
		t.Error("expected a non-empty skip rule")
	}
	if result.Skipped[0].Size != 0 {
		t.Errorf("Skipped[0].Size = %d, want 0 without ComputeSkipSize", result.Skipped[0].Size)
	}
	for _, e := range result.Entries {
		if e.Path == "node_modules" || e.Path == "node_modules/big.js" {
			t.Errorf("skipped tree leaked into Entries: %q", e.Path)
		}
	}
}

func TestScanComputesSkipSizeOnlyWhenRequested(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), []byte("{}"))
	mkdir(t, filepath.Join(root, "node_modules"))
	content := []byte("0123456789")
	writeFile(t, filepath.Join(root, "node_modules", "big.js"), content)

	result, err := Scan(root, ScanOptions{ComputeSkipSize: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("Skipped = %v, want exactly one entry", result.Skipped)
	}
	if result.Skipped[0].Size != int64(len(content)) {
		t.Errorf("Skipped[0].Size = %d, want %d", result.Skipped[0].Size, len(content))
	}
}

func TestScanNoSkipDisablesSkipRules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), []byte("{}"))
	mkdir(t, filepath.Join(root, "node_modules"))
	writeFile(t, filepath.Join(root, "node_modules", "big.js"), []byte("x"))

	result, err := Scan(root, ScanOptions{NoSkip: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want none with NoSkip", result.Skipped)
	}
	found := false
	for _, e := range result.Entries {
		if e.Path == "node_modules/big.js" {
			found = true
		}
	}
	if !found {
		t.Error("expected node_modules/big.js to be archived with NoSkip")
	}
}

func TestScanDetectsUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not block reads")
	}
	root := t.TempDir()
	path := filepath.Join(root, "secret.txt")
	writeFile(t, path, []byte("shh"))
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	result, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Unreadable) != 1 || result.Unreadable[0] != "secret.txt" {
		t.Errorf("Unreadable = %v, want [secret.txt]", result.Unreadable)
	}
	for _, e := range result.Entries {
		if e.Path == "secret.txt" {
			t.Error("unreadable file must not appear in Entries")
		}
	}
}

func TestScanDetectsSpecialFiles(t *testing.T) {
	root := t.TempDir()
	fifoPath := filepath.Join(root, "myfifo")
	if err := unix.Mkfifo(fifoPath, 0o644); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	result, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Special) != 1 || result.Special[0] != "myfifo" {
		t.Errorf("Special = %v, want [myfifo]", result.Special)
	}
	for _, e := range result.Entries {
		if e.Path == "myfifo" {
			t.Error("FIFO must not appear in Entries")
		}
	}
}

func TestScanDetectsSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "target.txt"), []byte("data"))
	linkPath := filepath.Join(root, "link.txt")
	if err := os.Symlink("target.txt", linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	result, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var link *Entry
	for i := range result.Entries {
		if result.Entries[i].Path == "link.txt" {
			link = &result.Entries[i]
		}
	}
	if link == nil {
		t.Fatal("expected link.txt in Entries")
	}
	if link.Kind != KindSymlink {
		t.Errorf("link.txt Kind = %v, want KindSymlink", link.Kind)
	}
	if link.LinkTarget != "target.txt" {
		t.Errorf("link.txt LinkTarget = %q, want %q", link.LinkTarget, "target.txt")
	}
}

func TestScanDetectsHardLink(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "a.txt")
	second := filepath.Join(root, "b.txt")
	writeFile(t, first, []byte("shared content"))
	if err := os.Link(first, second); err != nil {
		t.Fatalf("Link: %v", err)
	}

	result, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var a, b *Entry
	for i := range result.Entries {
		switch result.Entries[i].Path {
		case "a.txt":
			a = &result.Entries[i]
		case "b.txt":
			b = &result.Entries[i]
		}
	}
	if a == nil || b == nil {
		t.Fatalf("expected both a.txt and b.txt in Entries, got %+v", result.Entries)
	}
	if a.Kind != KindFile {
		t.Errorf("a.txt Kind = %v, want KindFile (first occurrence)", a.Kind)
	}
	if b.Kind != KindHardLink {
		t.Errorf("b.txt Kind = %v, want KindHardLink (second occurrence)", b.Kind)
	}
	if b.LinkTarget != "a.txt" {
		t.Errorf("b.txt LinkTarget = %q, want %q", b.LinkTarget, "a.txt")
	}
	if result.TotalBytes != int64(len("shared content")) {
		t.Errorf(
			"TotalBytes = %d, want the content counted once (%d)",
			result.TotalBytes,
			len("shared content"),
		)
	}
}

func TestScanReportsWindowsIncompatibleNames(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes:draft.txt")
	writeFile(t, path, []byte("data"))

	result, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	found := false
	for _, e := range result.Entries {
		if e.Path == "notes:draft.txt" {
			found = true
		}
	}
	if !found {
		t.Error(
			"Windows-incompatible names are warned, not skipped: expected notes:draft.txt in Entries",
		)
	}
	var issue *WindowsIssue
	for i := range result.WindowsIncompatible {
		if result.WindowsIncompatible[i].Path == "notes:draft.txt" {
			issue = &result.WindowsIncompatible[i]
		}
	}
	if issue == nil {
		t.Fatalf(
			"expected notes:draft.txt in WindowsIncompatible, got %+v",
			result.WindowsIncompatible,
		)
	}
	if issue.Reason == "" {
		t.Error("expected a non-empty reason")
	}
}
