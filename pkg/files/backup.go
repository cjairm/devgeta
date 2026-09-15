// Reversible writes: rename a path aside before replacing it, and put it
// back if the work that followed did not finish. Two commands need exactly
// this — `dg theme set` around a multi-app theme switch, and `dg import`
// around a multi-file profile restore — so it lives here with the suffix
// pair as a parameter rather than existing twice.
//
// The backup is always a SIBLING of the path it shadows. A collection point
// under another directory would put the backup on a different mount, which
// turns the rename into an EXDEV copy — no longer atomic, and no longer a
// reliable undo.
package files

import (
	"os"
	"path/filepath"
)

// BackupSuffixes names one caller's pair of sibling markers. They must be
// distinct from each other and specific enough that a leftover is
// recognizable as that caller's: `dg theme set` uses .dg-theme-backup /
// .dg-theme-absent, `dg import` uses .dg-import-backup / .dg-import-absent.
type BackupSuffixes struct {
	// Backup is appended to a path that existed and was renamed aside.
	Backup string
	// Absent is appended to a path that did NOT exist, as a marker that
	// "restore" means "delete".
	Absent string
}

// BackupPath renames p aside to p+Backup, or — when p does not exist yet —
// creates a p+Absent marker, since a rename-aside cannot record "there was
// nothing here" and the rollback of an added file has to be a delete.
func BackupPath(p string, s BackupSuffixes) error {
	if _, err := os.Lstat(p); err != nil {
		if os.IsNotExist(err) {
			// The parent tree may not exist yet on a machine where this
			// surface has never been written (no ~/.config/nvim/lua at all,
			// a profile with no Sessions/) — the marker still has to land
			// next to where p would be, so the directory is created rather
			// than treated as a second "nothing here" case.
			if err := os.MkdirAll(filepath.Dir(p), DirPermission); err != nil {
				return err
			}
			return os.WriteFile(p+s.Absent, nil, FilePermission)
		}
		return err
	}
	return os.Rename(p, p+s.Backup)
}

// RestorePath undoes BackupPath: renames the backup back over p, or removes
// p when an absent marker sits beside it instead. A path with neither
// sibling is left untouched — nothing was ever backed up there.
func RestorePath(p string, s BackupSuffixes) error {
	backup := p + s.Backup
	absent := p + s.Absent
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

// DiscardBackup deletes p's backup and absent-marker siblings without
// restoring them — the committed case, where the new content must stay and
// the old content the backup holds must never come back.
func DiscardBackup(p string, s BackupSuffixes) error {
	backup := p + s.Backup
	absent := p + s.Absent
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

// HasBackup reports whether p has a backup or absent-marker sibling on disk
// right now.
func HasBackup(p string, s BackupSuffixes) bool {
	if _, err := os.Lstat(p + s.Backup); err == nil {
		return true
	}
	if _, err := os.Lstat(p + s.Absent); err == nil {
		return true
	}
	return false
}
