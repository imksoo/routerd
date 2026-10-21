// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package dhcpeventqueue

import (
	"fmt"
	"os"
	"syscall"
)

func supportedPlatform() error { return nil }
func privatePath(path string, directory bool) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 || st.IsDir() != directory || (!directory && !st.Mode().IsRegular()) || st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("unsafe DHCP event queue path %s", path)
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || (!directory && stat.Nlink != 1) {
		return fmt.Errorf("DHCP event queue path is not privately owned: %s", path)
	}
	return nil
}

func lockConsumer(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
