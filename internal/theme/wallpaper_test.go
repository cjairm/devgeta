package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() {
	testutil.InitLogger()
}

// isolateWallpaperDir points paths.Paths.Config.Devgeta at a fresh temp
// tree, restoring the original in t.Cleanup, so CopyWallpaper's writes never
// touch the real machine.
func isolateWallpaperDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	orig := paths.Paths.Config.Devgeta
	t.Cleanup(func() { paths.Paths.Config.Devgeta = orig })
	paths.Paths.Config.Devgeta = filepath.Join(root, "devgeta")
	return root
}

func writeFakeImage(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// tiny1x1PNG is a real, valid 1x1 transparent PNG's bytes, so
// http.DetectContentType sniffs it as image/png.
var tiny1x1PNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestCopyWallpaper_RejectsMissingFile(t *testing.T) {
	root := isolateWallpaperDir(t)
	_, err := CopyWallpaper(filepath.Join(root, "does-not-exist.jpg"))
	if err == nil {
		t.Fatal("expected an error for a missing source file")
	}
}

func TestCopyWallpaper_RejectsNonImage(t *testing.T) {
	root := isolateWallpaperDir(t)
	src := filepath.Join(root, "notes.txt")
	writeFakeImage(t, src, []byte("just some text, not an image\n"))

	_, err := CopyWallpaper(src)
	if err == nil {
		t.Fatal("expected an error for a non-image file")
	}
}

func TestCopyWallpaper_DistinctImagesSameBasename_GetDistinctFiles(t *testing.T) {
	root := isolateWallpaperDir(t)
	srcA := filepath.Join(root, "a", "wallpaper.png")
	srcB := filepath.Join(root, "b", "wallpaper.png")
	writeFakeImage(t, srcA, tiny1x1PNG)
	// A different (still valid) PNG - append a trailing comment-like byte
	// sequence PNG readers ignore, but that changes the sha256 hash the
	// content-addressed name is derived from.
	writeFakeImage(t, srcB, append(append([]byte{}, tiny1x1PNG...), 0x00))

	destA, err := CopyWallpaper(srcA)
	if err != nil {
		t.Fatalf("CopyWallpaper(a) error: %v", err)
	}
	destB, err := CopyWallpaper(srcB)
	if err != nil {
		t.Fatalf("CopyWallpaper(b) error: %v", err)
	}

	if destA == destB {
		t.Fatalf("expected distinct destinations for distinct images, got the same: %s", destA)
	}
	gotA, err := os.ReadFile(destA)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := os.ReadFile(destB)
	if err != nil {
		t.Fatal(err)
	}
	wantA, _ := os.ReadFile(srcA)
	wantB, _ := os.ReadFile(srcB)
	if string(gotA) != string(wantA) {
		t.Errorf("destA content does not match srcA")
	}
	if string(gotB) != string(wantB) {
		t.Errorf("destB content does not match srcB")
	}
}

func TestCopyWallpaper_SameImageTwice_ResolvesToOneFile(t *testing.T) {
	root := isolateWallpaperDir(t)
	srcA := filepath.Join(root, "a", "wallpaper.png")
	srcB := filepath.Join(root, "b", "wallpaper.png")
	writeFakeImage(t, srcA, tiny1x1PNG)
	writeFakeImage(t, srcB, tiny1x1PNG)

	destA, err := CopyWallpaper(srcA)
	if err != nil {
		t.Fatalf("CopyWallpaper(a) error: %v", err)
	}
	destB, err := CopyWallpaper(srcB)
	if err != nil {
		t.Fatalf("CopyWallpaper(b) error: %v", err)
	}

	if destA != destB {
		t.Errorf("expected identical bytes to resolve to one file, got %s and %s", destA, destB)
	}
}

func TestCopyWallpaper_SameImageAgain_DoesNotChurn(t *testing.T) {
	root := isolateWallpaperDir(t)
	src := filepath.Join(root, "wallpaper.png")
	writeFakeImage(t, src, tiny1x1PNG)

	dest1, err := CopyWallpaper(src)
	if err != nil {
		t.Fatal(err)
	}
	info1, err := os.Stat(dest1)
	if err != nil {
		t.Fatal(err)
	}

	dest2, err := CopyWallpaper(src)
	if err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(dest2)
	if err != nil {
		t.Fatal(err)
	}

	if dest1 != dest2 {
		t.Fatalf("expected the same destination, got %s and %s", dest1, dest2)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Errorf(
			"expected no re-write (unchanged mtime) for re-setting the same image, got %v -> %v",
			info1.ModTime(), info2.ModTime(),
		)
	}
}

func TestSweepOrphanedWallpapers_RemovesUnreferencedKeepsReferencedAndForeign(t *testing.T) {
	isolateWallpaperDir(t)
	dir := WallpapersDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	referenced := filepath.Join(dir, "abcdef0123456789.png")
	orphaned := filepath.Join(dir, "1111111111111111.jpg")
	foreign := filepath.Join(dir, "not-content-addressed.png")
	for _, p := range []string{referenced, orphaned, foreign} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	gc := &config.GlobalConfig{Wallpapers: map[string]string{"default": referenced}}
	if err := SweepOrphanedWallpapers(gc); err != nil {
		t.Fatalf("SweepOrphanedWallpapers error: %v", err)
	}

	if _, err := os.Stat(referenced); err != nil {
		t.Errorf("expected referenced file to survive: %v", err)
	}
	if _, err := os.Stat(orphaned); !os.IsNotExist(err) {
		t.Errorf("expected orphaned file to be removed, err=%v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("expected a non-content-addressed file to survive untouched: %v", err)
	}
}

// TestNewWallpaperSetter_PicksPlatform proves the platform choice is made in
// exactly one place (base.IsMac()), the same check every other platform-
// branching app already uses.
func TestNewWallpaperSetter_PicksPlatform(t *testing.T) {
	mac := NewWallpaperSetter(&commands.MockBaseCommand{IsMacResult: true})
	if _, ok := mac.(*macWallpaperSetter); !ok {
		t.Errorf("expected a macWallpaperSetter for IsMac() == true, got %T", mac)
	}

	linux := NewWallpaperSetter(&commands.MockBaseCommand{IsMacResult: false})
	if _, ok := linux.(*fehWallpaperSetter); !ok {
		t.Errorf("expected a fehWallpaperSetter for IsMac() == false, got %T", linux)
	}
}

// TestFehWallpaperSetter_HelperMissing_ReturnsErrorNamingInstallCommand
// covers the normal state on a fresh Linux machine (cycle doc Step 9 /
// ADR-0044 point 6: feh is not in devgeta's install set). No command may be
// attempted, and the error must be actionable rather than a bare "not
// found". The caller (dg theme set) is what turns this into a warning
// rather than a command failure - the setter itself reports what happened.
func TestFehWallpaperSetter_HelperMissing_ReturnsErrorNamingInstallCommand(t *testing.T) {
	origLookPath := commands.LookPathFn
	t.Cleanup(func() { commands.LookPathFn = origLookPath })
	commands.LookPathFn = func(string) (string, error) { return "", fmt.Errorf("not found") }

	base := &commands.MockBaseCommand{IsMacResult: false}
	setter := NewWallpaperSetter(base)

	err := setter.SetWallpaper("/tmp/whatever.png")
	if err == nil {
		t.Fatal("expected an error when feh is not installed")
	}
	if got := err.Error(); !containsAll(got, "feh", "apt install feh") {
		t.Errorf("error %q does not name both the binary and the install command", got)
	}
	if len(base.ExecCommandCalls) != 0 {
		t.Errorf("expected no command attempted, got: %v", base.ExecCommandCalls)
	}
}

// TestFehWallpaperSetter_HelperPresent_InvokesItThroughTheSharedExecutor
// proves the real call goes through BaseCommandExecutor.ExecCommand (so the
// mocked executor records it, matching CLAUDE.md §6's external-tool rule)
// rather than a raw exec.Command.
func TestFehWallpaperSetter_HelperPresent_InvokesItThroughTheSharedExecutor(t *testing.T) {
	origLookPath := commands.LookPathFn
	t.Cleanup(func() { commands.LookPathFn = origLookPath })
	commands.LookPathFn = func(string) (string, error) { return "/usr/bin/feh", nil }

	base := &commands.MockBaseCommand{IsMacResult: false}
	setter := NewWallpaperSetter(base)

	if err := setter.SetWallpaper("/tmp/wallpaper.png"); err != nil {
		t.Fatalf("SetWallpaper error: %v", err)
	}
	if len(base.ExecCommandCalls) != 1 {
		t.Fatalf("expected exactly one command, got: %v", base.ExecCommandCalls)
	}
	got := base.ExecCommandCalls[0]
	if got.Command != "feh" {
		t.Errorf("Command = %q, want feh", got.Command)
	}
	wantArgs := []string{"--bg-fill", "/tmp/wallpaper.png"}
	if len(got.Args) != len(wantArgs) || got.Args[0] != wantArgs[0] || got.Args[1] != wantArgs[1] {
		t.Errorf("Args = %v, want %v", got.Args, wantArgs)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
