// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"github.com/imksoo/routerd/pkg/dpi"
	"strings"
	"testing"
	"time"
)

func TestAgentBidirectionalFlowCachePreservesCurrentPacketIdentity(t *testing.T) {
	a := newAgent(options{}, fakeBackend{result: dpi.ClassifyResult{AppName: "tls", Engine: "ndpi-agent"}})
	req := dpi.ClassifyRequest{TransportProtocol: "tcp", SrcAddress: "2001:db8::1", SrcPort: 50000, DstAddress: "2001:db8::2", DstPort: 443}
	first := a.Observe(t.Context(), req, time.Now())
	req.SrcAddress, req.DstAddress = req.DstAddress, req.SrcAddress
	req.SrcPort, req.DstPort = req.DstPort, req.SrcPort
	second := a.Observe(t.Context(), req, time.Now())
	if first.SrcAddress != req.DstAddress || second.SrcAddress != req.SrcAddress || second.SrcPort != 443 || second.DstPort != 50000 {
		t.Fatalf("cached packet identity: %+v %+v", first, second)
	}
	if stats := a.Status().Stats; stats.BackendPackets != 1 || stats.ActiveFlows != 1 {
		t.Fatalf("reverse tuple created another native flow: %+v", stats)
	}
}

func TestUnavailableNDPIResultContainsNoBuiltinClassification(t *testing.T) {
	r := metadataOnlyResult(dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("example.test")})
	if r.AppName != "unknown" || r.ApplicationProtocol != "" || r.AppConfidence != 0 || r.Confidence != 0 || r.TLSSNI != "" || r.Metadata["tls.sni"] != "" {
		t.Fatalf("unavailable backend claims app: %+v", r)
	}
}

func TestPacketForNDPIDoesNotWrapSyntheticLengthsOrPorts(t *testing.T) {
	req := dpi.ClassifyRequest{TransportProtocol: "udp", SrcAddress: "192.0.2.1", DstAddress: "198.51.100.1", SrcPort: 65536, DstPort: 53}
	if _, err := packetForNDPI(req); err == nil {
		t.Fatal("wrapped invalid port")
	}
	req.SrcPort = 12345
	req.Payload = make([]byte, 65535)
	if _, err := packetForNDPI(req); err == nil {
		t.Fatal("wrapped oversized IP payload")
	}
	req.Payload = nil
	if p, err := packetForNDPI(req); err != nil || len(p) != 28 {
		t.Fatalf("empty UDP: %d %v", len(p), err)
	}
}

type blockingBackend struct {
	entered, release chan struct{}
	calls            int
}

func (b *blockingBackend) Status() backendStatus { return backendStatus{Loaded: true} }
func (b *blockingBackend) Forget(string)         {}
func (b *blockingBackend) Close()                {}
func (b *blockingBackend) Classify(context.Context, string, dpi.ClassifyRequest, *flowState) (dpi.ClassifyResult, error) {
	b.calls++
	close(b.entered)
	<-b.release
	return dpi.ClassifyResult{AppName: "tls"}, nil
}

func TestAgentCancellationCannotStartAnotherNativeFlowDuringOwnerMutation(t *testing.T) {
	b := &blockingBackend{entered: make(chan struct{}), release: make(chan struct{})}
	a := newAgent(options{}, b)
	req := dpi.ClassifyRequest{TransportProtocol: "tcp", SrcAddress: "192.0.2.1", SrcPort: 12345, DstAddress: "198.51.100.1", DstPort: 443}
	firstDone := make(chan struct{})
	go func() { a.Observe(t.Context(), req, time.Now()); close(firstDone) }()
	<-b.entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	secondDone := make(chan dpi.ClassifyResult, 1)
	go func() { secondDone <- a.Observe(ctx, req, time.Now()) }()
	close(b.release)
	<-firstDone
	second := <-secondDone
	if b.calls != 1 || !strings.Contains(second.Reason, "canceled") {
		t.Fatalf("native owner overlap/cancellation: calls=%d result=%+v", b.calls, second)
	}
}

func TestAgentControlPacketsDoNotConsumePayloadInspectionBudget(t *testing.T) {
	a := newAgent(options{firstPayloadPackets: 1}, fakeBackend{})
	req := dpi.ClassifyRequest{TransportProtocol: "tcp", SrcAddress: "192.0.2.1", SrcPort: 12345, DstAddress: "198.51.100.1", DstPort: 443}
	a.Observe(t.Context(), req, time.Now())
	a.Observe(t.Context(), req, time.Now())
	req.L4Payload = dpi.MinimalTLSClientHello("payload.example")
	result := a.Observe(t.Context(), req, time.Now())
	if result.AppName != "tls" || result.TLSSNI != "payload.example" {
		t.Fatalf("control packets exhausted payload budget: %+v", result)
	}
}

func TestAgentRetainedFlowLimitAppliesImmediatelyAndEvictsOldest(t *testing.T) {
	a := newAgent(options{flowLimit: 1}, fakeBackend{})
	req := dpi.ClassifyRequest{TransportProtocol: "tcp", SrcAddress: "192.0.2.1", SrcPort: 12345, DstAddress: "198.51.100.1", DstPort: 443}
	a.Observe(t.Context(), req, time.Now())
	req.SrcPort++
	a.Observe(t.Context(), req, time.Now())
	if stats := a.Status().Stats; stats.ActiveFlows != 1 || stats.PrunedFlows != 1 {
		t.Fatalf("retained flow limit: %+v", stats)
	}
}
