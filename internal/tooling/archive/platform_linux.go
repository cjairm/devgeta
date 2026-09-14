// Linux-specific stat and filesystem-type helpers for `dg archive`. There is
// no iCloud placeholder concept on Linux, and `--mac-metadata` is darwin-only
// (cycle doc 2026-09-13-dg-archive.md), so xattrs are not read here.
package archive

import (
	"errors"

	"golang.org/x/sys/unix"
)

const msdosSuperMagic = 0x4d44

var errXattrsUnsupported = errors.New("xattrs unsupported on linux")

// IsFATFilesystem reports whether the filesystem holding path is FAT (a
// 4 GiB file-size limit), as opposed to exFAT or anything else.
func IsFATFilesystem(path string) (bool, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return false, err
	}
	return int64(stat.Type) == msdosSuperMagic, nil
}

// FreeBytes reports how many bytes an unprivileged process can still write to
// the filesystem holding path.
func FreeBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * stat.Bsize, nil
}

// isDataless always reports false: Linux has no iCloud-style dataless
// placeholder files.
func isDataless(path string) (bool, error) {
	return false, nil
}

// statIdentity returns the (device, inode, link-count) triple used to detect
// hard links: two regular files sharing (dev, ino) are the same file.
func statIdentity(path string) (dev uint64, ino uint64, nlink uint64, err error) {
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return 0, 0, 0, err
	}
	return uint64(stat.Dev), stat.Ino, uint64(stat.Nlink), nil
}

func listXattrs(path string) ([]string, error) {
	return nil, errXattrsUnsupported
}

func getXattr(path, name string) ([]byte, error) {
	return nil, errXattrsUnsupported
}
