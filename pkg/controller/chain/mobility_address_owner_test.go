// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"errors"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type addressOwnerStore struct {
	*routerstate.SQLiteStore
	failApplied error
	afterMerge  func(map[string]any)
}

func (s *addressOwnerStore) MergeObjectStatus(v, k, n string, u map[string]any) error {
	if _, ok := u["appliedMobilityStaticAddresses"]; ok && s.failApplied != nil {
		return s.failApplied
	}
	if err := s.SQLiteStore.MergeObjectStatus(v, k, n, u); err != nil {
		return err
	}
	if s.afterMerge != nil {
		s.afterMerge(u)
	}
	return nil
}

type addressOwnerNative struct {
	snapshot                   staticAddressSnapshot
	writes                     int
	readErr, addErr, deleteErr error
	afterObserve, afterCommand func()
}

func (n *addressOwnerNative) observe(context.Context, string) (staticAddressSnapshot, error) {
	observed := n.snapshot
	observed.Addresses = append([]netip.Prefix(nil), n.snapshot.Addresses...)
	if n.afterObserve != nil {
		n.afterObserve()
	}
	return observed, n.readErr
}
func (n *addressOwnerNative) command(_ context.Context, name string, args ...string) error {
	if name != "ip" || len(args) < 6 || args[0] != "-4" || args[1] != "addr" || args[4] != "dev" {
		return errors.New("unexpected address command")
	}
	n.writes++
	switch args[2] {
	case "add":
		if len(n.snapshot.Addresses) > 0 {
			return errors.New("exclusive add collision")
		}
		n.snapshot.Addresses = []netip.Prefix{netip.MustParsePrefix(args[3])}
		if n.afterCommand != nil {
			n.afterCommand()
		}
		return n.addErr
	case "del":
		n.snapshot.Addresses = nil
		if n.afterCommand != nil {
			n.afterCommand()
		}
		return n.deleteErr
	default:
		return errors.New("address acquisition must not replace")
	}
}
func mobilityAddressOwnerFixture(t *testing.T) (*IPv4StaticAddressController, *addressOwnerStore, *addressOwnerNative) {
	t.Helper()
	raw, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "address.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	store := &addressOwnerStore{SQLiteStore: raw}
	native := &addressOwnerNative{snapshot: staticAddressSnapshot{Index: 7, Scope: "address-native-world"}}
	c := &IPv4StaticAddressController{Router: &api.Router{}, Store: store, MobilitySource: store, ObserveAddresses: native.observe, Command: native.command, MobilityDataplane: dynamicconfig.MobilityDataplanePlan{PoolPrefix: "10.88.60.0/24", StaticAddresses: []dynamicconfig.MobilityIPv4AddressIntent{{ID: "owned", PoolRef: "cloudedge", PoolPrefix: "10.88.60.0/24", Purpose: dynamicconfig.MobilityIPv4AddressPurposeCaptureSource, Interface: "lan0", Address: "10.88.60.10/32"}}}}
	writeMobilityAddressPlan(t, c, store)
	return c, store, native
}
func writeMobilityAddressPlan(t *testing.T, c *IPv4StaticAddressController, s *addressOwnerStore) {
	t.Helper()
	now := time.Now().UTC()
	part := dynamicconfig.NewPart("address-native-owner", "MobilityPool/cloudedge/node/onprem", nil, 1, now.Add(-time.Second), now.Add(5*time.Minute))
	part.Spec.Digest = "native-fixture"
	part.Spec.MobilityDataplane = c.MobilityDataplane
	part.Spec.MobilityDataplane.PoolPrefix = "10.88.60.0/24"
	record, err := codec.Encode(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDynamicConfigPart(record); err != nil {
		t.Fatal(err)
	}
}
func addressPending(t *testing.T, s *addressOwnerStore) []mobilityAppliedIPv4Address {
	t.Helper()
	rows, err := pendingMobilityAddresses(nativeStatus(t, s.SQLiteStore))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestMobilityAddressExclusiveAcquireAndAcknowledgedWithdrawal(t *testing.T) {
	c, s, n := mobilityAddressOwnerFixture(t)
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := c.appliedMobilityStaticAddresses()
	if err != nil || len(rows) != 1 || rows[0].Scope != n.snapshot.Scope || rows[0].IfIndex != 7 || len(addressPending(t, s)) != 0 {
		t.Fatalf("native acquisition=%+v %v", rows, err)
	}
	c.MobilityDataplane = nativeWithdrawalFixture()
	writeMobilityAddressPlan(t, c, s)
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err = c.appliedMobilityStaticAddresses()
	if err != nil || len(rows) != 0 || len(n.snapshot.Addresses) != 0 || n.writes != 2 {
		t.Fatalf("withdrawal=%+v err=%v writes=%d", rows, err, n.writes)
	}
	assertNativeAck(t, nativeStatus(t, s.SQLiteStore), dynamicconfig.MobilityAddressWithdrawalAcks, c.MobilityDataplane.Withdrawals)
}

func TestMobilityAddressLostAcquisitionAckCannotAdoptEqualOldReceipt(t *testing.T) {
	c, s, n := mobilityAddressOwnerFixture(t)
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err != nil {
		t.Fatal(err)
	}
	n.snapshot.Addresses = nil
	failure := errors.New("acquisition acknowledgement failed")
	n.afterCommand = func() { s.failApplied = failure }
	if err := c.reconcileMobilityStaticAddresses(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("lost ACK=%v", err)
	}
	if len(addressPending(t, s)) != 1 {
		t.Fatal("pending add retired by old applied equality")
	}
	s.failApplied = nil
	n.afterCommand = nil
	writes := n.writes
	restarted := *c
	if err := restarted.reconcileMobilityStaticAddresses(t.Context()); err == nil || !strings.Contains(err.Error(), "without acknowledged ownership") {
		t.Fatalf("restart adopted uncertain presence: %v", err)
	}
	if n.writes != writes {
		t.Fatal("uncertain presence replayed")
	}
	rows, _ := c.appliedMobilityStaticAddresses()
	if len(rows) != 0 {
		t.Fatal("old absent acquisition was retained as ownership of a later writer")
	}
}

func TestMobilityAddressLostDeleteReplyFencesForeignReappearance(t *testing.T) {
	c, s, n := mobilityAddressOwnerFixture(t)
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.MobilityDataplane = nativeWithdrawalFixture()
	writeMobilityAddressPlan(t, c, s)
	n.deleteErr = errors.New("native delete reply lost")
	if err := c.reconcileMobilityStaticAddresses(t.Context()); !errors.Is(err, n.deleteErr) {
		t.Fatalf("lost delete=%v", err)
	}
	if len(addressPending(t, s)) != 1 {
		t.Fatal("lost delete lost pending receipt")
	}
	n.snapshot.Addresses = []netip.Prefix{netip.MustParsePrefix("10.88.60.10/32")}
	n.deleteErr = nil
	writes := n.writes
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err == nil {
		t.Fatal("reappeared foreign address was accepted")
	}
	if n.writes != writes {
		t.Fatal("pending delete replayed")
	}
	assertNativeAck(t, nativeStatus(t, s.SQLiteStore), dynamicconfig.MobilityAddressWithdrawalAcks, nil)
}

func TestMobilityAddressCurrentSourceAndIfindexRecheckedAfterPendingWrite(t *testing.T) {
	for _, change := range []string{"source", "index", "foreign"} {
		t.Run(change, func(t *testing.T) {
			c, s, n := mobilityAddressOwnerFixture(t)
			changed := false
			s.afterMerge = func(updates map[string]any) {
				pending, ok := updates["pendingMobilityStaticAddresses"].([]mobilityAppliedIPv4Address)
				if !ok || len(pending) == 0 || pending[0].Operation != "add" || changed {
					return
				}
				changed = true
				switch change {
				case "source":
					c.MobilityDataplane.StaticAddresses = nil
					writeMobilityAddressPlan(t, c, s)
				case "index":
					n.snapshot.Index++
				case "foreign":
					n.snapshot.Addresses = []netip.Prefix{netip.MustParsePrefix("10.88.60.10/32")}
				}
			}
			if err := c.reconcileMobilityStaticAddresses(t.Context()); err == nil || !changed {
				t.Fatalf("stale premise reported success: %v", err)
			}
			if n.writes != 0 {
				t.Fatal("native effect occurred after premise changed")
			}
			if len(addressPending(t, s)) != 0 {
				t.Fatal("unissued operation left a lost-reply fence")
			}
		})
	}
}

func TestMobilityAddressReactivatedIntentCancelsWithdrawal(t *testing.T) {
	c, s, n := mobilityAddressOwnerFixture(t)
	wanted := c.MobilityDataplane.StaticAddresses[0]
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.MobilityDataplane = nativeWithdrawalFixture()
	writeMobilityAddressPlan(t, c, s)
	changed := false
	s.afterMerge = func(updates map[string]any) {
		pending, ok := updates["pendingMobilityStaticAddresses"].([]mobilityAppliedIPv4Address)
		if ok && len(pending) > 0 && pending[0].Operation == "delete" && !changed {
			changed = true
			c.MobilityDataplane = dynamicconfig.MobilityDataplanePlan{PoolPrefix: "10.88.60.0/24", StaticAddresses: []dynamicconfig.MobilityIPv4AddressIntent{wanted}}
			writeMobilityAddressPlan(t, c, s)
		}
	}
	writes := n.writes
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err == nil || !changed {
		t.Fatalf("reactivated address removed: %v", err)
	}
	if n.writes != writes || len(n.snapshot.Addresses) != 1 || len(addressPending(t, s)) != 0 {
		t.Fatal("reactivated address changed or became uncertain")
	}
	assertNativeAck(t, nativeStatus(t, s.SQLiteStore), dynamicconfig.MobilityAddressWithdrawalAcks, nil)
}

func TestMobilityAddressUnchangedPresenceRechecksCurrentSource(t *testing.T) {
	c, s, n := mobilityAddressOwnerFixture(t)
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err != nil {
		t.Fatal(err)
	}
	changed := false
	n.afterObserve = func() {
		if !changed {
			changed = true
			c.MobilityDataplane.StaticAddresses = nil
			writeMobilityAddressPlan(t, c, s)
		}
	}
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err == nil || !changed {
		t.Fatalf("stale unchanged address reported success: %v", err)
	}
	rows, err := c.appliedMobilityStaticAddresses()
	if err != nil || len(rows) != 0 || len(n.snapshot.Addresses) != 0 {
		t.Fatalf("revoked effect survived: %+v %v", rows, err)
	}
}

func TestMobilityAddressPendingOtherWorldCannotBeRetiredByLocalAbsence(t *testing.T) {
	c, s, n := mobilityAddressOwnerFixture(t)
	n.addErr = errors.New("lost add reply")
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err == nil {
		t.Fatal("lost reply succeeded")
	}
	n.snapshot.Scope = "another-kernel-world"
	n.snapshot.Addresses = nil
	c.MobilityDataplane = nativeWithdrawalFixture()
	writeMobilityAddressPlan(t, c, s)
	writes := n.writes
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err == nil {
		t.Fatal("another world authorized retirement")
	}
	if n.writes != writes || len(addressPending(t, s)) != 1 {
		t.Fatal("old-world receipt changed")
	}
	assertNativeAck(t, nativeStatus(t, s.SQLiteStore), dynamicconfig.MobilityAddressWithdrawalAcks, nil)
}

func TestMobilityAddressSourceRevokedWhileAcknowledgementWaits(t *testing.T) {
	c, s, n := mobilityAddressOwnerFixture(t)
	changed := false
	s.afterMerge = func(updates map[string]any) {
		rows, ok := updates["appliedMobilityStaticAddresses"].([]mobilityAppliedIPv4Address)
		if ok && len(rows) > 0 && !changed {
			changed = true
			c.MobilityDataplane.StaticAddresses = nil
			writeMobilityAddressPlan(t, c, s)
		}
	}
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err == nil || !changed {
		t.Fatalf("source revocation reported success: %v", err)
	}
	rows, err := c.appliedMobilityStaticAddresses()
	if err != nil || len(rows) != 0 || len(n.snapshot.Addresses) != 0 || len(addressPending(t, s)) != 0 {
		t.Fatalf("known stale address retained: %+v %v", rows, err)
	}
}

func TestMobilityAddressVirtualParentIsRecheckedAfterPendingWrite(t *testing.T) {
	parent, native, raw, _ := segmentOwnerFixture(t)
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	store := &addressOwnerStore{SQLiteStore: raw}
	n := &addressOwnerNative{snapshot: staticAddressSnapshot{Index: native.link.Index, Scope: native.link.Scope}}
	c := IPv4StaticAddressController{Router: parent.Router, Store: store, Segments: &parent, ObserveAddresses: n.observe, Command: n.command, MobilityDataplane: dynamicconfig.MobilityDataplanePlan{StaticAddresses: []dynamicconfig.MobilityIPv4AddressIntent{{ID: "owned", PoolRef: "cloudedge", PoolPrefix: "10.88.60.0/24", Purpose: dynamicconfig.MobilityIPv4AddressPurposeCaptureSource, Interface: "vxlan100", Address: "10.88.60.10/32"}}}}
	changed := false
	store.afterMerge = func(updates map[string]any) {
		pending, ok := updates["pendingMobilityStaticAddresses"].([]mobilityAppliedIPv4Address)
		if ok && len(pending) > 0 && !changed {
			changed = true
			native.link.Token = "foreign-replacement"
		}
	}
	if err := c.reconcileMobilityStaticAddresses(t.Context()); err == nil || !changed {
		t.Fatalf("replaced parent authorized address: %v", err)
	}
	if n.writes != 0 || len(addressPending(t, store)) != 0 {
		t.Fatal("address effect or uncertainty survived a known pre-effect parent replacement")
	}
}
