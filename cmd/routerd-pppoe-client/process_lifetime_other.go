// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package main

import (
	"errors"
	"os/exec"
)

func bindPPPoEChildLifetime(*exec.Cmd) error {
	return errors.New("PPPoE native sessions are supported on Linux and FreeBSD only")
}
