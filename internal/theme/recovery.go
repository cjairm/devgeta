package theme

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
)

// backupSuffix and absentSuffix mark, as a sibling of the shadowed path, the
// two ways `dg theme set`'s transaction can leave a manifest entry mid-flight
// (cycle doc Step 5): a rename-aside of what was there, or (for a path that
// did not exist yet) a marker recording that "restore" means "delete".
const (
	backupSuffix = ".dg-theme-backup"
	absentSuffix = ".dg-theme-absent"
)

// ManifestEntry pairs a themed app with the exact filesystem paths its
// ForceConfigureTheme writes - not "its config directory", which for most of
// these apps holds files the user owns too. This is dg theme set's
// backup/restore unit.
type ManifestEntry struct {
	App   string
	Paths []string
}

// Manifest returns the fixed, ordered, per-surface backup manifest, resolved
// against the current paths.Paths values. The order matches ADR-0043's
// surface table; cmd/theme.go's `set` walks apps in this same order so a
// test can place one surface before another it rigs to fail. See the cycle
// doc's Step 5 table for why each entry is exactly these paths and not a
// whole config directory - OpenCode is the one surface where the whole
// directory genuinely is the unit, because its own ForceConfigureTheme wipes
// it wholesale already.
func Manifest() []ManifestEntry {
	return []ManifestEntry{
		{
			constants.Alacritty,
			[]string{filepath.Join(paths.Paths.Config.Alacritty, "alacritty.toml")},
		},
		{constants.Ghostty, []string{filepath.Join(paths.Paths.Config.Ghostty, "config")}},
		{constants.OpenCode, []string{paths.Paths.Config.OpenCode}},
		{constants.Tmux, []string{filepath.Join(paths.Paths.Home.Root, ".tmux.conf")}},
		{
			constants.Neovim,
			[]string{filepath.Join(paths.Paths.Config.Nvim, "lua", "devgeta", "theme.lua")},
		},
		{constants.Claude, []string{
			filepath.Join(paths.Paths.Config.Claude, "settings.json"),
			filepath.Join(paths.Paths.Config.Claude, "themes"),
		}},
		{constants.I3, []string{filepath.Join(paths.Paths.Config.I3, "config")}},
	}
}

// BackupPath renames p aside to p+backupSuffix, or — when p does not
// exist yet — creates a p+absentSuffix marker, since a rename-aside cannot
// record "there was nothing here." Both siblings sit next to p, keeping the
// backup on the same filesystem (a collection point under a different mount
// would make the rename an EXDEV copy, which is not atomic).
func BackupPath(p string) error {
	if _, err := os.Lstat(p); err != nil {
		if os.IsNotExist(err) {
			// The parent tree may not exist yet on a machine where this
			// surface has never been configured (e.g. no ~/.config/nvim/lua
			// at all) - the marker still has to land next to where p would
			// be, so the directory is created rather than treated as a
			// second "nothing here" case.
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			return os.WriteFile(p+absentSuffix, nil, 0o644)
		}
		return err
	}
	return os.Rename(p, p+backupSuffix)
}

// RestorePath undoes BackupPath: renames the backup back
// over p, or removes p when an absent marker sits beside it instead. A path
// with neither sibling is left untouched — nothing was ever backed up there.
func RestorePath(p string) error {
	backup := p + backupSuffix
	absent := p + absentSuffix
	if _, err := os.Lstat(backup); err == nil {
		if err := os.RemoveAll(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return os.Rename(backup, p)
	}
	if _, err := os.Lstat(absent); err == nil {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
		return os.Remove(absent)
	}
	return nil
}

// DiscardBackup deletes p's backup/absent-marker siblings without
// restoring them - the committed-switch case: current_theme already names
// the new theme, so the old content the backup holds must never come back.
func DiscardBackup(p string) error {
	backup := p + backupSuffix
	absent := p + absentSuffix
	var firstErr error
	if _, err := os.Lstat(backup); err == nil {
		if err := os.RemoveAll(backup); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if _, err := os.Lstat(absent); err == nil {
		if err := os.Remove(absent); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// HasBackup reports whether p has a backup or absent-marker sibling
// on disk right now.
func HasBackup(p string) bool {
	if _, err := os.Lstat(p + backupSuffix); err == nil {
		return true
	}
	if _, err := os.Lstat(p + absentSuffix); err == nil {
		return true
	}
	return false
}

// RecoverInterrupted sweeps the fixed Manifest for backups or absent markers
// a crashed `dg theme set` left behind, and reports in one line what it did.
// It is safe to call unconditionally — in the ordinary case it finds nothing
// and returns ("", nil) having touched no file — which is what lets `dg
// theme set`, `dg configure` and `dg install` all call it first, every run
// (cycle doc Step 5).
//
// pending_theme is what tells a crashed-before-commit attempt (restore
// backups) from a crashed-during-cleanup one (discard them, since the
// switch already committed): it is set before the first backup is taken and
// cleared in the same config.Update call that commits current_theme, so a
// backup on disk is always in exactly one of those two states.
func RecoverInterrupted() (string, error) {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return "", fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return "", fmt.Errorf("failed to load global config: %w", err)
	}
	pending := gc.PendingTheme

	touched := 0
	for _, entry := range Manifest() {
		for _, p := range entry.Paths {
			if !HasBackup(p) {
				continue
			}
			touched++
			if pending != "" {
				if err := RestorePath(p); err != nil {
					return "", fmt.Errorf("failed to restore %s: %w", p, err)
				}
			} else if err := DiscardBackup(p); err != nil {
				return "", fmt.Errorf("failed to clean up %s: %w", p, err)
			}
		}
	}

	if pending != "" {
		if err := config.Update(func(g *config.GlobalConfig) error {
			g.PendingTheme = ""
			return nil
		}); err != nil {
			return "", fmt.Errorf("failed to clear pending_theme: %w", err)
		}
		if touched == 0 {
			return "", nil
		}
		return fmt.Sprintf(
			"recovered an interrupted switch to %s: restored %d config path(s) — run `dg theme set %s` to retry",
			pending,
			touched,
			pending,
		), nil
	}

	if touched == 0 {
		return "", nil
	}
	return fmt.Sprintf(
		"cleaned up %d leftover backup(s) from a completed theme switch",
		touched,
	), nil
}
