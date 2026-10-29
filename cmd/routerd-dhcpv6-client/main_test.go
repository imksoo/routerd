// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/pdclient"
)

func TestRestoreLeaseIgnoresEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("write lease: %v", err)
	}
	daemon := &dhcpv6Daemon{opts: options{leaseFile: path}}
	if err := daemon.restoreLease(context.Background()); err != nil {
		t.Fatalf("restore empty lease: %v", err)
	}
}

func TestSelectLinkLocalIPv6(t *testing.T) {
	got, err := selectLinkLocalIPv6([]net.Addr{
		&net.IPNet{IP: net.ParseIP("2001:db8::10"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("fe80::10"), Mask: net.CIDRMask(64, 128)},
	})
	if err != nil {
		t.Fatalf("select link-local: %v", err)
	}
	if got != "fe80::10" {
		t.Fatalf("link-local = %q, want fe80::10", got)
	}
}

func TestSelectLinkLocalIPv6RequiresLinkLocalAddress(t *testing.T) {
	_, err := selectLinkLocalIPv6([]net.Addr{
		&net.IPNet{IP: net.ParseIP("2001:db8::10"), Mask: net.CIDRMask(64, 128)},
	})
	if err == nil || !strings.Contains(err.Error(), "link-local") {
		t.Fatalf("error = %v, want missing link-local", err)
	}
}

func TestLinkLocalFromMAC(t *testing.T) {
	if got, want := linkLocalFromMAC(net.HardwareAddr{0x02, 0x00, 0x5e, 0x00, 0x01, 0x13}), "fe80::5eff:fe00:113"; got != want {
		t.Fatalf("linkLocalFromMAC() = %q, want %q", got, want)
	}
}

func TestAddressUsableInIPOutputIgnoresOtherTentativeAddresses(t *testing.T) {
	output := "7: wan-vmac    inet6 2409:10:3d60:1221::21/128 scope global tentative dadfailed\n" +
		"7: wan-vmac    inet6 fe80::5eff:fe00:113/64 scope link nodad\n"
	if !addressUsableInIPOutput(output, "fe80::5eff:fe00:113") {
		t.Fatal("usable link-local address was rejected because another address is tentative")
	}
	if addressUsableInIPOutput(output, "2409:10:3d60:1221::21") {
		t.Fatal("tentative address must remain unusable")
	}
}

func TestAddressUsableInIfconfigOutput(t *testing.T) {
	output := "em0: flags=8843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST> metric 0 mtu 1500\n" +
		"\tinet6 fe80::1%em0 prefixlen 64 scopeid 0x1 <link>\n" +
		"\tinet6 fe80::2%em0 prefixlen 64 scopeid 0x1 <link> tentative\n" +
		"\tinet6 fe80::3%em0 prefixlen 64 scopeid 0x1 <link>\n"
	if !addressUsableInIfconfigOutput(output, "fe80::3") {
		t.Fatal("second ready link-local address was not accepted")
	}
	if addressUsableInIfconfigOutput(output, "fe80::2") {
		t.Fatal("tentative target address was accepted")
	}
	if addressUsableInIfconfigOutput(output, "fe80::4") {
		t.Fatal("absent target address was accepted")
	}
	if !addressUsableInIfconfigOutput(output, "fe80::1") {
		t.Fatal("ready address was affected by another tentative address")
	}
}

func TestDHCPv6ListenAddressesAreInterfaceScoped(t *testing.T) {
	first, err := dhcpv6ListenAddr("fe80::10", "wan0", 546)
	if err != nil {
		t.Fatalf("first listen address: %v", err)
	}
	second, err := dhcpv6ListenAddr("fe80::20", "wan1", 546)
	if err != nil {
		t.Fatalf("second listen address: %v", err)
	}
	if first.IP.IsUnspecified() || second.IP.IsUnspecified() {
		t.Fatalf("DHCPv6 client bind must not be wildcard: first=%v second=%v", first, second)
	}
	if first.Zone != "wan0" || second.Zone != "wan1" || first.IP.Equal(second.IP) {
		t.Fatalf("scoped addresses = %v, %v", first, second)
	}
}

func TestDaemonTickRetransmitsSolicit(t *testing.T) {
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	transport := &daemonMemoryTransport{}
	client, err := pdclient.New(pdclient.Config{
		Resource:    "wan-pd",
		Interface:   "wan-vmac",
		ClientDUID:  []byte{0, 3, 0, 1, 2, 0, 0, 0, 1, 3},
		IAID:        1,
		Now:         func() time.Time { return now },
		Transaction: func() (uint32, error) { return 0x010203, nil },
		Random:      func() float64 { return 0.5 },
	}, transport)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("start client: %v", err)
	}
	dir := t.TempDir()
	daemon := &dhcpv6Daemon{opts: options{resource: "wan-pd", leaseFile: filepath.Join(dir, "lease.json"), eventFile: filepath.Join(dir, "events.jsonl")}, client: client}
	daemon.cond = sync.NewCond(&daemon.mu)
	daemon.initTelemetry()

	now = client.NextRetryAt()
	if err := daemon.tick(context.Background()); err != nil {
		t.Fatalf("daemon tick: %v", err)
	}
	if len(transport.sent) != 2 {
		t.Fatalf("sent packets = %d, want initial Solicit and one retry", len(transport.sent))
	}
	if transport.sent[0].Message.TransactionID != transport.sent[1].Message.TransactionID {
		t.Fatalf("retry changed transaction ID: first=%06x retry=%06x", transport.sent[0].Message.TransactionID, transport.sent[1].Message.TransactionID)
	}
}

