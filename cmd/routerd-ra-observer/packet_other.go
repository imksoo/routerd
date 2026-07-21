// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package main

import "fmt"

type packetSocket struct{}

func openPacketSocket(ifname string) (*packetSocket, error) {
	return nil, fmt.Errorf("RA observer is only implemented on Linux; interface %s cannot be observed", ifname)
}

func (s *packetSocket) read(frame []byte) (int, error) {
	return 0, fmt.Errorf("RA observer is not implemented on this platform")
}

func (s *packetSocket) close() error {
	return nil
}
