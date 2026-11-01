// SPDX-License-Identifier: BSD-3-Clause

//go:build (linux || freebsd) && cgo && libndpi

package main

import (
	"context"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dpi"
)

func TestLibNDPIBackendClassifiesHTTPPacket(t *testing.T) {
	agent := newAgent(options{flowTTL: time.Hour, flowLimit: 100, firstPayloadPackets: 3}, nil)
	if !agent.Status().LibNDPILoaded {
		t.Skip("libndpi backend is not loaded")
	}
	packet := selftestHTTPPacket("routerd.example")
	got := agent.Observe(context.Background(), dpi.ClassifyRequest{Packet: packet}, time.Unix(100, 0))
	if got.Engine != "ndpi-agent" || got.Source != "ndpi-agent" {
		t.Fatalf("classification source = %+v", got)
	}
	if got.AppName == "" || got.AppName == "unknown" {
		t.Fatalf("classification = %+v", got)
	}
}
