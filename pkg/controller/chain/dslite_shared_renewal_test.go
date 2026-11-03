// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDSLiteSharedSourceRenewalKeepsValidHeldAddressAndTunnel(t *testing.T) {
	c, f, spec := newDSLiteEndpointFixture(t, true)
	source, _, err := c.delegatedEndpointSource(spec)
	if err != nil {
		t.Fatal(err)
	}
	f.present = source.Address + "/128"
	preferred, valid := nativePDAddressDeadlines(source.Lease, true)
	owner := delegatedNativeReceipt{IfName: "wan0", IfIndex: f.index, Scope: f.scope, PrefixDelegation: "wan-pd", Addresses: []delegatedNativeAddress{{Address: f.present, PreferredUntil: preferred, ValidUntil: valid, Held: true}}}
	if err := f.store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-source", map[string]any{"phase": "Applied", "dryRun": false, "ifname": "wan0", "address": f.present, "prefixSource": "wan-pd", "validUntil": pdLifetimeDeadline(source.Lease.LastReplyAt, source.Lease.ValidSeconds), "nativeReceipt": owner}); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := c.nativeTunnelReceipts()
	if err != nil || len(before) != 1 {
		t.Fatalf("initial tunnel: %v %v", before, err)
	}
	writes := len(f.native.writes)
	f.now = f.now.Add(20 * time.Second)
	f.changeLease(func(lease *routerstate.PDLease) { lease.LastReplyAt = f.now })
	// The address actor has not refreshed its shorter, still-valid native
	// lifetime yet. Renewing the same prefix must not mark the tunnel Pending,
	// fail its health probe, or move established sessions to another AFTR.
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := f.store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "dslite"); got["phase"] != "Up" {
		t.Fatalf("ordinary renewal disrupted the live tunnel: %v", got)
	}
	after, err := c.nativeTunnelReceipts()
	if err != nil || len(after) != 1 || after[0] != before[0] || len(f.native.writes) != writes || len(f.writes) != 0 {
		t.Fatalf("ordinary renewal replaced native ownership: before=%v after=%v native=%v addresses=%v err=%v", before, after, f.native.writes, f.writes, err)
	}
	current, _, err := c.delegatedEndpointSource(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.recheckTunnelLease(spec, source); err != nil {
		t.Fatalf("same-prefix positive renewal rejected after a wait: %v", err)
	}
	if ready, err := c.sharedEndpointReady(t.Context(), current); err != nil || !ready {
		t.Fatalf("valid held address rejected: %t %v", ready, err)
	}
	// A newly shortened authority must first clamp the native owner's lifetime.
	f.changeLease(func(lease *routerstate.PDLease) { lease.ValidSeconds = 30 })
	shortened, _, err := c.delegatedEndpointSource(spec)
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := c.sharedEndpointReady(t.Context(), shortened); err != nil || ready {
		t.Fatalf("native address outlives shortened authority: %t %v", ready, err)
	}
	if err := c.recheckTunnelLease(spec, current); err == nil {
		t.Fatal("shortened authority accepted without native lifetime clamp")
	}
}
