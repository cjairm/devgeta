package agentpack

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// makeReadOnly strips the write bits from every file and directory under dir,
// leaving read and execute alone: a hook that was executable upstream must
// stay executable. The cached clone is the audit copy of upstream, so nothing
// should be able to edit it by accident. Symlinks are left as they are, since
// chmod would follow them out of the tree.
//
// Directories are changed after their contents, because a read-only directory
// can't have its entries chmod-ed.
func makeReadOnly(dir string) error {
	return walkModes(dir, true, func(mode fs.FileMode) fs.FileMode { return mode &^ 0o222 })
}

// makeWritable gives the owner write permission on everything under dir, so
// the tree can be deleted.
func makeWritable(dir string) error {
	return walkModes(dir, false, func(mode fs.FileMode) fs.FileMode { return mode | 0o200 })
}

// walkModes applies change to the permission bits of every non-symlink entry
// under root, including root. bottomUp chooses the order: read-only must
// close directories last, writable must open them first.
func walkModes(root string, bottomUp bool, change func(fs.FileMode) fs.FileMode) error {
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink == 0 {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if bottomUp {
		for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
			paths[i], paths[j] = paths[j], paths[i]
		}
	}
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if err := os.Chmod(p, change(info.Mode().Perm())); err != nil {
			return err
		}
	}
	return nil
}

// removeTree deletes dir even when it is read-only, which os.RemoveAll can't.
// A missing dir is not an error.
func removeTree(dir string) error {
	if _, err := os.Lstat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := makeWritable(dir); err != nil {
		return fmt.Errorf("failed to unlock %s for removal: %w", dir, err)
	}
	return os.RemoveAll(dir)
}

// copyTree copies src into the existing directory dst, keeping each file's
// permission bits (hook scripts must stay executable) while forcing owner
// write, because the source is usually the read-only clone cache and a built
// tree is an ordinary local copy. A top-level .git is skipped: the tree is
// installed into coders, and a clone's history is not part of a pack.
//
// Symlinks are recreated as symlinks, but only when they are relative and stay
// inside the tree. Anything else could point a coder at files outside the
// pack, so it fails the build instead.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			return fs.SkipDir
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			return copySymlink(p, target, rel)
		case d.Type().IsRegular():
			return copyFileKeepingMode(p, target)
		default:
			return fmt.Errorf("%s is neither a file, a directory nor a symlink", rel)
		}
	})
}

func copyFileKeepingMode(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// 0o200 makes it owner-writable; &^ 0o022 keeps group and others from writing.
	mode := (info.Mode().Perm() | 0o200) &^ 0o022
	return os.WriteFile(dst, data, mode)
}

func copySymlink(src, dst, rel string) error {
	linkTarget, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if filepath.IsAbs(linkTarget) {
		return fmt.Errorf("%s is a symlink to the absolute path %s; packs may only link inside themselves", rel, linkTarget)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(rel), linkTarget))
	if resolved == ".." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s is a symlink to %s, which leaves the pack", rel, linkTarget)
	}
	return os.Symlink(linkTarget, dst)
}
