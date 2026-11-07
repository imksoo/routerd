// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resourcequery"
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
	if err != nil || len(after) != 1 || after[0] != before[0] || len(f.writes) != 0 {
		t.Fatalf("ordinary renewal replaced native ownership: before=%v after=%v native=%v addresses=%v err=%v", before, after, f.native.writes, f.writes, err)
	}
	for _, operation := range f.native.writes[writes:] {
		if strings.Contains(operation, "link add") || strings.Contains(operation, "delete") || strings.Contains(operation, "addr replace") {
			t.Fatalf("renewal recreated forwarding identity: %s", operation)
		}
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
	owner.claim(f.present, preferred, nativePDDeadline(current.Lease.LastReplyAt, current.Lease.ValidSeconds))
	if err := f.store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-source", map[string]any{"phase": "Pending", "reason": "AddressApplying", "dryRun": false, "ifname": "wan0", "address": f.present, "prefixSource": "wan-pd", "nativeReceipt": owner}); err != nil {
		t.Fatal(err)
	}
	if ready, err := c.sharedEndpointReady(t.Context(), current); err != nil || !ready {
		t.Fatalf("confirmed address rejected during its owner's refresh: %t %v", ready, err)
	}
	// A newly shortened authority must first clamp the native owner's lifetime.
	f.changeLease(func(lease *routerstate.PDLease) { lease.ValidSeconds = 30; lease.PreferredSeconds = 15 })
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
	f.now = valid.Add(time.Second)
	if ready, err := c.sharedEndpointReady(t.Context(), current); err != nil || ready {
		t.Fatalf("unconfirmed extension outlived the held acquisition: %t %v", ready, err)
	}
}

func TestLANHeldRenewalKeepsAppliedDependencyAndConfirmedLifetime(t *testing.T) {
	store, _, c, _ := sqliteNativeLANFixture(t)
	installLANIOFixture(&c)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	now := c.Now().Add(30 * time.Second)
	c.Now = func() time.Time { return now }
	lease, _, err := store.ReadPDLease("wan-pd")
	if err != nil {
		t.Fatal(err)
	}
	lease.LastReplyAt, lease.LeaseUpdatedAt, lease.SnapshotUpdatedAt = now, now, now
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	observer := &lanRenewalSaveObserver{SQLiteStore: store}
	c.Store = observer
	apply := c.Command
	observed := false
	c.Command = func(ctx context.Context, name string, args ...string) error {
		observed = true
		status := store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
		receipt, err := delegatedReceipt(status)
		if err != nil || receipt == nil || !receipt.ownedAt("2001:db8:1200:1::1/64", now) {
			t.Fatalf("valid held ownership lost: %v %v", status, err)
		}
		if status["phase"] != "Applied" || status["validUntil"] != before["validUntil"] {
			t.Fatalf("ordinary renewal dropped readiness or published an unconfirmed extension: %v", status)
		}
		ready, err := resourcequery.ReadDependenciesReady(store, []api.ResourceDependencySpec{{Resource: "IPv6DelegatedAddress/lan-address", Phase: "Applied"}})
		if err != nil || !ready {
			t.Fatalf("dependent forwarding was disabled during renewal: %t %v", ready, err)
		}
		return apply(ctx, name, args...)
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !observed {
		t.Fatal("renewal did not update the native lifetime")
	}
	for _, phase := range observer.phases {
		if phase != "Applied" {
			t.Fatalf("renewal published a transient unavailable dependency: phases=%v", observer.phases)
		}
	}
	after := store.ObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	if after["phase"] != "Applied" || after["validUntil"] != pdLifetimeDeadline(now, lease.ValidSeconds) {
		t.Fatalf("completed renewal was not published: %v", after)
	}
}

// Dependent actors can read any persisted intermediate state, including after
// the native command succeeds but before the final Applied publication.
type lanRenewalSaveObserver struct {
	*routerstate.SQLiteStore
	phases []string
}

func (s *lanRenewalSaveObserver) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if err := s.SQLiteStore.SaveObjectStatus(version, kind, name, status); err != nil {
		return err
	}
	if kind == "IPv6DelegatedAddress" && name == "lan-address" {
		phase, _ := status["phase"].(string)
		s.phases = append(s.phases, phase)
	}
	return nil
}

// Publish the renewal immediately after a reader obtains the old lease. This
// reproduces independent PD and address actors advancing during a tunnel check.
type dsliteRenewAfterReadStore struct {
	*dsliteEndpointFixtureStore
	afterRead func()
}

func (s *dsliteRenewAfterReadStore) ReadPDLease(name string) (routerstate.PDLease, bool, error) {
	lease, found, err := s.dsliteEndpointFixtureStore.ReadPDLease(name)
	if err == nil && found && s.afterRead != nil {
		action := s.afterRead
		s.afterRead = nil
		action()
	}
	return lease, found, err
}

func TestDSLiteSharedRenewalBetweenLeaseAndOwnerReadKeepsTunnel(t *testing.T) {
	c, f, spec := newDSLiteEndpointFixture(t, true)
	source, _, err := c.delegatedEndpointSource(spec)
	if err != nil {
		t.Fatal(err)
	}
	f.present = source.Address + "/128"
	preferred, valid := nativePDAddressDeadlines(source.Lease, true)
	owner := delegatedNativeReceipt{IfName: "wan0", IfIndex: f.index, Scope: f.scope, PrefixDelegation: "wan-pd", Addresses: []delegatedNativeAddress{{Address: f.present, PreferredUntil: preferred, ValidUntil: valid, Held: true}}}
	saveOwner := func() {
		t.Helper()
		if err := f.store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-source", map[string]any{"phase": "Applied", "dryRun": false, "ifname": "wan0", "address": f.present, "prefixSource": "wan-pd", "nativeReceipt": owner}); err != nil {
			t.Fatal(err)
		}
	}
	saveOwner()
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := c.nativeTunnelReceipts()
	if err != nil || len(before) != 1 {
		t.Fatalf("initial tunnel: %v %v", before, err)
	}
	writes := len(f.native.writes)
	c.Store = &dsliteRenewAfterReadStore{dsliteEndpointFixtureStore: f.store, afterRead: func() {
		f.now = f.now.Add(20 * time.Second)
		f.changeLease(func(lease *routerstate.PDLease) { lease.LastReplyAt = f.now })
		owner.Addresses[0].PreferredUntil = preferred.Add(30 * time.Second)
		owner.Addresses[0].ValidUntil = valid.Add(30 * time.Second)
		saveOwner()
	}}
	if err := c.requireTunnelInputs(t.Context(), before[0], spec); err != nil {
		t.Fatalf("same-prefix renewal withdrew a valid tunnel: %v; writes=%v", err, f.native.writes[writes:])
	}
	after, err := c.nativeTunnelReceipts()
	if err != nil || len(after) != 1 || after[0] != before[0] || len(f.native.writes) != writes {
		t.Fatalf("renewal changed tunnel ownership: before=%v after=%v writes=%v err=%v", before, after, f.native.writes[writes:], err)
	}
}
