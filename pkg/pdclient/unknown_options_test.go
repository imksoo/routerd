// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"context"
	"encoding/hex"
	"net/netip"
	"testing"
	"time"
)

func TestUnsupportedSIPOptionsPreserveInformationAndPDRenewal(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	c, _ := testClient(t, &now)
	ctx := context.Background()
	acquire(t, c, prefix("2001:db8:1::/56", 90, 120))
	priorPrefix := c.Lease.Prefixes[0]
	priorT1, priorT2 := c.Lease.T1, c.Lease.T2

	// Independent option bytes: SIP domain sip.example (21) and IPv6 server
	// 2001:db8::21 (22). Neither is requested or implemented by this client.
	// Put them before known options so accepting the packet requires walking
	// past both unsupported options, not merely ignoring trailing bytes.
	sip, err := hex.DecodeString("0015000d03736970076578616d706c65000016001020010db8000000000000000000000021")
	if err != nil {
		t.Fatal(err)
	}
	withSIP := func(msg Message) []byte {
		payload := mustPayload(t, msg)
		out := append([]byte{}, payload[:4]...)
		out = append(out, sip...)
		return append(out, payload[4:]...)
	}
	if err := c.InfoRequest(ctx); err != nil {
		t.Fatal(err)
	}
	now = c.infoExchange.retryAt
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	dns := netip.MustParseAddr("2001:db8::53")
	refresh := uint32(600)
	info := Message{Type: MessageReply, TransactionID: c.infoExchange.message.TransactionID,
		ClientDUID: c.Config.ClientDUID, ServerDUID: testServer,
		DNSServers: []netip.Addr{dns}, DomainSearch: []string{"example"},
		AFTRName: "aftr.example", InfoRefreshTime: &refresh}
	if err := c.Handle(ctx, withSIP(info)); err != nil {
		t.Fatal(err)
	}
	if c.infoExchange.active() || len(c.Info.DNSServers) != 1 || c.Info.DNSServers[0] != dns ||
		len(c.Info.DomainSearch) != 1 || c.Info.DomainSearch[0] != "example" ||
		c.Info.AFTRName != "aftr.example" || !c.Info.RefreshAt.Equal(now.Add(600*time.Second)) {
		t.Fatalf("unsupported options discarded usable information: %+v", c.Info)
	}
	if c.State != StateBound || len(c.Lease.Prefixes) != 1 || c.Lease.Prefixes[0] != priorPrefix || c.Lease.T1 != priorT1 || c.Lease.T2 != priorT2 {
		t.Fatalf("information exchange altered the existing PD lease: %+v", c.Lease)
	}

	now = now.Add(30 * time.Second)
	if err := c.Renew(ctx); err != nil {
		t.Fatal(err)
	}
	reply := response(c, MessageReply, prefix("2001:db8:1::/56", 180, 240))
	reply.DNSServers, reply.DomainSearch, reply.AFTRName = info.DNSServers, info.DomainSearch, info.AFTRName
	payload := withSIP(reply)
	// Unknown contents do not waive outer TLV bounds. A truncated option must
	// not complete this renewal or publish information from a partial packet.
	infoUpdated := c.Info.UpdatedAt
	if err := c.Handle(ctx, append(append([]byte{}, payload...), 0, 22, 0, 16, 0)); err == nil {
		t.Fatal("truncated unsupported option accepted")
	}
	if c.State != StateRenewing || !c.leaseExchange.active() || c.Lease.Prefixes[0] != priorPrefix || !c.Info.UpdatedAt.Equal(infoUpdated) {
		t.Fatal("truncated packet changed lease/information or completed renewal")
	}
	if err := c.Handle(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if c.State != StateBound || c.leaseExchange.active() || len(c.Lease.Prefixes) != 1 ||
		c.Lease.Prefixes[0].Prefix != priorPrefix.Prefix || !c.Lease.Prefixes[0].ExpiresAt().Equal(now.Add(240*time.Second)) ||
		len(c.Info.DNSServers) != 1 || c.Info.DNSServers[0] != dns || c.Info.AFTRName != "aftr.example" {
		t.Fatalf("unsupported options prevented PD renewal or lost DNS/AFTR: lease=%+v info=%+v", c.Lease, c.Info)
	}
}
