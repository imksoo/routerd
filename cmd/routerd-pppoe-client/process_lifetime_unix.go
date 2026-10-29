// SPDX-License-Identifier: BSD-3-Clause

//go:build linux || freebsd

package main

import (
	"os/exec"
	"syscall"
)

func bindPPPoEChildLifetime(cmd *exec.Cmd) error {
	// Both supported native backends remain in the foreground. An abrupt daemon
	// death must also terminate the process which owns the negotiated interface.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	return nil
}
