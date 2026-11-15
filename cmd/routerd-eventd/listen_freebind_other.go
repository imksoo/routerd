// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package main

import "net"

func listenConfig() net.ListenConfig {
	return net.ListenConfig{}
}
