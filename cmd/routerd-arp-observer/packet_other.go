// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package main

import (
	"context"
	"fmt"
)

type packetSocket struct{}

func openPacketSocket(_ context.Context, ifname string) (*packetSocket, error) {
	return nil, fmt.Errorf("ARP observer is supported on Linux only (interface %s)", ifname)
}

func (s *packetSocket) read(_ []byte) (int, error) {
	return 0, fmt.Errorf("ARP observer is supported on Linux only")
}

func (s *packetSocket) write(_ []byte) (int, error) {
	return 0, fmt.Errorf("ARP observer is supported on Linux only")
}

func (s *packetSocket) close() error {
	return nil
}