func TestNextReadDeadlineUsesSolicitRetry(t *testing.T) {
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	retryAt := now.Add(time.Second)
	if got := nextReadDeadline(context.Background(), now, 3*time.Second, retryAt); !got.Equal(retryAt) {
		t.Fatalf("deadline = %s, want retry at %s", got, retryAt)
	}
}

func TestRefreshingAndRebindingLeaseRemainDependencyReady(t *testing.T) {
	now := time.Date(2026, 9, 13, 4, 38, 41, 0, time.UTC)
	for _, state := range []pdclient.State{pdclient.StateBound, pdclient.StateRenewing, pdclient.StateRebinding} {
		t.Run(string(state), func(t *testing.T) {
			if got := resourcePhase(state); got != "Bound" {
				t.Fatalf("resource phase = %q, want Bound", got)
			}
			client, err := pdclient.New(pdclient.Config{
				Resource:   "wan-pd",
				Interface:  "wan-vmac",
				ClientDUID: []byte{0, 3, 0, 1, 2, 0, 0x5e, 0, 1, 0x13},
				IAID:       1,
				Now:        func() time.Time { return now },
			}, &daemonMemoryTransport{})
			if err != nil {
				t.Fatal(err)
			}
			client.Restore(pdclient.Snapshot{
				Resource:       "wan-pd",
				Interface:      "wan-vmac",
				State:          state,
				Prefixes:       []pdclient.PrefixSnapshot{{Prefix: "2001:db8:1220::/60", Preferred: 14400, Valid: 14400, AcquiredAt: now}},
				LeaseUpdatedAt: now,
				CurrentPrefix:  "2001:db8:1220::/60",
				ServerDUID:     "00030001010203040506",
				ClientDUID:     "0003000102005e000113",
				IAID:           1,
				T1Seconds:      7200,
				T2Seconds:      12600,
				Preferred:      14400,
				Valid:          14400,
				AcquiredAt:     now,
				ExpiresAt:      now.Add(4 * time.Hour),
			})
			daemon := &dhcpv6Daemon{opts: options{resource: "wan-pd"}, client: client, recorder: &packetRecorder{limit: 10}}
			daemon.initTelemetry()
			status := daemon.statusLocked().Resources[0]
			if status.Phase != "Bound" || status.Observed["leaseState"] != string(state) || status.Conditions[0].Status != "True" {
				t.Fatalf("status = %#v", status)
			}
		})
	}
}

func TestStartClientResumesRestoredLeaseExchange(t *testing.T) {
	now := time.Date(2026, 9, 13, 4, 38, 41, 0, time.UTC)
	tests := []struct {
		state   pdclient.State
		message uint8
	}{
		{state: pdclient.StateRenewing, message: pdclient.MessageRenew},
		{state: pdclient.StateRebinding, message: pdclient.MessageRebind},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			transport := &daemonMemoryTransport{}
			client, err := pdclient.New(pdclient.Config{
				Resource:    "wan-pd",
				Interface:   "wan-vmac",
				ClientDUID:  []byte{0, 3, 0, 1, 2, 0, 0x5e, 0, 1, 0x13},
				IAID:        1,
				Now:         func() time.Time { return now },
				Transaction: func() (uint32, error) { return 0x010203, nil },
				Random:      func() float64 { return 0.5 },
			}, transport)
			if err != nil {
				t.Fatal(err)
			}
			client.Restore(pdclient.Snapshot{
				Resource:       "wan-pd",
				Interface:      "wan-vmac",
				State:          tt.state,
				Prefixes:       []pdclient.PrefixSnapshot{{Prefix: "2001:db8:1220::/60", Preferred: 14400, Valid: 14400, AcquiredAt: now.Add(-2 * time.Hour)}},
				LeaseUpdatedAt: now.Add(-2 * time.Hour),
				CurrentPrefix:  "2001:db8:1220::/60",
				ServerDUID:     "00030001010203040506",
				ClientDUID:     "0003000102005e000113",
				IAID:           1,
				T1Seconds:      7200,
				T2Seconds:      12600,
				Preferred:      14400,
				Valid:          14400,
				AcquiredAt:     now.Add(-2 * time.Hour),
				ExpiresAt:      now.Add(2 * time.Hour),
			})
			daemon := &dhcpv6Daemon{
				opts:   options{resource: "wan-pd", leaseFile: filepath.Join(t.TempDir(), "lease.json")},
				client: client,
			}
			if err := daemon.startClient(context.Background()); err != nil {
				t.Fatalf("start client: %v", err)
			}
			if len(transport.sent) != 1 || transport.sent[0].Message.Type != tt.message {
				t.Fatalf("sent packets = %#v, want one message type %d", transport.sent, tt.message)
			}
			if client.NextRetryAt().IsZero() {
				t.Fatal("restored lease exchange did not schedule a retransmission")
			}
		})
	}
}

