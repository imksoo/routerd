// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	gobgpapi "github.com/osrg/gobgp/v4/api"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/manageddaemon"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type observedFIBBackend struct {
	scope           string
	routes          []fibObservedRoute
	locals          []fibLocalAddress
	effects         []string
	failRead        error
	failAdd         error
	failDelete      error
	beforeEffect    func(string, bgpstate.NativeFIBRoute)
	independentHops bool
	interfaceIndex  int
}

func (b *observedFIBBackend) index() int {
	if b.interfaceIndex > 0 {
		return b.interfaceIndex
	}
	return 7
}
func (b *observedFIBBackend) KernelScope(ctx context.Context) (string, error) {
	return b.scope, ctx.Err()
}

func (b *observedFIBBackend) Snapshot(ctx context.Context) (fibSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return fibSnapshot{}, err
	}
	if b.failRead != nil {
		return fibSnapshot{}, b.failRead
	}
	return fibSnapshot{KernelScope: b.scope, Routes: append([]fibObservedRoute(nil), b.routes...), Addresses: b.locals}, nil
}
func (b *observedFIBBackend) Prepare(_ context.Context, r FIBRoute, _ fibSnapshot) ([]bgpstate.NativeFIBRoute, string, error) {
	native := bgpstate.NativeFIBRoute{Prefix: r.Prefix, PreferredSource: r.PreferredSource, Table: 254, Protocol: 186, Metric: 200}
	for _, hop := range r.NextHops {
		native.NextHops = append(native.NextHops, bgpstate.FIBNextHop{Gateway: hop, InterfaceIndex: b.index()})
	}
	if !b.independentHops {
		return []bgpstate.NativeFIBRoute{native}, "", nil
	}
	out := make([]bgpstate.NativeFIBRoute, 0, len(native.NextHops))
	for _, hop := range native.NextHops {
		row := native
		row.Table = 0
		row.Protocol = 0x8000
		row.Metric = 0
		row.PreferredSource = "192.0.2.10"
		row.NextHops = []bgpstate.FIBNextHop{hop}
		out = append(out, row)
	}
	return out, "", nil
}
func (b *observedFIBBackend) Add(ctx context.Context, r bgpstate.NativeFIBRoute) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.beforeEffect != nil {
		b.beforeEffect("add", r)
	}
	b.effects = append(b.effects, "add "+r.Prefix)
	if b.failAdd != nil {
		return b.failAdd
	}
	b.routes = append(b.routes, fibObservedRoute{Route: r, Exact: true})
	return nil
}
func (b *observedFIBBackend) Delete(ctx context.Context, r bgpstate.NativeFIBRoute) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.beforeEffect != nil {
		b.beforeEffect("withdraw", r)
	}
	b.effects = append(b.effects, "withdraw "+r.Prefix)
	if b.failDelete != nil {
		return b.failDelete
	}
	kept := make([]fibObservedRoute, 0, len(b.routes))
	for _, row := range b.routes {
		if !row.Exact || !bgpstate.EqualNativeFIBRoute(row.Route, r) {
			kept = append(kept, row)
		}
	}
	b.routes = kept
	return nil
}
func newFIBOwnerFixture(t *testing.T) (*routerstate.SQLiteStore, *observedFIBBackend, *nativeFIBSyncer) {
	t.Helper()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	backend := &observedFIBBackend{scope: "linux:fixture:network"}
	backend.beforeEffect = func(action string, route bgpstate.NativeFIBRoute) {
		t.Helper()
		journal, found, err := store.ReadBGPFIB(t.Context(), backend.scope)
		if err != nil || !found || journal.Pending == nil || journal.Pending.Action != action || !bgpstate.EqualNativeFIBRoute(journal.Pending.Route, route) {
			t.Fatalf("native %s was not preceded by its exact pending intent: %+v, found=%v err=%v", action, journal, found, err)
		}
	}
	return store, backend, &nativeFIBSyncer{backend: backend}
}
func syncFIBFixture(ctx context.Context, s FIBSyncer, store FIBStore, routes []FIBRoute) (FIBSyncResult, error) {
	return s.SyncBGP(ctx, store, routes, func(context.Context) ([]FIBRoute, error) { return routes, nil })
}
func fibDesired(hop string) []FIBRoute {
	return []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{hop}}}
}

