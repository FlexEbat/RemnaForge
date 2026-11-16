// Package fsx wraps the file system calls that change the system. In a dry
// run each of them announces what it would do and does nothing; otherwise it
// is the os call.
package fsx

import (
	"os"

	"github.com/FlexEbat/RemnaForge/internal/runctx"
)

func WriteFile(path string, data []byte, perm os.FileMode) error {
	if runctx.DryRun() {
		runctx.Say("write %s (%d bytes, mode %04o)", path, len(data), perm)
		return nil
	}
	return os.WriteFile(path, data, perm)
}

func MkdirAll(path string, perm os.FileMode) error {
	if runctx.DryRun() {
		if _, err := os.Stat(path); err != nil {
			runctx.Say("create directory %s", path)
		}
		return nil
	}
	return os.MkdirAll(path, perm)
}

func Remove(path string) error {
	if runctx.DryRun() {
		if _, err := os.Lstat(path); err == nil {
			runctx.Say("remove %s", path)
		}
		return nil
	}
	return os.Remove(path)
}

func RemoveAll(path string) error {
	if runctx.DryRun() {
		if _, err := os.Lstat(path); err == nil {
			runctx.Say("remove %s recursively", path)
		}
		return nil
	}
	return os.RemoveAll(path)
}

func Rename(oldpath, newpath string) error {
	if runctx.DryRun() {
		runctx.Say("rename %s to %s", oldpath, newpath)
		return nil
	}
	return os.Rename(oldpath, newpath)
}

func Chmod(path string, mode os.FileMode) error {
	if runctx.DryRun() {
		runctx.Say("set mode %04o on %s", mode, path)
		return nil
	}
	return os.Chmod(path, mode)
}

func Symlink(oldname, newname string) error {
	if runctx.DryRun() {
		runctx.Say("link %s to %s", newname, oldname)
		return nil
	}
	return os.Symlink(oldname, newname)
}
