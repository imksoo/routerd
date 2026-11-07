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
		t.Fatal("native build did not load its required libndpi backend")
	}
	defer agent.Close()
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
	if got.Reason != "libndpi_protocol_detection" || got.TLSSNI != "routerd.example" || got.AppName == "" || got.AppName == "unknown" {
		t.Fatalf("classification = %+v", got)
	}
}

func TestLibNDPIBackendDNSHostnameIsNotTLSSNI(t *testing.T) {
	agent := newAgent(options{flowTTL: time.Hour, flowLimit: 100, firstPayloadPackets: 3}, nil)
	defer agent.Close()
	payload := []byte{0x12, 0x34, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
	packet := append([]byte{0x45, 0, 0, 0, 0, 0, 0, 0, 64, 17, 0, 0, 192, 0, 2, 10, 198, 51, 100, 53, 0xc0, 0x01, 0, 53, 0, 0, 0, 0}, payload...)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	binary.BigEndian.PutUint16(packet[24:26], uint16(len(packet)-20))
	got := agent.Observe(context.Background(), dpi.ClassifyRequest{Packet: packet}, time.Unix(100, 0))
	if got.Reason != "libndpi_protocol_detection" || got.DNSQuery != "example.com" || got.TLSSNI != "" || got.Metadata["tls.sni"] != "" {
		t.Fatalf("DNS name mislabeled as TLS SNI: %+v", got)
	}
}
