// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package freebsdpcap

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func bindCaptureChildLifetime(cmd *exec.Cmd) error {
	info, err := os.Stat(cmd.Path)
	if err != nil {
		return fmt.Errorf("capture executable: %w", err)
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return fmt.Errorf("set-ID capture executable would clear parent-death protection")
	}
	// Go's FreeBSD exec implementation applies PROC_PDEATHSIG_CTL before
	// exec and rechecks PPID to close the parent-exit setup race. The base
	// tcpdump stays in the foreground in this capture mode.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return nil
}
