// Darwin-specific stat, filesystem-type, and xattr helpers for `dg archive`.
package archive

import (
	"golang.org/x/sys/unix"
)

// IsFATFilesystem reports whether the filesystem holding path is FAT (a
// 4 GiB file-size limit), as opposed to exFAT or anything else.
func IsFATFilesystem(path string) (bool, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return false, err
	}
	return unix.ByteSliceToString(stat.Fstypename[:]) == "msdos", nil
}

// FreeBytes reports how many bytes an unprivileged process can still write to
// the filesystem holding path.
func FreeBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// isDataless reports whether path is an iCloud "Optimize Mac Storage"
// placeholder whose content is not actually present on disk.
func isDataless(path string) (bool, error) {
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return false, err
	}
	return stat.Flags&unix.SF_DATALESS != 0, nil
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

// listXattrs returns the extended attribute names set on path.
func listXattrs(path string) ([]string, error) {
	size, err := unix.Listxattr(path, nil)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	n, err := unix.Listxattr(path, buf)
	if err != nil {
		return nil, err
	}
	return splitNulSeparated(buf[:n]), nil
}

// getXattr returns the value of the extended attribute name on path.
func getXattr(path, name string) ([]byte, error) {
	size, err := unix.Getxattr(path, name, nil)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	n, err := unix.Getxattr(path, name, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func splitNulSeparated(buf []byte) []string {
	var names []string
	start := 0
	for i, b := range buf {
		if b == 0 {
			if i > start {
				names = append(names, string(buf[start:i]))
			}
			start = i + 1
		}
	}
	return names
}