func TestFIBOwnerPreservesForeignProtocolRoutesAcrossRestartReplacementAndRemoval(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	foreign := bgpstate.NativeFIBRoute{Prefix: "10.77.60.99/32", NextHops: []bgpstate.FIBNextHop{{Gateway: "192.0.2.99", InterfaceIndex: 7}}, Table: 254, Protocol: 186, Metric: 200}
	b.routes = []fibObservedRoute{{Route: foreign, Exact: true}}
	result, err := syncFIBFixture(t.Context(), s, store, fibDesired("192.0.2.1"))
	if err != nil || !result.Installed["10.77.60.11/32"] {
		t.Fatalf("initial=%+v %v", result, err)
	}
	restarted := &nativeFIBSyncer{backend: b}
	if _, err := syncFIBFixture(t.Context(), restarted, store, fibDesired("192.0.2.2")); err != nil {
		t.Fatal(err)
	}
	if err := restarted.ClearBGP(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if len(b.routes) != 1 || !bgpstate.EqualNativeFIBRoute(b.routes[0].Route, foreign) {
		t.Fatalf("foreign BGP protocol route was changed: %+v", b.routes)
	}
	want := []string{"add 10.77.60.11/32", "withdraw 10.77.60.11/32", "add 10.77.60.11/32", "withdraw 10.77.60.11/32"}
	if !reflect.DeepEqual(b.effects, want) {
		t.Fatalf("native operations=%v, want %v", b.effects, want)
	}
	journal, found, err := store.ReadBGPFIB(t.Context(), b.scope)
	if err != nil || !found || len(journal.Applied) != 0 || journal.Pending != nil {
		t.Fatalf("completed journal=%+v %v %v", journal, found, err)
	}
}

func TestFIBOwnerDoesNotAdoptOrReplaceUnjournaledMatchingRoute(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	routes, _, _ := b.Prepare(t.Context(), fibDesired("192.0.2.1")[0], fibSnapshot{})
	b.routes = []fibObservedRoute{{Route: routes[0], Exact: true}}
	if _, err := syncFIBFixture(t.Context(), s, store, fibDesired("192.0.2.1")); err == nil || !strings.Contains(err.Error(), "unowned") {
		t.Fatalf("matching foreign route admission=%v", err)
	}
	if err := s.ClearBGP(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if len(b.effects) != 0 || len(b.routes) != 1 {
		t.Fatalf("matching foreign route mutated: %v %+v", b.effects, b.routes)
	}
}

type failFIBCheckpointStore struct {
	FIBStore
	calls  int
	failAt int
}

func (s *failFIBCheckpointStore) SaveBGPFIB(ctx context.Context, j bgpstate.FIBJournal) error {
	s.calls++
	if s.calls == s.failAt {
		return errors.New("checkpoint disk failure")
	}
	return s.FIBStore.SaveBGPFIB(ctx, j)
}

func TestFIBOwnerUnconfirmedAddPresentIsUnknownAndFencedAfterRestart(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	failing := &failFIBCheckpointStore{FIBStore: store, failAt: 2}
	if _, err := syncFIBFixture(t.Context(), s, failing, fibDesired("192.0.2.1")); err == nil {
		t.Fatal("missing final checkpoint failure")
	}
	journal, _, err := store.ReadBGPFIB(t.Context(), b.scope)
	if err != nil || journal.Pending == nil || len(journal.Applied) != 0 {
		t.Fatalf("unconfirmed write lost pending: %+v %v", journal, err)
	}
	restarted := &nativeFIBSyncer{backend: b}
	if err := restarted.ClearBGP(t.Context(), store); err == nil || !strings.Contains(err.Error(), "unknown prior outcome") {
		t.Fatalf("unconfirmed write was treated as owned: %v", err)
	}
	journal, _, err = store.ReadBGPFIB(t.Context(), b.scope)
	if err != nil || journal.Pending == nil || !journal.Pending.Unknown || len(b.effects) != 1 {
		t.Fatalf("uncertainty not retained: %+v effects=%v err=%v", journal, b.effects, err)
	}
	// An operator removes the inspected ambiguous native route. Complete native
	// absence, rather than elapsed time or the protocol number, permits recovery.
	b.routes = nil
	if _, err := syncFIBFixture(t.Context(), restarted, store, fibDesired("192.0.2.1")); err != nil {
		t.Fatalf("absent pending recovery: %v", err)
	}
	if len(b.effects) != 2 {
		t.Fatalf("absence recovery operations=%v", b.effects)
	}
}

func TestFIBOwnerFailedAddAbsentRetriesAndFailedWithdrawKeepsReceipt(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	b.failAdd = errors.New("native add denied")
	if _, err := syncFIBFixture(t.Context(), s, store, fibDesired("192.0.2.1")); err == nil {
		t.Fatal("missing add failure")
	}
	b.failAdd = nil
	if _, err := syncFIBFixture(t.Context(), (&nativeFIBSyncer{backend: b}), store, fibDesired("192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	b.failDelete = errors.New("native withdrawal denied")
	if err := s.ClearBGP(t.Context(), store); err == nil {
		t.Fatal("missing withdrawal failure")
	}
	journal, _, err := store.ReadBGPFIB(t.Context(), b.scope)
	if err != nil || journal.Pending == nil || journal.Pending.Action != "withdraw" || len(journal.Applied) != 1 {
		t.Fatalf("failed withdrawal forgot ownership: %+v %v", journal, err)
	}
	b.failDelete = nil
	if err := (&nativeFIBSyncer{backend: b}).ClearBGP(t.Context(), store); err == nil || !strings.Contains(err.Error(), "unknown prior outcome") {
		t.Fatalf("unacknowledged withdrawal replayed: %v", err)
	}
	b.routes = nil
	if err := (&nativeFIBSyncer{backend: b}).ClearBGP(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if len(b.routes) != 0 {
		t.Fatalf("withdraw retry left owned route: %+v", b.routes)
	}
}

func TestFIBOwnerChangedForeignTupleAndFailedObservationDoNotAuthorizeDeletion(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	if _, err := syncFIBFixture(t.Context(), s, store, fibDesired("192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	b.failRead = errors.New("native dump incomplete")
	if err := s.ClearBGP(t.Context(), store); !errors.Is(err, b.failRead) {
		t.Fatalf("incomplete dump=%v", err)
	}
	if len(b.effects) != 1 {
		t.Fatalf("failed observation caused effect: %v", b.effects)
	}
	b.failRead = nil
	b.routes[0].Route.NextHops = []bgpstate.FIBNextHop{{Gateway: "192.0.2.99", InterfaceIndex: 8}}
	if err := s.ClearBGP(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if len(b.effects) != 1 || len(b.routes) != 1 {
		t.Fatalf("changed native tuple was deleted: %v %+v", b.effects, b.routes)
	}
}

func TestFIBOwnerPreferredSourceUsesOneCompleteAddressSnapshot(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	b.locals = []fibLocalAddress{{Address: netip.MustParseAddr("10.77.60.4"), Prefix: netip.MustParsePrefix("10.77.60.0/24")}}
	result, err := syncFIBFixture(t.Context(), s, store, fibDesired("192.0.2.1"))
	if err != nil || result.PreferredSource["10.77.60.11/32"] != "10.77.60.4" {
		t.Fatalf("captured preferred source=%+v %v", result, err)
	}
	oldScope := b.scope
	original, _, err := store.ReadBGPFIB(t.Context(), oldScope)
	if err != nil {
		t.Fatal(err)
	}
	b.scope = "linux:another-boot:network"
	if err := s.ClearBGP(t.Context(), store); err != nil {
		t.Fatalf("current-world removal inspected another kernel's receipts: %v", err)
	}
	if len(b.effects) != 1 || len(b.routes) != 1 {
		t.Fatal("another scope's receipt authorized native deletion")
	}
	retained, _, err := store.ReadBGPFIB(t.Context(), oldScope)
	if err != nil || !reflect.DeepEqual(original, retained) {
		t.Fatalf("old world's receipt changed: %+v %v", retained, err)
	}
}

func TestFIBOwnerJournalFailurePreventsNativeAdd(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	failing := &failFIBCheckpointStore{FIBStore: store, failAt: 1}
	if _, err := syncFIBFixture(t.Context(), s, failing, fibDesired("192.0.2.1")); err == nil {
		t.Fatal("missing pending checkpoint error")
	}
	if len(b.effects) != 0 {
		t.Fatalf("pending failure caused native effect: %v", b.effects)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := syncFIBFixture(t.Context(), s, store, fibDesired("192.0.2.1")); err == nil {
		t.Fatal("closed journal treated as absent")
	}
	if len(b.effects) != 0 {
		t.Fatal("closed journal caused native effect")
	}
}

func TestFIBOwnerIndependentNativeHopsRetainPartialWithdrawalOwnership(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	b.independentHops = true
	b.scope = "freebsd:fixture:network:fib:0"
	routes := []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"192.0.2.1", "192.0.2.2"}}}
	if _, err := syncFIBFixture(t.Context(), s, store, routes); err != nil {
		t.Fatal(err)
	}
	original := b.beforeEffect
	failSecond := true
	b.beforeEffect = func(action string, r bgpstate.NativeFIBRoute) {
		original(action, r)
		if action == "withdraw" && r.NextHops[0].Gateway == "192.0.2.2" && failSecond {
			b.failDelete = errors.New("second native hop is busy")
		}
	}
	if err := s.ClearBGP(t.Context(), store); err == nil {
		t.Fatal("missing second-hop withdrawal failure")
	}
	journal, _, err := store.ReadBGPFIB(t.Context(), b.scope)
	if err != nil || len(journal.Applied) != 1 || journal.Pending == nil || journal.Pending.Route.NextHops[0].Gateway != "192.0.2.2" {
		t.Fatalf("partial native withdrawal lost remaining ownership: %+v %v", journal, err)
	}
	failSecond = false
	b.failDelete = nil
	if err := (&nativeFIBSyncer{backend: b}).ClearBGP(t.Context(), store); err == nil || !strings.Contains(err.Error(), "unknown prior outcome") {
		t.Fatalf("partial unacknowledged withdrawal replayed: %v", err)
	}
	b.routes = nil
	if err := (&nativeFIBSyncer{backend: b}).ClearBGP(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if len(b.routes) != 0 {
		t.Fatalf("partial withdrawal retry left native routes: %+v", b.routes)
	}
}

type afterPendingFIBStore struct {
	*routerstate.SQLiteStore
	after func(bgpstate.FIBJournal)
}

func (s *afterPendingFIBStore) SaveBGPFIB(ctx context.Context, j bgpstate.FIBJournal) error {
	if err := s.SQLiteStore.SaveBGPFIB(ctx, j); err != nil {
		return err
	}
	if j.Pending != nil && s.after != nil {
		callback := s.after
		s.after = nil
		callback(j)
	}
	return nil
}

func TestFIBOwnerRechecksNativeConflictAfterPendingJournalWait(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	wrapped := &afterPendingFIBStore{SQLiteStore: store, after: func(j bgpstate.FIBJournal) {
		foreign := j.Pending.Route
		foreign.NextHops = []bgpstate.FIBNextHop{{Gateway: "192.0.2.99", InterfaceIndex: 9}}
		b.routes = append(b.routes, fibObservedRoute{Route: foreign, Exact: true})
	}}
	if _, err := syncFIBFixture(t.Context(), s, wrapped, fibDesired("192.0.2.1")); err == nil || !strings.Contains(err.Error(), "changed before add") {
		t.Fatalf("stale pre-pending observation authorized add: %v", err)
	}
	if len(b.effects) != 0 || len(b.routes) != 1 {
		t.Fatalf("foreign route overwritten after journal wait: %v %+v", b.effects, b.routes)
	}
	journal, _, err := store.ReadBGPFIB(t.Context(), b.scope)
	if err != nil || journal.Pending != nil || len(journal.Applied) != 0 {
		t.Fatalf("known unissued add left uncertain ownership: %+v %v", journal, err)
	}
}

func TestFIBOwnerRechecksGatewayTopologyAndAuthorityAfterPendingJournalWait(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	wrapped := &afterPendingFIBStore{SQLiteStore: store, after: func(bgpstate.FIBJournal) { b.interfaceIndex = 8 }}
	if _, err := syncFIBFixture(t.Context(), s, wrapped, fibDesired("192.0.2.1")); err == nil || !strings.Contains(err.Error(), "topology changed") {
		t.Fatalf("stale gateway authorized effect: %v", err)
	}
	if len(b.effects) != 0 {
		t.Fatal("old native interface was used after journal wait")
	}
	current := fibDesired("192.0.2.1")
	wrapped.after = func(bgpstate.FIBJournal) { current = nil }
	if _, err := s.SyncBGP(t.Context(), wrapped, fibDesired("192.0.2.1"), func(context.Context) ([]FIBRoute, error) { return current, nil }); err == nil || !strings.Contains(err.Error(), "desired routes changed") {
		t.Fatalf("revoked authority authorized effect: %v", err)
	}
	if len(b.effects) != 0 {
		t.Fatal("revoked authority caused native add")
	}
	journal, _, err := store.ReadBGPFIB(t.Context(), b.scope)
	if err != nil || journal.Pending != nil {
		t.Fatalf("unissued authority failure became unknown: %+v %v", journal, err)
	}
	if _, err := syncFIBFixture(t.Context(), s, store, fibDesired("192.0.2.1")); err != nil {
		t.Fatalf("fresh gateway intent retry: %v", err)
	}
	if b.routes[0].Route.NextHops[0].InterfaceIndex != 8 {
		t.Fatal("retry retained stale native gateway topology")
	}
}

func TestControllerRechecksActualRIBAfterFIBPendingJournalWait(t *testing.T) {
	store, b, s := newFIBOwnerFixture(t)
	server := &fakeServer{routes: []*gobgpapi.Destination{testDestination("10.77.60.11/32", "192.0.2.1")}}
	wrapped := &afterPendingFIBStore{SQLiteStore: store, after: func(bgpstate.FIBJournal) { server.routes = nil }}
	controller := &Controller{Router: bgpRouterWithImportPrefixes("10.77.60.0/24"), Store: wrapped, Server: server, FIB: s}
	if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "desired routes changed") {
		t.Fatalf("actual RIB withdrawal was ignored after pending wait: %v", err)
	}
	if len(b.effects) != 0 {
		t.Fatalf("stale actual controller observation reached native FIB: %v", b.effects)
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatalf("retry after current empty RIB: %v", err)
	}
	if len(b.effects) != 0 {
		t.Fatal("empty actual RIB installed stale path")
	}
}

func TestFIBOwnerReacquiresAfterRebootUsingOnlyCurrentWorldJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	backend := &observedFIBBackend{scope: "linux:old-boot:network"}
	owner := &nativeFIBSyncer{backend: backend}
	if _, err := syncFIBFixture(t.Context(), owner, store, fibDesired("192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	oldScope := backend.scope
	oldJournal, _, err := store.ReadBGPFIB(t.Context(), oldScope)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	backend.scope = "linux:new-boot:network"
	backend.routes = nil
	current := &nativeFIBSyncer{backend: backend}
	result, err := syncFIBFixture(t.Context(), current, restarted, fibDesired("192.0.2.1"))
	if err != nil || !result.Installed["10.77.60.11/32"] {
		t.Fatalf("reboot reacquisition=%+v %v", result, err)
	}
	currentJournal, found, err := restarted.ReadBGPFIB(t.Context(), backend.scope)
	if err != nil || !found || len(currentJournal.Applied) != 1 {
		t.Fatalf("current-world receipt=%+v %v %v", currentJournal, found, err)
	}
	if err := current.ClearBGP(t.Context(), restarted); err != nil {
		t.Fatal(err)
	}
	retained, found, err := restarted.ReadBGPFIB(t.Context(), oldScope)
	if err != nil || !found || !reflect.DeepEqual(retained, oldJournal) {
		t.Fatalf("reboot/withdraw rewrote old world's authority: %+v %v %v", retained, found, err)
	}
	if len(backend.routes) != 0 || !reflect.DeepEqual(backend.effects, []string{"add 10.77.60.11/32", "add 10.77.60.11/32", "withdraw 10.77.60.11/32"}) {
		t.Fatalf("current-world effect sequence=%v routes=%+v", backend.effects, backend.routes)
	}
}

func TestFIBOwnerNewWorldDoesNotInheritLostPendingOrAdoptForeignPresence(t *testing.T) {
	store, b, owner := newFIBOwnerFixture(t)
	oldScope := b.scope
	failing := &failFIBCheckpointStore{FIBStore: store, failAt: 2}
	if _, err := syncFIBFixture(t.Context(), owner, failing, fibDesired("192.0.2.1")); err == nil {
		t.Fatal("missing lost final receipt")
	}
	oldJournal, _, err := store.ReadBGPFIB(t.Context(), oldScope)
	if err != nil || oldJournal.Pending == nil {
		t.Fatalf("old pending=%+v %v", oldJournal, err)
	}
	oldNative := append([]fibObservedRoute(nil), b.routes...)
	b.scope = "linux:new-boot:network"
	// The same native tuple in another scope is foreign. The old world's pending
	// effect is neither evidence for adoption nor an operation to replay here.
	if _, err := syncFIBFixture(t.Context(), owner, store, fibDesired("192.0.2.1")); err == nil || !strings.Contains(err.Error(), "unowned") {
		t.Fatalf("foreign matching route was adopted: %v", err)
	}
	if len(b.effects) != 1 {
		t.Fatalf("old pending replayed into new world: %v", b.effects)
	}
	b.routes = nil
	if _, err := syncFIBFixture(t.Context(), owner, store, fibDesired("192.0.2.1")); err != nil {
		t.Fatalf("new world inherited old pending fence: %v", err)
	}
	currentScope := b.scope
	currentJournal, _, err := store.ReadBGPFIB(t.Context(), currentScope)
	if err != nil {
		t.Fatal(err)
	}
	retained, _, err := store.ReadBGPFIB(t.Context(), oldScope)
	if err != nil || !reflect.DeepEqual(retained, oldJournal) {
		t.Fatalf("new world changed old pending: %+v %v", retained, err)
	}
	b.scope = oldScope
	b.routes = oldNative
	if err := owner.ClearBGP(t.Context(), store); err == nil || !strings.Contains(err.Error(), "unknown prior outcome") {
		t.Fatalf("return to old world forgot uncertainty: %v", err)
	}
	if len(b.effects) != 2 {
		t.Fatalf("old uncertain operation was replayed: %v", b.effects)
	}
	unchanged, _, err := store.ReadBGPFIB(t.Context(), currentScope)
	if err != nil || !reflect.DeepEqual(unchanged, currentJournal) {
		t.Fatalf("old-world recovery changed new-world receipt: %+v %v", unchanged, err)
	}
}

func TestBGPRemovalChecksOnlyCurrentWorldOwnershipWithoutNativeRIB(t *testing.T) {
	store, b, owner := newFIBOwnerFixture(t)
	if _, err := syncFIBFixture(t.Context(), owner, store, fibDesired("192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	oldScope := b.scope
	oldJournal, _, err := store.ReadBGPFIB(t.Context(), oldScope)
	if err != nil {
		t.Fatal(err)
	}
	b.scope = "linux:new-boot:network"
	b.failRead = errors.New("a full native RIB is unavailable")
	controller := &Controller{Store: store, FIB: owner, Daemon: manageddaemon.Spec{StatePath: filepath.Join(t.TempDir(), "applied.json")}}
	if err := controller.Remove(t.Context(), func(context.Context) error {
		t.Fatal("old-world receipt authorized stopping a current-world helper")
		return nil
	}); err != nil {
		t.Fatalf("no-current-world removal required full RIB: %v", err)
	}
	retained, _, err := store.ReadBGPFIB(t.Context(), oldScope)
	if err != nil || !reflect.DeepEqual(retained, oldJournal) || len(b.effects) != 1 {
		t.Fatalf("unrelated authority removed: %+v effects=%v err=%v", retained, b.effects, err)
	}
}
