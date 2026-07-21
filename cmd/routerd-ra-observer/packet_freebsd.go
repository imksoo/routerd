// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"sync"

	"github.com/imksoo/routerd/internal/freebsdpcap"
)

type packetSocket struct {
	capture   *freebsdpcap.Reader
	closeOnce sync.Once
	closeErr  error
}

func openPacketSocket(ifname string) (*packetSocket, error) {
	capture, err := freebsdpcap.Open(ifname)
	if err != nil {
		return nil, err
	}
	return &packetSocket{capture: capture}, nil
}

func (s *packetSocket) read(frame []byte) (int, error) { return s.capture.Read(frame) }

func (s *packetSocket) close() error {
	s.closeOnce.Do(func() { s.closeErr = s.capture.Close() })
	return s.closeErr
}
