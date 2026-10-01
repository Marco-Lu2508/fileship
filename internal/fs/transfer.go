package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Transfer moves between data and database volumes. Copy failures leave the
// source intact; deletion failures keep the complete recovery copy.
func Transfer(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	// Do not follow links when copying a tree outside its original root.
	if err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("cannot transfer non-regular file: %s", path)
		}
		return nil
	}); err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		err = copyDir(src, dst)
	} else {
		err = copyFile(src, dst)
	}
	if err != nil {
		os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}
