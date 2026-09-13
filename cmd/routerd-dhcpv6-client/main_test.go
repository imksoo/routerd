// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	daemon := &dhcpv6Daemon{opts: options{resource: "wan-pd"}, client: client}
	daemon.initTelemetry()

	now = client.NextSolicitRetryAt()
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
				Resource:      "wan-pd",
				Interface:     "wan-vmac",
				State:         state,
				CurrentPrefix: "2001:db8:1220::/60",
				ServerDUID:    "00030001010203040506",
				ClientDUID:    "0003000102005e000113",
				IAID:          1,
				T1Seconds:     7200,
				T2Seconds:     12600,
				Preferred:     14400,
				Valid:         14400,
				AcquiredAt:    now,
				ExpiresAt:     now.Add(4 * time.Hour),
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
				Resource:      "wan-pd",
				Interface:     "wan-vmac",
				State:         tt.state,
				CurrentPrefix: "2001:db8:1220::/60",
				ServerDUID:    "00030001010203040506",
				ClientDUID:    "0003000102005e000113",
				IAID:          1,
				T1Seconds:     7200,
				T2Seconds:     12600,
				Preferred:     14400,
				Valid:         14400,
				AcquiredAt:    now.Add(-2 * time.Hour),
				ExpiresAt:     now.Add(2 * time.Hour),
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
	sent []pdclient.OutboundPacket
}

func (m *daemonMemoryTransport) Send(_ context.Context, packet pdclient.OutboundPacket) error {
	m.sent = append(m.sent, packet)
	return nil
}