type daemonMemoryTransport struct {
	sent    []pdclient.OutboundPacket
	failure error
}

func (m *daemonMemoryTransport) Send(_ context.Context, packet pdclient.OutboundPacket) error {
	m.sent = append(m.sent, packet)
	return m.failure
}

func TestStopCommandCancelsTheDaemonOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	client, err := pdclient.New(pdclient.Config{Resource: "wan-pd", Interface: "wan0", ClientDUID: []byte{1}, IAID: 7}, &daemonMemoryTransport{})
	if err != nil {
		t.Fatal(err)
	}
	d := &dhcpv6Daemon{opts: options{leaseFile: filepath.Join(dir, "lease.json"), eventFile: filepath.Join(dir, "events.jsonl")}, client: client, cancel: cancel}
	d.cond = sync.NewCond(&d.mu)
	response := httptest.NewRecorder()
	d.handleCommand(response, httptest.NewRequest(http.MethodPost, "/v1/commands/stop", strings.NewReader("")))
	var result daemonapi.CommandResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !result.Accepted || ctx.Err() != context.Canceled {
		t.Fatalf("stop did not cancel the worker: %+v", result)
	}
}

func TestReceivePathPersistsAllPrefixesAndExplicitRevocation(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	transport := &daemonMemoryTransport{}
	xid := uint32(20)
	client, err := pdclient.New(pdclient.Config{Resource: "wan-pd", Interface: "wan0", ClientDUID: []byte{1}, IAID: 7, Now: func() time.Time { return now }, Transaction: func() (uint32, error) { xid++; return xid, nil }, Random: func() float64 { return .5 }}, transport)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	d := &dhcpv6Daemon{opts: options{resource: "wan-pd", ifname: "wan0", leaseFile: filepath.Join(dir, "lease.json"), eventFile: filepath.Join(dir, "events.jsonl")}, client: client, recorder: &packetRecorder{limit: 10}}
	d.cond = sync.NewCond(&d.mu)
	d.initTelemetry()
	sendReply := func(kind uint8, entries []pdclient.IAPrefix) {
		t.Helper()
		packet := transport.sent[len(transport.sent)-1]
		msg := pdclient.Message{Type: kind, TransactionID: packet.Message.TransactionID, ClientDUID: []byte{1}, ServerDUID: []byte{2}, IAPDs: []pdclient.IAPD{{IAID: 7, T1: 30, T2: 60, Prefixes: entries}}}
		payload, err := pdclient.EncodeMessage(msg)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.handlePayload(context.Background(), payload); err != nil {
			t.Fatal(err)
		}
	}
	load := func() pdclient.Snapshot {
		t.Helper()
		data, err := os.ReadFile(d.opts.leaseFile)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot pdclient.Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries := []pdclient.IAPrefix{{Prefix: netip.MustParsePrefix("2001:db8:1::/56"), Preferred: 90, Valid: 120}, {Prefix: netip.MustParsePrefix("2001:db8:2::/56"), Preferred: 100, Valid: 180}}
	sendReply(pdclient.MessageAdvertise, entries)
	sendReply(pdclient.MessageReply, entries)
	first := load()
	if len(first.Prefixes) != 2 || first.CurrentPrefix != "2001:db8:1::/56" {
		t.Fatalf("daemon flattened Reply: %+v", first)
	}
	now = now.Add(10 * time.Second)
	if err := client.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	sendReply(pdclient.MessageReply, []pdclient.IAPrefix{{Prefix: entries[0].Prefix, Preferred: 0, Valid: 0}})
	after := load()
	if len(after.Prefixes) != 1 || after.CurrentPrefix != "2001:db8:2::/56" || !after.Prefixes[0].AcquiredAt.Equal(first.Prefixes[1].AcquiredAt) || !after.ExpiresAt.Equal(now.Add(170*time.Second)) {
		t.Fatalf("receive/save path lost omitted lease: %+v", after)
	}
	// A secondary lifetime update alone must reach disk, even with the same
	// primary/state. This previously escaped snapshotChanged.
	if err := client.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	sendReply(pdclient.MessageReply, []pdclient.IAPrefix{{Prefix: entries[1].Prefix, Preferred: 100, Valid: 300}, entries[0]})
	before := load()
	now = now.Add(time.Second)
	if err := client.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	sendReply(pdclient.MessageReply, []pdclient.IAPrefix{{Prefix: entries[0].Prefix, Preferred: 0, Valid: 240}})
	final := load()
	if before.CurrentPrefix != final.CurrentPrefix || len(final.Prefixes) != 2 || final.Prefixes[0].Valid != 240 {
		t.Fatalf("secondary update not persisted: before=%+v after=%+v", before, final)
	}
}

func TestInformationCommandSchedulesAndPublishesOnlySuccessfulSend(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	transport := &daemonMemoryTransport{}
	client, err := pdclient.New(pdclient.Config{Resource: "wan-pd", Interface: "wan0", ClientDUID: []byte{1}, IAID: 7, Now: func() time.Time { return now }, Random: func() float64 { return .5 }}, transport)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	d := &dhcpv6Daemon{opts: options{resource: "wan-pd", leaseFile: filepath.Join(dir, "lease.json"), eventFile: filepath.Join(dir, "events.jsonl")}, client: client, recorder: &packetRecorder{limit: 10}}
	d.cond = sync.NewCond(&d.mu)
	d.initTelemetry()
	command := httptest.NewRecorder()
	d.handleCommand(command, httptest.NewRequest(http.MethodPost, "/v1/commands/info-request", nil))
	if command.Code != http.StatusOK || !client.Snapshot().InfoPending || len(transport.sent) != 0 {
		t.Fatalf("command did not schedule information exchange: %d %+v", command.Code, client.Snapshot())
	}
	for _, event := range d.events {
		if event.Type == daemonapi.EventDHCPv6InfoRequestSent {
			t.Fatal("published send before transport effect")
		}
	}
	now = client.NextRetryAt()
	if err := d.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range d.events {
		if event.Type == daemonapi.EventDHCPv6InfoRequestSent {
			count++
		}
	}
	if count != 1 || len(transport.sent) == 0 || transport.sent[0].Message.Type != pdclient.MessageInfoRequest {
		t.Fatal("actual information send missing from daemon events")
	}
	request := transport.sent[0].Message
	reply := pdclient.Message{Type: pdclient.MessageReply, TransactionID: request.TransactionID, ClientDUID: []byte{1}, ServerDUID: []byte{2}, StatusCode: pdclient.StatusUnspecFail}
	payload, err := pdclient.EncodeMessage(reply)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.handlePayload(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(d.opts.leaseFile)
	if err != nil {
		t.Fatal(err)
	}
	var saved pdclient.Snapshot
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.InfoStatusCode != pdclient.StatusUnspecFail || saved.InfoLastError == "" || !saved.InfoPending {
		t.Fatalf("status failure not persisted: %+v", saved)
	}
}

func TestDaemonKeepsTheRetryOwnerAfterTransportFailure(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	transport := &daemonMemoryTransport{failure: errors.New("network unreachable")}
	client, err := pdclient.New(pdclient.Config{Resource: "wan-pd", Interface: "wan0", ClientDUID: []byte{1}, IAID: 7, Now: func() time.Time { return now }, Random: func() float64 { return .5 }}, transport)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	d := &dhcpv6Daemon{opts: options{resource: "wan-pd", leaseFile: filepath.Join(dir, "lease.json"), eventFile: filepath.Join(dir, "events.jsonl")}, client: client, recorder: &packetRecorder{limit: 10}}
	d.cond = sync.NewCond(&d.mu)
	d.initTelemetry()
	if err := d.startClient(context.Background()); err != nil {
		t.Fatalf("temporary transport failure killed worker: %v", err)
	}
	snapshot := client.Snapshot()
	if snapshot.LastError == "" || !snapshot.LastSentAt.IsZero() || !client.NextRetryAt().After(now) || len(d.events) != 0 {
		t.Fatalf("failed send claimed success or lost retry: %+v", snapshot)
	}
	transport.failure = nil
	now = client.NextRetryAt()
	if err := d.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.Snapshot().LastSentAt.IsZero() || len(d.events) != 1 || d.events[0].Type != daemonapi.EventDHCPv6SolicitSent {
		t.Fatal("same worker did not recover through scheduled retry")
	}
}
