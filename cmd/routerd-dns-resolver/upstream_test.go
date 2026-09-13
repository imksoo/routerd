// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/pkg/api"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
)

func TestZoneAnswerMatchesResourceRef(t *testing.T) {
	table := newZoneTable([]resolvercfg.RuntimeZone{{
		Name: "lan-zone",
		Spec: api.DNSZoneSpec{
			Zone: "lab.example",
			Records: []api.DNSZoneRecordSpec{{
				Hostname: "router",
				IPv4:     "192.168.160.5",
			}},
		},
	}})
	req := new(dns.Msg)
	req.SetQuestion("router.lab.example.", dns.TypeA)
	resp, ok := table.Answer(req, []string{"DNSZone/lan-zone"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("Answer ok=%v resp=%v", ok, resp)
	}
}

func TestZoneAnswerWildcard(t *testing.T) {
	table := newZoneTable([]resolvercfg.RuntimeZone{{
		Name: "apps-zone",
		Spec: api.DNSZoneSpec{
			Zone: "apps.lain.internal",
			Records: []api.DNSZoneRecordSpec{{
				Hostname: "*",
				IPv4:     "10.250.0.30",
			}},
		},
	}})
	// A name with no exact record matches the wildcard.
	req := new(dns.Msg)
	req.SetQuestion("birdclaw.apps.lain.internal.", dns.TypeA)
	resp, ok := table.Answer(req, []string{"DNSZone/apps-zone"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("wildcard Answer ok=%v resp=%v", ok, resp)
	}
	if a, isA := resp.Answer[0].(*dns.A); !isA || a.A.String() != "10.250.0.30" {
		t.Fatalf("wildcard answer = %v, want 10.250.0.30", resp.Answer[0])
	}
	// A deeper name also matches the apex wildcard.
	deep := new(dns.Msg)
	deep.SetQuestion("a.b.apps.lain.internal.", dns.TypeA)
	resp, ok = table.Answer(deep, []string{"DNSZone/apps-zone"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("deep wildcard Answer ok=%v resp=%v", ok, resp)
	}
	// AAAA for an A-only wildcard name is NODATA (NOERROR, empty), not NXDOMAIN,
	// so dual-stack clients do not negative-cache the name.
	aaaa := new(dns.Msg)
	aaaa.SetQuestion("birdclaw.apps.lain.internal.", dns.TypeAAAA)
	resp, ok = table.Answer(aaaa, []string{"DNSZone/apps-zone"})
	if !ok || resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 0 {
		t.Fatalf("AAAA NODATA expected NOERROR+empty, got ok=%v rcode=%v answers=%d", ok, resp.Rcode, len(resp.Answer))
	}
}

func TestZoneRestoresStickyHostRecordAfterRestart(t *testing.T) {
	table := newZoneTable([]resolvercfg.RuntimeZone{{
		Name: "home",
		Spec: api.DNSZoneSpec{
			Zone:        "home.internal",
			DHCPDerived: api.DNSZoneDHCPDerivedSpec{Sources: []string{"DHCPv4Server/lan-dhcpv4"}, TTL: 60},
		},
		DHCPHostRecords: []resolvercfg.RuntimeDHCPHostRecord{{
			Hostname: "MacBookProM5Max",
			IP:       "172.18.1.127",
		}},
	}})
	req := new(dns.Msg)
	req.SetQuestion("macbookprom5max.home.internal.", dns.TypeA)
	resp, ok := table.Answer(req, []string{"DNSZone/home"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("restored answer ok=%v resp=%v", ok, resp)
	}
	if a, isA := resp.Answer[0].(*dns.A); !isA || a.A.String() != "172.18.1.127" {
		t.Fatalf("restored answer = %v", resp.Answer[0])
	}

	ptrReq := new(dns.Msg)
	ptrReq.SetQuestion("127.1.18.172.in-addr.arpa.", dns.TypePTR)
	ptrResp, ok := table.Answer(ptrReq, []string{"DNSZone/home"})
	if !ok || len(ptrResp.Answer) != 1 {
		t.Fatalf("restored PTR ok=%v resp=%v", ok, ptrResp)
	}
}

func TestZoneStickySnapshotKeepsFirstDuplicateHostname(t *testing.T) {
	table := newZoneTable([]resolvercfg.RuntimeZone{{
		Name: "home",
		Spec: api.DNSZoneSpec{
			Zone:        "home.internal",
			DHCPDerived: api.DNSZoneDHCPDerivedSpec{Sources: []string{"DHCPv4Server/lan-dhcpv4"}, TTL: 60},
		},
		DHCPHostRecords: []resolvercfg.RuntimeDHCPHostRecord{
			{Hostname: "MacBookProM5Max", IP: "172.18.1.127"},
			{Hostname: "MacBookProM5Max", IP: "172.18.1.181"},
		},
	}})

	req := new(dns.Msg)
	req.SetQuestion("macbookprom5max.home.internal.", dns.TypeA)
	resp, ok := table.Answer(req, []string{"DNSZone/home"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("restored answer ok=%v resp=%v", ok, resp)
	}
	if a, isA := resp.Answer[0].(*dns.A); !isA || a.A.String() != "172.18.1.127" {
		t.Fatalf("duplicate sticky answer = %v, want first record 172.18.1.127", resp.Answer[0])
	}
}

func TestZoneActiveLeaseTakesPrecedenceOverStickyHostRecord(t *testing.T) {
	dir := t.TempDir()
	leaseFile := dir + "/dnsmasq.leases"
	if err := os.WriteFile(leaseFile, []byte("1789306800 fc:b2:14:8c:49:1a 172.18.1.127 MacBookProM5Max *\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	table := newZoneTable([]resolvercfg.RuntimeZone{{
		Name: "home",
		Spec: api.DNSZoneSpec{
			Zone: "home.internal",
			DHCPDerived: api.DNSZoneDHCPDerivedSpec{
				Sources:   []string{"DHCPv4Server/lan-dhcpv4"},
				LeaseFile: leaseFile,
			},
		},
		DHCPHostRecords: []resolvercfg.RuntimeDHCPHostRecord{{
			Hostname: "MacBookProM5Max",
			IP:       "172.18.1.181",
		}},
	}})
	req := new(dns.Msg)
	req.SetQuestion("macbookprom5max.home.internal.", dns.TypeA)
	resp, ok := table.Answer(req, []string{"DNSZone/home"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("active lease answer ok=%v resp=%v", ok, resp)
	}
	if a, isA := resp.Answer[0].(*dns.A); !isA || a.A.String() != "172.18.1.127" {
		t.Fatalf("active lease was replaced by sticky recovery: %v", resp.Answer[0])
	}
}

func TestZoneStaticPTRTakesPrecedenceOverStickyHostRecord(t *testing.T) {
	table := newZoneTable([]resolvercfg.RuntimeZone{{
		Name: "home",
		Spec: api.DNSZoneSpec{
			Zone:        "home.internal",
			DHCPDerived: api.DNSZoneDHCPDerivedSpec{Sources: []string{"DHCPv4Server/lan-dhcpv4"}},
			Records: []api.DNSZoneRecordSpec{{
				Hostname: "declared-name",
				IPv4:     "172.18.1.127",
			}},
		},
		DHCPHostRecords: []resolvercfg.RuntimeDHCPHostRecord{{
			Hostname: "sticky-name",
			IP:       "172.18.1.127",
		}},
	}})

	ptrReq := new(dns.Msg)
	ptrReq.SetQuestion("127.1.18.172.in-addr.arpa.", dns.TypePTR)
	resp, ok := table.Answer(ptrReq, []string{"DNSZone/home"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("static PTR missing: ok=%v resp=%v", ok, resp)
	}
	ptr, isPTR := resp.Answer[0].(*dns.PTR)
	if !isPTR || ptr.Ptr != "declared-name.home.internal." {
		t.Fatalf("PTR = %v, want declared-name.home.internal.", resp.Answer[0])
	}
}

func TestZoneActiveLeasePTRTakesPrecedenceOverStickyHostRecord(t *testing.T) {
	dir := t.TempDir()
	leaseFile := dir + "/dnsmasq.leases"
	if err := os.WriteFile(leaseFile, []byte("1789306800 fc:b2:14:8c:49:1a 172.18.1.127 active-name *\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	table := newZoneTable([]resolvercfg.RuntimeZone{{
		Name: "home",
		Spec: api.DNSZoneSpec{
			Zone: "home.internal",
			DHCPDerived: api.DNSZoneDHCPDerivedSpec{
				Sources:   []string{"DHCPv4Server/lan-dhcpv4"},
				LeaseFile: leaseFile,
			},
		},
		DHCPHostRecords: []resolvercfg.RuntimeDHCPHostRecord{{
			Hostname: "sticky-name",
			IP:       "172.18.1.127",
		}},
	}})

	ptrReq := new(dns.Msg)
	ptrReq.SetQuestion("127.1.18.172.in-addr.arpa.", dns.TypePTR)
	resp, ok := table.Answer(ptrReq, []string{"DNSZone/home"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("active lease PTR missing: ok=%v resp=%v", ok, resp)
	}
	ptr, isPTR := resp.Answer[0].(*dns.PTR)
	if !isPTR || ptr.Ptr != "active-name.home.internal." {
		t.Fatalf("PTR = %v, want active-name.home.internal.", resp.Answer[0])
	}
}

func TestParseDNSUpstreamDefaults(t *testing.T) {
	tests := []struct {
		raw     string
		scheme  string
		address string
	}{
		{"https://dns.example/dns-query", "https", ""},
		{"tls://dns.example", "tls", "dns.example:853"},
		{"quic://dns.example:8853", "quic", "dns.example:8853"},
		{"udp://[2001:db8::53]", "udp", "[2001:db8::53]:53"},
		{"2001:db8::53", "udp", "[2001:db8::53]:53"},
		{"192.0.2.53", "udp", "192.0.2.53:53"},
	}
	for i, tt := range tests {
		upstream, err := parseDNSUpstream(i, tt.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", tt.raw, err)
		}
		if upstream.Scheme != tt.scheme || upstream.Address != tt.address {
			t.Fatalf("parse %q = scheme %q address %q", tt.raw, upstream.Scheme, upstream.Address)
		}
	}
}

func TestUpstreamPoolFallsBackToSecondUpstream(t *testing.T) {
	failed, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatal(err)
	}
	defer failed.Close()
	ok, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatal(err)
	}
	defer ok.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := ok.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = ok.WriteTo(buf[:n], addr)
		}
	}()

	pool, err := newUpstreamPool([]string{"udp://" + failed.LocalAddr().String(), "udp://" + ok.LocalAddr().String()}, upstreamPoolConfig{
		ProbeInterval: time.Hour,
		ProbeTimeout:  50 * time.Millisecond,
		FailThreshold: 1,
		PassThreshold: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	query := dnsProbeQuery()
	resp, err := pool.Exchange(context.Background(), query, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp) != string(query) {
		t.Fatalf("response = %x, want %x", resp, query)
	}
	snapshot := pool.Snapshot()
	if snapshot[0].Phase != upstreamDown || snapshot[1].Phase != upstreamHealthy {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestLengthPrefixedExchange(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() {
		var length uint16
		if err := binary.Read(server, binary.BigEndian, &length); err != nil {
			t.Error(err)
			return
		}
		query := make([]byte, length)
		if _, err := io.ReadFull(server, query); err != nil {
			t.Error(err)
			return
		}
		_ = binary.Write(server, binary.BigEndian, uint16(len(query)))
		_, _ = server.Write(query)
	}()
	query := dnsProbeQuery()
	resp, err := exchangeLengthPrefixed(context.Background(), client, query)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp) != string(query) {
		t.Fatalf("response = %x, want %x", resp, query)
	}
}
