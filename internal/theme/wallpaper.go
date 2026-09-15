package theme

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// WallpapersDir is where dg theme set-wallpaper copies images, keyed by
// content rather than by theme name (ADR-0044 point 3) - never inside
// configs/themes/, which is embedded and overwritten on every install/
// upgrade.
func WallpapersDir() string {
	return filepath.Join(paths.Paths.Config.Devgeta, "wallpapers")
}

// contentAddressedNamePattern is the shape CopyWallpaper writes and
// SweepOrphanedWallpapers is allowed to delete: 16 lowercase hex characters
// (the leading half of a sha256) plus the source's extension. Anything else
// in WallpapersDir() is a file the sweep must never touch, even if it looks
// orphaned - it did not create it.
var contentAddressedNamePattern = regexp.MustCompile(`^[0-9a-f]{16}\.[a-z0-9]+$`)

// CopyWallpaper validates path is a readable image and copies it into
// WallpapersDir() under a content-addressed name, returning the copy's
// absolute path. The destination name is derived from the file's bytes, not
// its basename (cycle doc Step 9): distinct images always get distinct
// names regardless of what they were called, identical images collapse to
// one file no matter how many themes reference it, and re-setting the same
// image is a no-op - the name already existing means it already holds the
// right bytes.
func CopyWallpaper(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read wallpaper %s: %w", path, err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("wallpaper %s is empty", path)
	}

	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	if !strings.HasPrefix(http.DetectContentType(head), "image/") {
		return "", fmt.Errorf("%s does not look like an image", path)
	}

	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:])[:16] + strings.ToLower(filepath.Ext(path))
	dest := filepath.Join(WallpapersDir(), name)

	if _, err := os.Stat(dest); err == nil {
		// The name already holds these exact bytes - nothing to write.
		return dest, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to stat %s: %w", dest, err)
	}

	if err := files.WriteFileAtomic(dest, data, files.FilePermission); err != nil {
		return "", fmt.Errorf("failed to copy wallpaper to %s: %w", dest, err)
	}
	return dest, nil
}

// SweepOrphanedWallpapers removes every content-addressed file in
// WallpapersDir() that no value in gc.Wallpapers points to, so replacing a
// theme's wallpaper does not grow the directory without bound. It is
// best-effort by design (cycle doc Step 9): called only after the owning
// config.Update has already committed, so a failed removal never leaves a
// wrong path recorded, only a stale file on disk. Never removes a file
// whose name is not shaped like one CopyWallpaper wrote - a user's own file
// dropped in the same directory survives regardless of whether anything
// references it.
func SweepOrphanedWallpapers(gc *config.GlobalConfig) error {
	dir := WallpapersDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", dir, err)
	}

	referenced := make(map[string]bool, len(gc.Wallpapers))
	for _, p := range gc.Wallpapers {
		referenced[p] = true
	}

	var firstErr error
	for _, e := range entries {
		if e.IsDir() || !contentAddressedNamePattern.MatchString(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if referenced[p] {
			continue
		}
		if err := os.Remove(p); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to remove orphaned wallpaper %s: %w", p, err)
		}
	}
	return firstErr
}

// WallpaperSetter applies an image as the desktop wallpaper on the current
// platform. Every call site treats it as best-effort (ADR-0044 point 4):
// dg theme set never fails because a wallpaper could not be applied.
type WallpaperSetter interface {
	SetWallpaper(path string) error
}

// NewWallpaperSetter picks the platform implementation via base.IsMac(),
// the same check every other platform-branching app already uses
// (BaseCommandExecutor.IsMac()).
func NewWallpaperSetter(base cmd.BaseCommandExecutor) WallpaperSetter {
	if base.IsMac() {
		return &macWallpaperSetter{base: base}
	}
	return &fehWallpaperSetter{base: base}
}

// macWallpaperSetter sets the wallpaper on every Space at once via
// AppleScript's System Events, the commonly documented route for doing so
// (looping over "every desktop" rather than "desktop 1" alone, which would
// only cover the current Space). Not independently measured against a real
// machine as part of this cycle - cycle doc Step 8 called for that
// measurement before implementing, and it was skipped this pass; verifying
// it (including survival across a Space switch and a logout) is left to the
// maintainer.
type macWallpaperSetter struct {
	base cmd.BaseCommandExecutor
}

func (m *macWallpaperSetter) SetWallpaper(path string) error {
	script := fmt.Sprintf(
		`tell application "System Events" to tell every desktop to set picture to %q`,
		path,
	)
	_, _, err := m.base.ExecCommand(cmd.CommandParams{
		Command: "osascript",
		Args:    []string{"-e", script},
	})
	if err != nil {
		return fmt.Errorf("failed to set wallpaper via osascript: %w", err)
	}
	return nil
}

// fehWallpaperSetter sets the wallpaper via feh on Linux/i3. feh is not in
// devgeta's install set (cycle doc Step 9 / ADR-0044 point 6), so a missing
// binary is the normal state on a fresh machine, not an edge case - it warns
// naming the install command rather than failing or silently doing nothing.
type fehWallpaperSetter struct {
	base cmd.BaseCommandExecutor
}

func (f *fehWallpaperSetter) SetWallpaper(path string) error {
	if _, err := cmd.LookPathFn("feh"); err != nil {
		return fmt.Errorf(
			"feh is not installed - install it with `sudo apt install feh` to enable wallpaper support: %w",
			err,
		)
	}
	_, _, err := f.base.ExecCommand(cmd.CommandParams{
		Command: "feh",
		Args:    []string{"--bg-fill", path},
	})
	if err != nil {
		return fmt.Errorf("failed to set wallpaper via feh: %w", err)
	}
	return nil
}
