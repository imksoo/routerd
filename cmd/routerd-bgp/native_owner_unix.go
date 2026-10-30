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

func lockBGPProcess(path string) (int, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return -1, err
	}
	dir, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open BGP runtime directory: %w", err)
	}
	defer unix.Close(dir)
	var info unix.Stat_t
	if err := unix.Fstat(dir, &info); err != nil {
		return -1, err
	}
	if info.Uid != uint32(os.Geteuid()) || info.Mode&0022 != 0 {
		return -1, errors.New("BGP runtime directory is not owned and protected from other writers")
	}
	descriptor, err := unix.Openat(dir, filepath.Base(path), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return -1, fmt.Errorf("open BGP process owner: %w", err)
	}
	if err := unix.Fstat(descriptor, &info); err != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Nlink != 1 || info.Uid != uint32(os.Geteuid()) || info.Mode&0077 != 0 {
		_ = unix.Close(descriptor)
		return -1, errors.Join(err, errors.New("BGP process lock is not a private owned regular inode"))
	}
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(descriptor)
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return -1, errors.New("another routerd-bgp process owns this kernel network world")
		}
		return -1, fmt.Errorf("acquire BGP process owner: %w", err)
	}
	return descriptor, nil
}

func closeBGPProcess(descriptor int) error { return unix.Close(descriptor) }
