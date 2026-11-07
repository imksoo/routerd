// SPDX-License-Identifier: BSD-3-Clause
//go:build !linux && !freebsd

package freebsdpcap

import (
	"fmt"
	"os/exec"
)

func bindCaptureChildLifetime(_ *exec.Cmd) error {
	return fmt.Errorf("capture child lifetime is unsupported on this platform")
}
