// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	state "github.com/imksoo/routerd/pkg/state"
)

func sqliteNativeLANFixture(t *testing.T) (*state.SQLiteStore, *api.Router, LANAddressController, string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	router, _, _ := finiteLANFixture(now)
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := state.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	lease, _, err := store.ConfigurePDLease("wan-pd", state.PDLeaseConfig{Client: api.IPv6PDClientRouterd, UplinkIfname: "wan0", ClientDUID: "01", IAID: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease.CurrentPrefix = "2001:db8:1200::/60"
	lease.LastReplyAt = now
	lease.SnapshotUpdatedAt = now
	lease.LeaseUpdatedAt = now
	lease.PreferredSeconds = 90
	lease.ValidSeconds = 120
	lease.Phase = daemonapi.ResourcePhaseBound
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "lan", map[string]any{"phase": "Up"}); err != nil {
		t.Fatal(err)
	}
	c := LANAddressController{Router: router, DeclaredRouter: router, Store: store, Now: func() time.Time { return now }}
	return store, router, c, path
}

func TestLANNativeReceiptAcquisitionAndRemovedRecovery(t *testing.T) {
	store, _, c, path := sqliteNativeLANFixture(t)
	present := false
	failDelete := false
	writes := 0
	c.Command = func(_ context.Context, _ string, args ...string) error {
		status, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
		if err != nil {
			return err
		}
		receipt, err := delegatedReceipt(status)
		if err != nil || receipt == nil || len(receipt.Addresses) != 1 || receipt.Addresses[0].Pending == nil {
			t.Fatalf("native effect before durable pending ownership: %+v %v", status, err)
		}
		writes++
		line := strings.Join(args, " ")
		if strings.Contains(line, "del") || strings.Contains(line, "-alias") {
			if failDelete {
				return errors.New("native withdrawal failed")
			}
			present = false
		} else {
			present = true
		}
		return nil
	}
	addresses := installLANIOFixture(&c)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, _ := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	receipt, err := delegatedReceipt(status)
	if err != nil || receipt == nil || receipt.Addresses[0].Pending != nil || status["phase"] != "Applied" || !present || writes != 1 {
		t.Fatalf("unconfirmed acquisition: %+v %v", status, err)
	}
	// The resource and its PD declaration both disappear; only the durable
	// native receipt identifies cleanup after a process restart.
	c.Router = &api.Router{}
	c.DeclaredRouter = c.Router
	failDelete = true
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("failed deleted-resource cleanup acknowledged")
	}
	status, _ = store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	receipt, err = delegatedReceipt(status)
	if err != nil || !receipt.has("2001:db8:1200:1::1/64") {
		t.Fatalf("failed deletion lost owner: %+v %v", status, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := state.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c.Store = reopened
	failDelete = false
	if err := c.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "ownership unknown") {
		t.Fatalf("pending present recovery was not fenced: %v", err)
	}
	present = false // operator confirms removal of the ambiguous native tuple.
	delete(addresses, "2001:db8:1200:1::1/64")
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err = reopened.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	if err != nil || status != nil || present || writes != 2 {
		t.Fatalf("restart cleanup incomplete: status=%+v present=%t writes=%d err=%v", status, present, writes, err)
	}
}

func TestLANWhenFalseDisplayDoesNotAuthorizeForeignDeletion(t *testing.T) {
	store, router, c, _ := sqliteNativeLANFixture(t)
	spec, _ := router.Spec.Resources[2].IPv6DelegatedAddressSpec()
	spec.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gw.role": {Equals: "master"}}}
	router.Spec.Resources[2].Spec = spec
	if _, err := store.Set("VirtualAddress/gw.role", "backup", "test"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address", map[string]any{"phase": "Applied", "address": "2001:db8:1200:1::1/64", "prefixSource": "wan-pd"}); err != nil {
		t.Fatal(err)
	}
	c.Router = mustWhenFilteredRouter(t, true, router, store)
	c.ObserveAddress = lanProbeFixture(func(context.Context, string, string) bool { return true }, nil, nil)
	c.Command = func(context.Context, string, ...string) error {
		t.Fatal("display/declared address authorized a native deletion")
		return nil
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestLANRemovedReceiptDoesNotDeleteInterfaceReplacement(t *testing.T) {
	store, _, c, _ := sqliteNativeLANFixture(t)
	recordLANReceiptForTest(t, store, "lan-address", "lan0", "wan-pd", "2001:db8:1200:1::1/64")
	c.Router = &api.Router{}
	c.DeclaredRouter = c.Router
	c.ObserveAddress = func(context.Context, string, string, bool) (delegatedAddressObservation, error) {
		return delegatedAddressObservation{ifIndex: 8, scope: "replacement-kernel", present: true, usable: true}, nil
	}
	c.Command = func(context.Context, string, ...string) error {
		t.Fatal("old receipt deleted a foreign replacement")
		return nil
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPDCanonicalOlderValidAssociationDrivesLANReadinessAndAtomicStatus(t *testing.T) {
	store, router, c, _ := sqliteNativeLANFixture(t)
	lease, _, _ := store.ReadPDLease("wan-pd")
	epoch := time.Now().UTC().Add(-10 * time.Second)
	deadline := epoch.Add(100 * time.Second)
	lease.LastReplyAt = epoch
	snapshotEpoch := lease.SnapshotUpdatedAt
	lease.LeaseUpdatedAt = epoch
	lease.LastObservedAt = epoch
	lease.ValidSeconds = 5
	lease.PreferredSeconds = 4
	lease.PreviousPrefixes = []state.PDPreviousPrefix{{Prefix: "2001:db8:1100::/60", ExpiresAt: deadline}}
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	now := epoch.Add(11 * time.Second)
	c.Now = func() time.Time { return now }
	recordLANReceiptForTest(t, store, "lan-address", "lan0", "wan-pd", "2001:db8:1100:1::1/64")
	var commands []string
	c.Command = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil
	}
	installLANIOFixture(&c, "2001:db8:1100:1::1/64")
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := "ip -6 addr replace 2001:db8:1100:1::1/64 dev lan0 preferred_lft 0 valid_lft 89"
	if c.currentOS() == platform.OSFreeBSD {
		want = "ifconfig lan0 inet6 2001:db8:1100:1::1 prefixlen 64 pltime 0 vltime 89 alias"
	}
	if len(commands) != 1 || commands[0] != want {
		t.Fatalf("valid older canonical prefix was lost/extended: %+v", commands)
	}
	ready, err := resourcequery.ReadSourceReady(store, "DHCPv6PrefixDelegation/wan-pd")
	if err != nil || !ready {
		t.Fatalf("canonical previous authority not ready: %t %v", ready, err)
	}
	current, _, _ := store.ReadPDLease("wan-pd")
	eventBus := bus.NewWithStore(store)
	if err := RecordPDLeaseObservation(store, eventBus, router, "wan-pd", current.Config(), current, nil, daemonapi.ResourceStatus{}); err != nil {
		t.Fatal(err)
	}
	current, _, _ = store.ReadPDLease("wan-pd")
	if current.Phase != daemonapi.ResourcePhaseBound || current.CurrentPrefix != "2001:db8:1100::/60" || !current.LastReplyAt.Equal(epoch) || !current.SnapshotUpdatedAt.Equal(snapshotEpoch) || !current.LeaseUpdatedAt.Equal(epoch) || current.LastReplyAt.Add(time.Duration(current.ValidSeconds)*time.Second).After(deadline) {
		t.Fatalf("atomic projection fabricated lease evidence: %+v", current)
	}
	for _, event := range eventBus.Recent("routerd.resource.status.changed") {
		if event.Attributes["phase"] == daemonapi.ResourcePhaseExpired {
			t.Fatalf("live older association emitted expiry: %+v", event)
		}
	}
}

func TestLANColdSnapshotCommittedBySourceOwnerSurvivesMissingWorkerFile(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store, declared, path := sqliteLANAddressStagingFixture(t, pdSnapshotFixture(now))
	statusOwner := DaemonStatusController{Router: declared, Store: store, DaemonSockets: map[string]string{"wan-pd": filepath.Join(t.TempDir(), "missing.sock")}, PDLeaseSnapshotPath: func(string) string { return path }}
	if err := statusOwner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	canonical, found, err := store.ReadPDLease("wan-pd")
	if err != nil || !found || canonical.CurrentPrefix != "2001:db8:1200::/60" {
		t.Fatalf("worker file did not enter canonical authority: %+v %v", canonical, err)
	}
	c := LANAddressController{Router: mustWhenFilteredRouter(t, true, declared, store), DeclaredRouter: declared, Store: store, Now: func() time.Time { return now }, VMACPresent: func(string) bool { return true }, EnsureVMAC: func(context.Context, string) error { return nil }}
	var commands []string
	c.Command = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil
	}
	installLANIOFixture(&c)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	commands = nil
	if err := statusOwner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 {
		t.Fatalf("missing helper/file revoked valid canonical lease: %+v", commands)
	}
	after, _, _ := store.ReadPDLease("wan-pd")
	if !after.LastReplyAt.Equal(canonical.LastReplyAt) || !after.SnapshotUpdatedAt.Equal(canonical.SnapshotUpdatedAt) {
		t.Fatalf("missing worker fabricated a fresh observation: %+v", after)
	}
}

type lanSourceReadFault struct {
	*state.SQLiteStore
	failed  bool
	failure error
}

func (s *lanSourceReadFault) ReadPDLease(name string) (state.PDLease, bool, error) {
	if s.failed {
		return state.PDLease{}, false, s.failure
	}
	return s.SQLiteStore.ReadPDLease(name)
}
func TestLANUnknownSourceAfterNativeWriteRetainsReceiptWithoutWithdrawal(t *testing.T) {
	store, _, c, _ := sqliteNativeLANFixture(t)
	fault := &lanSourceReadFault{SQLiteStore: store, failure: errors.New("source read unavailable")}
	c.Store = fault
	present := false
	writes := 0
	c.Command = func(_ context.Context, _ string, args ...string) error {
		writes++
		line := strings.Join(args, " ")
		if strings.Contains(line, "del") || strings.Contains(line, "-alias") {
			t.Fatal("unknown observation became native withdrawal")
		}
		present = true
		fault.failed = true
		return nil
	}
	installLANIOFixture(&c)
	if err := c.Reconcile(t.Context()); !errors.Is(err, fault.failure) {
		t.Fatalf("unknown source read lost: %v", err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := delegatedReceipt(status)
	if err != nil || !receipt.has("2001:db8:1200:1::1/64") || !present || writes != 1 {
		t.Fatalf("unknown observation lost recoverable native receipt: %+v %v", status, err)
	}
}

func TestPDFileSourceOwnerRejectsStaleReplayAfterRelease(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := pdSnapshotFixture(now.Add(-10 * time.Second))
	store, declared, path := sqliteLANAddressStagingFixture(t, snapshot)
	source := DaemonStatusController{Router: declared, Store: store, DaemonSockets: map[string]string{"wan-pd": filepath.Join(t.TempDir(), "missing.sock")}, PDLeaseSnapshotPath: func(string) string { return path }}
	released := snapshot
	released.State = pdclient.StateReleased
	released.CurrentPrefix = ""
	released.Prefixes = nil
	released.UpdatedAt = now.Add(-5 * time.Second)
	if err := store.SavePDLeaseSnapshot("wan-pd", released); err != nil {
		t.Fatal(err)
	}
	if err := source.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, _, _ := store.ReadPDLease("wan-pd")
	if current.CurrentPrefix != "" || current.SnapshotUpdatedAt != released.UpdatedAt {
		t.Fatalf("stale persisted file revived released lease: %+v", current)
	}
	snapshot.UpdatedAt = now.Add(-time.Second)
	snapshot.LeaseUpdatedAt = snapshot.UpdatedAt
	snapshot.Prefixes[0].AcquiredAt = snapshot.UpdatedAt
	writePDSnapshot(t, path, snapshot)
	if err := source.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, _, _ = store.ReadPDLease("wan-pd")
	if current.CurrentPrefix != snapshot.CurrentPrefix || !current.LastReplyAt.Equal(snapshot.UpdatedAt) {
		t.Fatalf("genuine newer source transaction rejected: %+v", current)
	}
}

type lanHeldSaveFault struct {
	*state.SQLiteStore
	failure error
	fail    bool
}

func (s *lanHeldSaveFault) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if s.fail && kind == "IPv6DelegatedAddress" {
		receipt, err := delegatedReceipt(status)
		if err != nil {
			return err
		}
		if receipt != nil {
			for _, entry := range receipt.Addresses {
				if entry.Held && entry.Pending == nil {
					return s.failure
				}
			}
		}
	}
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}
func TestLANNewPendingPresentAfterLostConfirmationIsFenced(t *testing.T) {
	store, _, c, path := sqliteNativeLANFixture(t)
	failure := errors.New("held receipt persistence failed")
	fault := &lanHeldSaveFault{SQLiteStore: store, failure: failure, fail: true}
	c.Store = fault
	present := false
	writes := 0
	c.Command = func(_ context.Context, name string, args ...string) error {
		writes++
		wantName := "ip"
		want := "-6 addr add 2001:db8:1200:1::1/64 dev lan0 preferred_lft 90 valid_lft 120"
		if c.currentOS() == platform.OSFreeBSD {
			wantName = "ifconfig"
			want = "lan0 inet6 2001:db8:1200:1::1 prefixlen 64 pltime 90 vltime 120 alias"
		}
		if name != wantName || strings.Join(args, " ") != want {
			t.Fatalf("new tuple acquired through overwrite: %+v", args)
		}
		present = true
		return nil
	}
	addresses := installLANIOFixture(&c)
	if err := c.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("lost held confirmation hidden: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := state.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c.Store = reopened
	if err := c.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "ownership unknown") {
		t.Fatalf("ambiguous pending+present was adopted: %v", err)
	}
	if writes != 1 {
		t.Fatal("recovery mutated ambiguous native tuple")
	}
	present = false // explicit native absence fences out the possible foreign tuple.
	delete(addresses, "2001:db8:1200:1::1/64")
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if writes != 2 || !present {
		t.Fatal("confirmed absence did not permit a fresh acquisition")
	}
}
