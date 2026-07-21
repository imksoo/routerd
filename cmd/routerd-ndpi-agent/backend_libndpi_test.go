// SPDX-License-Identifier: BSD-3-Clause

//go:build (linux || freebsd) && cgo && libndpi

package main

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dpi"
)

func TestLibNDPIBackendClassifiesTLSPacket(t *testing.T) {
	agent := newAgent(options{flowTTL: time.Hour, flowLimit: 100, firstPayloadPackets: 3}, nil)
	if !agent.Status().LibNDPILoaded {
		t.Skip("libndpi backend is not loaded")
	}
	payload := dpi.MinimalTLSClientHello("routerd.example")
	packet := append([]byte{
		0x45, 0x00, 0x00, 0x00, 0, 0, 0, 0, 64, 6, 0, 0,
		172, 18, 0, 101,
		198, 51, 100, 10,
		0xcf, 0xb0, 0x01, 0xbb,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0x50, 0x18, 0, 0, 0, 0, 0, 0,
	}, payload...)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	got := agent.Observe(context.Background(), dpi.ClassifyRequest{Packet: packet}, time.Unix(100, 0))
	if got.Engine != "ndpi-agent" || got.Source != "ndpi-agent" {
		t.Fatalf("classification source = %+v", got)
	}
	if got.AppName == "" || got.AppName == "unknown" {
		t.Fatalf("classification = %+v", got)
	}
}
