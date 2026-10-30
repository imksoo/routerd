// SPDX-License-Identifier: BSD-3-Clause
//go:build !linux && !freebsd

package main

import "errors"

func lockNativeOperation(string) (int, error) {
	return -1, errors.New("native operation ownership is unsupported on this platform")
}
func closeNativeOperation(int) error {
	return errors.New("native operation ownership is unsupported on this platform")
}
