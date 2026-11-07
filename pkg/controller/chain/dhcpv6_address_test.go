// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type dhcpv6NativeFixture struct {
	c           DHCPv6AddressController
	store       *routerstate.SQLiteStore
	path        string
	now         time.Time
	snapshot    pdclient.Snapshot
	observation delegatedAddressObservation
	calls       []string
	unavailable bool
	declines    []daemonapi.DHCPv6DeclineRequest
}

func newDHCPv6NativeFixture(t *testing.T) *dhcpv6NativeFixture {
	t.Helper()
	f := &dhcpv6NativeFixture{path: filepath.Join(t.TempDir(), "state.db"), now: time.Now().UTC().Truncate(time.Second), observation: delegatedAddressObservation{ifIndex: 7, scope: "fixture-scope", lifetimeKnown: true}}
	var err error
	f.store, err = routerstate.OpenSQLite(f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.store.Close() })
	router := dhcpv6WorkerRouter()
	router.Spec.Resources = router.Spec.Resources[:3]
	f.snapshot = pdclient.Snapshot{Resource: "DHCPv6Address/address", Interface: "wan0", Mode: pdclient.RequestAddresses, ClientDUID: "01", IAID: 7, ServerDUID: "02", State: pdclient.StateBound, UpdatedAt: f.now, AddressUpdatedAt: f.now, AddressT1Seconds: 30, AddressT2Seconds: 45, Addresses: []pdclient.AddressSnapshot{{Address: "2001:db8::123", Preferred: 30, Valid: 60, AcquiredAt: f.now}}}
	f.c = DHCPv6AddressController{Router: router, DeclaredRouter: router, Store: f.store, OperatingSystem: platform.OSLinux, Now: func() time.Time { return f.now },
		Source: func(context.Context, string) (dhcpv6AddressSource, error) {
			return dhcpv6AddressSource{Config: routerstate.DHCPv6AddressSource{Worker: "DHCPv6Address/address", IfName: "wan0", ClientDUID: "01", IAID: 7, Mode: pdclient.RequestAddresses}, Available: true, SupervisorOwner: "owner"}, nil
		},
		ReadLease: func(context.Context, dhcpv6AddressSource) (pdclient.Snapshot, error) {
			if f.unavailable {
				return pdclient.Snapshot{}, errors.New("helper unavailable")
			}
			return f.snapshot, nil
		},
		Decline: func(_ context.Context, _ dhcpv6AddressSource, request daemonapi.DHCPv6DeclineRequest) error {
			f.declines = append(f.declines, request)
			f.snapshot.Addresses = nil
			f.snapshot.State = pdclient.StateExpired
			f.snapshot.UpdatedAt = f.now
			return nil
		},
		ObserveAddress: func(context.Context, string, string, string) (delegatedAddressObservation, error) {
			return f.observation, nil
		},
		Command: func(_ context.Context, name string, args ...string) error {
			f.calls = append(f.calls, name+" "+strings.Join(args, " "))
			if len(args) < 4 {
				t.Fatalf("unexpected command %s %v", name, args)
			}
			if args[2] == "del" {
				f.observation.present, f.observation.usable = false, false
				f.observation.sameHost = nil
				return nil
			}
			f.observation.present = true
			f.observation.sameHost = []string{args[3]}
			for i := range args {
				if i+1 < len(args) {
					n, _ := strconv.ParseUint(args[i+1], 10, 32)
					if args[i] == "preferred_lft" {
						f.observation.preferredSeconds = uint32(n)
					}
					if args[i] == "valid_lft" {
						f.observation.validSeconds = uint32(n)
					}
				}
			}
			return nil
		},
	}
	return f
}

func TestDHCPv6NativeAddressDADAndSQLiteRecoveryDoNotExtendLease(t *testing.T) {
	f := newDHCPv6NativeFixture(t)
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != "ip -6 addr add 2001:db8::123/128 dev wan0 preferred_lft 30 valid_lft 60" {
		t.Fatalf("native acquisition=%v", f.calls)
	}
	record, err := f.store.ReadDHCPv6AddressRecord("address")
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Phase == "Applied" {
		t.Fatal("tentative address was reported Applied")
	}
	f.observation.usable = true
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := f.store.ObjectStatus(api.NetAPIVersion, "DHCPv6Address", "address"); got["phase"] != "Applied" || got["address"] != "2001:db8::123/128" {
		t.Fatalf("DAD passed without applied projection: %v", got)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store, err = routerstate.OpenSQLite(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.c.Store = f.store
	f.unavailable = true
	f.now = f.now.Add(20 * time.Second)
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatal("SQLite recovery or unavailable helper refreshed a finite address")
	}
	f.now = f.now.Add(41 * time.Second)
	f.observation.present, f.observation.usable = false, false
	f.observation.sameHost = nil
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	record, err = f.store.ReadDHCPv6AddressRecord("address")
	if err != nil {
		t.Fatal(err)
	}
	if len(record.State.Leases) != 0 || record.State.Native != nil && len(record.State.Native.Addresses) != 0 {
		t.Fatalf("expiry retained authority or ownership: %+v", record.State)
	}
	if len(f.calls) != 1 {
		t.Fatal("expired address was recreated or foreign cleanup was attempted")
	}
}

func TestDHCPv6NativeAddressRemovalAndForeignTuple(t *testing.T) {
	f := newDHCPv6NativeFixture(t)
	f.observation.present, f.observation.usable = true, true
	f.observation.sameHost = []string{"2001:db8::123/128"}
	_ = f.c.Reconcile(t.Context())
	if len(f.calls) != 0 {
		t.Fatal("preexisting address was adopted or changed")
	}
	f.observation.present, f.observation.usable = false, false
	f.observation.sameHost = nil
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.c.Router, f.c.DeclaredRouter = &api.Router{}, &api.Router{}
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || !strings.Contains(f.calls[1], "addr del 2001:db8::123/128") {
		t.Fatalf("resource removal did not withdraw exact owned address: %v", f.calls)
	}
	record, err := f.store.ReadDHCPv6AddressRecord("address")
	if err != nil || record.Revision != 0 {
		t.Fatalf("removed owner retained: %+v %v", record, err)
	}
}

func TestDHCPv6NativeDADFailureWithdrawsAndReportsExactAcquisition(t *testing.T) {
	f := newDHCPv6NativeFixture(t)
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.observation.dadFailed = true
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.declines) != 1 || len(f.declines[0].Addresses) != 1 || f.declines[0].Addresses[0].Address != "2001:db8::123" || !f.declines[0].Addresses[0].AcquiredAt.Equal(f.snapshot.AddressUpdatedAt) {
		t.Fatalf("DAD report not bound to acquisition: %+v", f.declines)
	}
	if f.observation.present || len(f.calls) != 2 {
		t.Fatalf("DAD-failed owned tuple remained: %v", f.calls)
	}
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatal("declined address was reapplied")
	}
}

