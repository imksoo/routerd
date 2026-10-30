// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func lockNativeOperation(path string) (int, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return -1, err
	}
	descriptor, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return -1, fmt.Errorf("open native operation owner: %w", err)
	}
	var info unix.Stat_t
	if err := unix.Fstat(descriptor, &info); err != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Nlink != 1 {
		_ = unix.Close(descriptor)
		return -1, errors.Join(err, errors.New("native operation lock is not a private regular inode"))
	}
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(descriptor)
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return -1, errors.New("another routerd process owns native operations; use its control API or stop it first")
		}
		return -1, fmt.Errorf("acquire native operation owner: %w", err)
	}
	return descriptor, nil
}
func closeNativeOperation(descriptor int) error { return unix.Close(descriptor) }
