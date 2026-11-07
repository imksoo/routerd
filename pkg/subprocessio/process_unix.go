// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package subprocessio

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Run owns a synchronous plugin invocation and its process group. Scripts may
// spawn CLI children, so cancellation must reach the group and waiting for an
// inherited output pipe must be bounded. Remote requests may still have taken
// effect; the provider journal must preserve that uncertainty.
func Run(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	killGroup := func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = killGroup
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	// A script that exited cannot leave background commands as a second owner.
	if cmd.Process != nil {
		cleanupErr := killGroup()
		if !errors.Is(cleanupErr, os.ErrProcessDone) {
			err = errors.Join(err, cleanupErr)
		}
	}
	return err
}
