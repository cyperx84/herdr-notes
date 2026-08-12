//go:build !windows

package store

import (
	"errors"
	"os"
)

func replaceFile(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

func syncParent(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}
