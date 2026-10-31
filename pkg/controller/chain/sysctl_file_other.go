// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package chain

import (
	"errors"
	"os"
)

func copySysctlFileOwner(*os.File, os.FileInfo) error {
	return errors.New("persistent sysctl ownership is unsupported on this OS")
}
func sameSysctlFileOwner(os.FileInfo, os.FileInfo) bool { return false }
