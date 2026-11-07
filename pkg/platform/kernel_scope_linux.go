// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package platform

import (
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// KernelNetworkScope identifies the live kernel and network namespace, rather
// than a configurable state directory or interface name.
func KernelNetworkScope(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	boot := strings.TrimSpace(string(data))
	var namespace unix.Stat_t
	if err := unix.Stat("/proc/self/ns/net", &namespace); err != nil {
		return "", err
	}
	if boot == "" || namespace.Ino == 0 {
		return "", fmt.Errorf("kernel network identity is unavailable")
	}
	return fmt.Sprintf("linux:%s:%d", boot, namespace.Ino), nil
}