func TestDHCPv6NativeStableObservationDoesNotRewriteState(t *testing.T) {
	f := newDHCPv6NativeFixture(t)
	f.observation.usable = true
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := f.store.ReadDHCPv6AddressRecord("address")
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	f.snapshot.UpdatedAt = f.now
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.ReadDHCPv6AddressRecord("address")
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision {
		t.Fatalf("unchanged observation rewrote native state: %d -> %d", before.Revision, after.Revision)
	}
}

func TestDHCPv6NativeLostApplyAcknowledgmentDoesNotAdoptTuple(t *testing.T) {
	f := newDHCPv6NativeFixture(t)
	apply := f.c.Command
	f.c.Command = func(ctx context.Context, name string, args ...string) error {
		if err := apply(ctx, name, args...); err != nil {
			return err
		}
		return errors.New("lost apply acknowledgment")
	}
	if err := f.c.Reconcile(t.Context()); err == nil {
		t.Fatal("lost acknowledgment reported success")
	}
	f.observation.usable = true
	f.c.Command = apply
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	record, err := f.store.ReadDHCPv6AddressRecord("address")
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Phase == "Applied" || len(f.calls) != 1 {
		t.Fatalf("uncertain apply adopted/replaced tuple: %+v calls=%v", record.State, f.calls)
	}
}

func TestDHCPv6NativeInfiniteLifetimeIsNotConvertedToFiniteExpiry(t *testing.T) {
	f := newDHCPv6NativeFixture(t)
	f.snapshot.Addresses[0].Preferred = 0xffffffff
	f.snapshot.Addresses[0].Valid = 0xffffffff
	f.observation.usable = true
	command := f.c.Command
	f.c.Command = func(ctx context.Context, name string, args ...string) error {
		err := command(ctx, name, args...)
		f.observation.preferredSeconds, f.observation.validSeconds = 0xffffffff, 0xffffffff
		return err
	}
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.unavailable = true
	f.now = f.now.Add(140 * 365 * 24 * time.Hour)
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	record, err := f.store.ReadDHCPv6AddressRecord("address")
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Phase != "Applied" || len(f.calls) != 1 {
		t.Fatalf("infinite lease was expired or replaced: %+v calls=%v", record.State, f.calls)
	}
}

func TestDHCPv6NativeUncertainApplyExpiresWithoutTouchingReplacement(t *testing.T) {
	f := newDHCPv6NativeFixture(t)
	apply := f.c.Command
	f.c.Command = func(ctx context.Context, name string, args ...string) error {
		if err := apply(ctx, name, args...); err != nil {
			return err
		}
		return errors.New("lost apply acknowledgment")
	}
	if err := f.c.Reconcile(t.Context()); err == nil {
		t.Fatal("lost apply acknowledgment reported success")
	}
	f.c.Command = apply
	f.now = f.now.Add(61 * time.Second)
	// The tuple is present after the original finite lifetime. It could be a
	// replacement owned by another actor; expiry must not authorize deletion.
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	record, err := f.store.ReadDHCPv6AddressRecord("address")
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Native != nil || len(f.calls) != 1 {
		t.Fatalf("expired pending ownership retained or changed replacement: %+v calls=%v", record.State, f.calls)
	}
}

func TestDHCPv6NativeWithdrawalRechecksExpiryAfterObservation(t *testing.T) {
	f := newDHCPv6NativeFixture(t)
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.c.Router, f.c.DeclaredRouter = &api.Router{}, &api.Router{}
	reads := 0
	f.c.ObserveAddress = func(context.Context, string, string, string) (delegatedAddressObservation, error) {
		reads++
		if reads == 2 {
			f.now = f.now.Add(time.Minute)
		}
		return f.observation, nil
	}
	_ = f.c.Reconcile(t.Context())
	if len(f.calls) != 1 {
		t.Fatalf("withdrawal deleted tuple after ownership deadline: %v", f.calls)
	}
	if err := f.c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	record, err := f.store.ReadDHCPv6AddressRecord("address")
	if err != nil || record.Revision != 0 {
		t.Fatalf("expired cleanup journal not retired: %+v %v", record, err)
	}
}
