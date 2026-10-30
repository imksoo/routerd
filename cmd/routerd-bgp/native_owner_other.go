// SPDX-License-Identifier: BSD-3-Clause
//go:build !linux && !freebsd

package main

import "errors"

func lockBGPProcess(string) (int, error) {
	return -1, errors.New("BGP process ownership is unsupported on this platform")
}

func closeBGPProcess(int) error {
	return errors.New("BGP process ownership is unsupported on this platform")
}
