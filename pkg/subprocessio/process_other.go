// SPDX-License-Identifier: BSD-3-Clause
//go:build !linux && !freebsd

package subprocessio

import (
	"fmt"
	"os/exec"
)

func Run(cmd *exec.Cmd) error {
	return fmt.Errorf("owned plugin process groups are unsupported on this platform")
}
