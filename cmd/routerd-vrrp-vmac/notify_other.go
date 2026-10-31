//go:build !linux

// SPDX-License-Identifier: BSD-3-Clause

package main

import "errors"

func notifyRouterdVRRPTransition(commandRunner) error {
	return errors.New("VRRP main-process notification is supported on Linux only")
}
