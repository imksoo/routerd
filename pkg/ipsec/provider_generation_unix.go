// SPDX-License-Identifier: BSD-3-Clause

//go:build linux || freebsd

package ipsec

import (
	"fmt"
	"os"
	"syscall"
)

// providerGeneration identifies the VICI endpoint instance which accepted a
// reconciliation. A new socket inode proves a new provider instance; an
// unchanged inode makes no claim about provider continuity.
func providerGeneration(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("unsupported VICI socket metadata")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
