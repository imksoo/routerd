// SPDX-License-Identifier: BSD-3-Clause

//go:build linux || freebsd

package chain

import (
	"errors"
	"os"
	"syscall"
)

func copySysctlFileOwner(file *os.File, original os.FileInfo) error {
	stat, ok := original.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("persistent sysctl file ownership is unavailable")
	}
	return file.Chown(int(stat.Uid), int(stat.Gid))
}

func sameSysctlFileOwner(a, b os.FileInfo) bool {
	left, leftOK := a.Sys().(*syscall.Stat_t)
	right, rightOK := b.Sys().(*syscall.Stat_t)
	return leftOK && rightOK && left.Uid == right.Uid && left.Gid == right.Gid
}
