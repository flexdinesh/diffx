//go:build !windows

package localserver

import (
	"errors"
	"os"
	"syscall"
)

func protectRuntime(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ok || stat.Uid != uint32(os.Getuid()) {
		return errors.New("daemon runtime must be a directory owned by this user")
	}
	return os.Chmod(path, 0o700)
}
