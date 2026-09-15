package theme

import (
	"fmt"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// themeSuffixes marks, as a sibling of the shadowed path, the two ways `dg
// theme set`'s transaction can leave a manifest entry mid-flight (cycle doc
// 2026-09-14-dg-theme.md Step 5): a rename-aside of what was there, or (for
// a path that did not exist yet) a marker recording that "restore" means
// "delete".
//
// The mechanism itself is pkg/files — `dg import` needs the same primitives
// under its own suffixes, so they take the pair as a parameter and exist
// once. What stays here is the suffixes themselves, which are what makes a
// leftover on disk recognizable as an interrupted theme switch.
const (
	backupSuffix = ".dg-theme-backup"
	absentSuffix = ".dg-theme-absent"
)

var themeSuffixes = files.BackupSuffixes{Backup: backupSuffix, Absent: absentSuffix}

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

// BackupPath renames p aside under the theme suffixes, or — when p does not
// exist yet — leaves an absent marker.
func BackupPath(p string) error { return files.BackupPath(p, themeSuffixes) }

// RestorePath undoes BackupPath.
func RestorePath(p string) error { return files.RestorePath(p, themeSuffixes) }

// DiscardBackup deletes p's siblings without restoring them - the
// committed-switch case: current_theme already names the new theme, so the
// old content the backup holds must never come back.
func DiscardBackup(p string) error { return files.DiscardBackup(p, themeSuffixes) }

// HasBackup reports whether p has a backup or absent-marker sibling
// on disk right now.
func HasBackup(p string) bool { return files.HasBackup(p, themeSuffixes) }

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
