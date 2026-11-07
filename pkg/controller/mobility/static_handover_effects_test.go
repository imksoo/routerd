// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type handoverPaths struct {
	*fakeBGPPaths
	deleteErr error
	deleting  func()
}

func (p *handoverPaths) DeletePath(ctx context.Context, path bgpdaemon.AppliedPath) error {
	if p.deleting != nil {
		p.deleting()
	}
	if p.deleteErr != nil {
		return p.deleteErr
	}
	return p.fakeBGPPaths.DeletePath(ctx, path)
}

type handoverFaultStore struct {
	*routerstate.SQLiteStore
	planErr, releaseErr, readErr error
	releaseCalls                 int
	planSaved                    func()
}

func (s *handoverFaultStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	return s.SQLiteStore.ReadObjectStatus(version, kind, name)
}

func (s *handoverFaultStore) UpsertDynamicConfigPart(part routerstate.DynamicConfigPartRecord) error {
	if s.planErr != nil {
		return s.planErr
	}
	if err := s.SQLiteStore.UpsertDynamicConfigPart(part); err != nil {
		return err
	}
	if s.planSaved != nil {
		s.planSaved()
	}
	return nil
}

func (s *handoverFaultStore) RecordFederationEvent(event routerstate.EventRecord) error {
	if event.Payload["sourceType"] == staticHandoverType {
		s.releaseCalls++
		if s.releaseErr != nil {
			return s.releaseErr
		}
	}
	return s.SQLiteStore.RecordFederationEvent(event)
}

func TestStaticHandoverRefreshesFiniteProofAfterReconfirmingWithdrawal(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	faults := &handoverFaultStore{SQLiteStore: store}
	spec := staticPoolSpec()
	spec.StaticHandovers = []api.MobilityStaticHandover{transferForTest()}
	paths := &fakeBGPPaths{}
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: faults, BGPPaths: paths, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNoHandoverRelease(t, store, now)
	ackStoredNativeHandover(t, store)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil || len(first) != 1 {
		t.Fatalf("initial release: %+v %v", first, err)
	}
	now = now.Add(time.Minute)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if faults.releaseCalls != 1 {
		t.Fatal("fresh proof caused unnecessary repeated publication")
	}
	now = now.Add(2 * time.Minute)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil || len(refreshed) != 1 || refreshed[0].ID != first[0].ID || !refreshed[0].ExpiresAt.After(first[0].ExpiresAt) || faults.releaseCalls != 2 {
		t.Fatalf("finite proof did not refresh same operation for outbox repush: %+v %v", refreshed, err)
	}
	if refreshed[0].Payload["toNodeRef"] != transferForTest().ToNodeRef {
		t.Fatal("refresh changed immutable transfer identity")
	}
}

func TestStaticHandoverRejectsReleaseWithRetainedAdvertisement(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	spec := staticPoolSpec()
	spec.StaticHandovers = []api.MobilityStaticHandover{transferForTest()}
	snapshot := handoverReceiverSnapshot("onprem-router", spec, nil, now)
	plan, err := ReconcilePool(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	plan.BGPPaths = append(plan.BGPPaths, bgpdaemon.AppliedPath{Source: snapshot.Pool.Source, Prefix: transferForTest().Address})
	paths := &fakeBGPPaths{}
	controller := Controller{BGPPaths: paths}
	if err := controller.applyPoolPlan(context.Background(), poolReconcileState{Runtime: snapshot}, plan); err == nil || !strings.Contains(err.Error(), "still advertises") {
		t.Fatalf("inconsistent release accepted: %v", err)
	}
	if len(paths.upserts) != 0 || len(paths.deletes) != 0 {
		t.Fatal("inconsistent release mutated BGP before validation")
	}
}

func transferForTest() api.MobilityStaticHandover {
	return api.MobilityStaticHandover{ID: "move-client-10-attempt-1", Address: "10.88.60.10/32", FromNodeRef: "onprem-router", ToNodeRef: "azure-router"}
}

func assertNoHandoverRelease(t *testing.T, store *routerstate.SQLiteStore, now time.Time) {
	t.Helper()
	events, err := store.ListFederationEvents("cloudedge", true, now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Payload["sourceType"] == staticHandoverType {
			t.Fatalf("handover released before its effects succeeded: %+v", event)
		}
	}
}

func TestStaticHandoverWithdrawalFailureWithholdsTargetUntilRetry(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := staticPoolSpec()
	paths := &handoverPaths{fakeBGPPaths: &fakeBGPPaths{}}
	source := Controller{Router: staticRouter("onprem-router", spec), Store: store, BGPPaths: paths, Now: func() time.Time { return now }}
	if err := source.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	spec.StaticHandovers = []api.MobilityStaticHandover{transferForTest()}
	source.Router = staticRouter("onprem-router", spec)
	now = now.Add(time.Minute)
	res := poolResource(t, source.Router)
	resolved, err := res.MobilityPoolSpec()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.collectPoolReconcileState(res, resolved, now); err != nil {
		t.Fatal(err)
	}
	assertNoHandoverRelease(t, store, now) // Snapshot collection is read-only.
	paths.deleting = func() { assertNoHandoverRelease(t, store, now) }
	paths.deleteErr = errors.New("withdraw rejected")
	if err := source.Reconcile(context.Background()); !errors.Is(err, paths.deleteErr) {
		t.Fatalf("withdrawal error was hidden: %v", err)
	}
	assertNoHandoverRelease(t, store, now)
	target := Controller{Router: staticRouter("azure-router", spec), Store: store, BGPPaths: paths, Now: source.Now}
	if err := target.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, found := maybePathBySourcePrefix(paths.fakeBGPPaths, DynamicSource("cloudedge", "azure-router"), transferForTest().Address); found {
		t.Fatal("target advertised while old owner withdrawal was failing")
	}
	paths.deleteErr = nil
	if err := source.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNoHandoverRelease(t, store, now)
	ackStoredNativeHandover(t, store)
	if err := source.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := target.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	pathBySourcePrefix(t, paths.fakeBGPPaths, DynamicSource("cloudedge", "azure-router"), transferForTest().Address)
	if _, found := maybePathBySourcePrefix(paths.fakeBGPPaths, DynamicSource("cloudedge", "onprem-router"), transferForTest().Address); found {
		t.Fatal("source retained ownership after confirmed release")
	}
}

func TestStaticHandoverRetriesUnpublishedEffectsAcrossRestart(t *testing.T) {
	for _, stage := range []string{"plan persistence", "release persistence"} {
		t.Run(stage, func(t *testing.T) {
			now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
			store := testStore(t, now)
			spec := staticPoolSpec()
			paths := &fakeBGPPaths{}
			source := Controller{Router: staticRouter("onprem-router", spec), Store: store, BGPPaths: paths, Now: func() time.Time { return now }}
			if err := source.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			spec.StaticHandovers = []api.MobilityStaticHandover{transferForTest()}
			source.Router = staticRouter("onprem-router", spec)
			failure := errors.New(stage + " failed")
			faults := &handoverFaultStore{SQLiteStore: store}
			if stage == "plan persistence" {
				faults.planErr = failure
			} else {
				faults.releaseErr = failure
			}
			source.Store = faults
			if stage == "release persistence" {
				if err := source.Reconcile(context.Background()); err != nil {
					t.Fatal(err)
				}
				ackStoredNativeHandover(t, store)
			}
			if err := source.Reconcile(context.Background()); !errors.Is(err, failure) {
				t.Fatalf("failed publication error = %v", err)
			}
			assertNoHandoverRelease(t, store, now)
			if _, found := maybePathBySourcePrefix(paths, DynamicSource("cloudedge", "onprem-router"), transferForTest().Address); found {
				t.Fatal("test did not reach effect-success/publication-failure window")
			}
			// New controller, only persisted state and the actual retained path client.
			restarted := Controller{Router: source.Router, Store: store, BGPPaths: paths, Now: source.Now}
			if err := restarted.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			ackStoredNativeHandover(t, store)
			if err := restarted.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
			if err != nil || len(events) != 1 || events[0].Payload["handoverID"] != transferForTest().ID {
				t.Fatalf("restart did not finish exact release: %+v, %v", events, err)
			}
		})
	}
}

// Source publication tests install explicit receipts; actual native-owner
// issuance, partial failure and restart are exercised by chain tests.
func ackStoredNativeHandover(t *testing.T, store *routerstate.SQLiteStore) {
	t.Helper()
	parts, err := store.GetDynamicConfigPartsBySource(DynamicSource("cloudedge", "onprem-router"))
	if err != nil || len(parts) != 1 {
		t.Fatalf("stored source plan: %#v %v", parts, err)
	}
	plan, err := codec.DecodeMobilityDataplanePlan(parts[0].MobilityDataplaneJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Withdrawals) == 0 {
		return
	}
	if err := store.MergeObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName, map[string]any{dynamicconfig.MobilitySAMWithdrawalAcks: plan.Withdrawals, dynamicconfig.MobilityRouteWithdrawalAcks: plan.Withdrawals, dynamicconfig.MobilityAddressWithdrawalAcks: plan.Withdrawals}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedStaticHandoverRetainsDeclaredPoolEffects(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := staticPoolSpec()
	paths := &fakeBGPPaths{}
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: store, BGPPaths: paths, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	intent := transferForTest()
	intent.ID = ""
	spec.StaticHandovers = []api.MobilityStaticHandover{intent}
	controller.Router = staticRouter("onprem-router", spec)
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("invalid transfer was accepted: %v", err)
	}
	if len(paths.deletes) != 0 {
		t.Fatal("rejected declared pool was mistaken for removed configuration")
	}
	pathBySourcePrefix(t, paths, DynamicSource("cloudedge", "onprem-router"), intent.Address)
	assertNoHandoverRelease(t, store, now)
}

func poolResource(t *testing.T, router *api.Router) api.Resource {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "MobilityPool" {
			return resource
		}
	}
	t.Fatal("missing test pool")
	return api.Resource{}
}

func TestStaticHandoverNativeAcksBindAllOwnersAndCurrentAttempt(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := staticPoolSpec()
	spec.StaticHandovers = []api.MobilityStaticHandover{transferForTest()}
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: store, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNoHandoverRelease(t, store, now)
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Pending" || status["reason"] != "NativeWithdrawalPending" {
		t.Fatalf("unconfirmed native state: %#v", status)
	}
	parts, _ := store.GetDynamicConfigPartsBySource(DynamicSource("cloudedge", "onprem-router"))
	plan, err := codec.DecodeMobilityDataplanePlan(parts[0].MobilityDataplaneJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Withdrawals) != 1 {
		t.Fatalf("missing empty native request: %+v", plan)
	}
	for _, fields := range [][]string{{dynamicconfig.MobilitySAMWithdrawalAcks}, {dynamicconfig.MobilitySAMWithdrawalAcks, dynamicconfig.MobilityRouteWithdrawalAcks}} {
		updates := map[string]any{}
		for _, field := range fields {
			updates[field] = plan.Withdrawals
		}
		if err := store.MergeObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName, updates); err != nil {
			t.Fatal(err)
		}
		if err := controller.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertNoHandoverRelease(t, store, now)
	}
	ackStoredNativeHandover(t, store)
	// A later operator attempt with the same address/from/to cannot use the
	// first transfer's native acknowledgement, even if its effects are empty.
	spec.StaticHandovers[0].ID = "move-client-10-attempt-2"
	controller.Router = staticRouter("onprem-router", spec)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNoHandoverRelease(t, store, now)
	ackStoredNativeHandover(t, store)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil || len(events) != 1 || events[0].Payload["handoverID"] != "move-client-10-attempt-2" {
		t.Fatalf("current native proof: %+v %v", events, err)
	}
}

func TestStaticHandoverLiveProofRetainsRelayCaptureDuringRefresh(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	spec := staticPoolSpec()
	intent := transferForTest()
	spec.StaticHandovers = []api.MobilityStaticHandover{intent}
	snapshot := handoverReceiverSnapshot("onprem-router", spec, nil, now)
	snapshot.Pool.Self.Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan0"}
	snapshot.Pool.SelfCaptureInterface = "lan0"
	snapshot.TunnelInterfaces = []string{"wg0"}
	snapshot.BGP = BGPSnapshot{InstalledNextHops: map[string][]string{intent.Address: {"10.99.0.2"}}, HomeOwnerNodes: map[string]string{intent.Address: intent.ToNodeRef}, InstalledObserved: true, CaptureRIBObserved: true}
	before, err := ReconcilePool(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.LocalDataplane.Withdrawals) != 1 {
		t.Fatal("pending handover did not request native withdrawal")
	}
	for _, capture := range before.LocalDataplane.Captures {
		if capture.Address == intent.Address && capture.Disposition != dynamicconfig.CaptureRelease {
			t.Fatalf("pending handover retained capture: %+v", capture)
		}
	}
	proof := handoverReleaseFixture(intent, now)
	proof.ExpiresAt = now.Add(time.Minute)
	snapshot.Events = []routerstate.EventRecord{proof}
	after, err := ReconcilePool(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.LocalDataplane.Withdrawals) != 0 || len(after.HandoverReleases) != 1 {
		t.Fatalf("live proof refresh should permit relay: %+v", after)
	}
	found := false
	for _, capture := range after.LocalDataplane.Captures {
		if capture.Address == intent.Address && capture.Disposition == dynamicconfig.CaptureDesired {
			found = true
		}
	}
	if !found {
		t.Fatalf("live transferred address lost normal proxy delivery: %+v", after.LocalDataplane)
	}
}

func TestStaticHandoverWaitsForIndependentStaticAddressWithdrawal(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := staticPoolSpec()
	spec.StaticHandovers = []api.MobilityStaticHandover{transferForTest()}
	router := staticRouter("onprem-router", spec)
	address := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "client"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: transferForTest().Address}}
	router.Spec.Resources = append(router.Spec.Resources, address)
	controller := Controller{Router: router, DeclaredRouter: router, Store: store, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	ackStoredNativeHandover(t, store)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNoHandoverRelease(t, store, now)
	static, _ := address.IPv4StaticAddressSpec()
	static.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"active": {Equals: "yes"}}}
	address.Spec = static
	router.Spec.Resources[len(router.Spec.Resources)-1] = address
	if _, err := store.Set("active", "no", "test standby"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNoHandoverRelease(t, store, now)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "client", map[string]any{"interface": "lan", "address": static.Address, "staticAddressRemoved": true, "dryRun": false}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, _ := store.ListFederationEvents("cloudedge", false, now.Unix())
	if len(events) != 1 {
		t.Fatalf("successfully withdrawn inactive static address still blocks: %+v", events)
	}
}

func TestStaticHandoverNativeReadFailureCannotPublish(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := staticPoolSpec()
	spec.StaticHandovers = []api.MobilityStaticHandover{transferForTest()}
	faults := &handoverFaultStore{SQLiteStore: store}
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: faults, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	ackStoredNativeHandover(t, store)
	failure := errors.New("ownership read failed")
	faults.readErr = failure
	if err := controller.Reconcile(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("native read error lost: %v", err)
	}
	assertNoHandoverRelease(t, store, now)
}

func TestStaticHandoverRefreshRechecksProofAfterPlanPersistence(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := staticPoolSpec()
	intent := transferForTest()
	spec.StaticHandovers = []api.MobilityStaticHandover{intent}
	proof := handoverReleaseFixture(intent, now)
	if err := store.RecordFederationEvent(proof); err != nil {
		t.Fatal(err)
	}
	faults := &handoverFaultStore{SQLiteStore: store, planSaved: func() { now = now.Add(2 * time.Minute) }}
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: faults, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListFederationEvents("cloudedge", true, now.Unix())
	if err != nil || len(events) != 1 || !events[0].ExpiresAt.Equal(proof.ExpiresAt) {
		t.Fatalf("expired proof refreshed after wait: %+v %v", events, err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["reason"] != "NativeWithdrawalPending" {
		t.Fatalf("expired proof didn't wait: %#v", status)
	}
}

func TestStaticHandoverRechecksPersistedNativePlanDigestBeforePublication(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := staticPoolSpec()
	spec.StaticHandovers = []api.MobilityStaticHandover{transferForTest()}
	faults := &handoverFaultStore{SQLiteStore: store}
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: faults, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	ackStoredNativeHandover(t, store)
	faults.planSaved = func() {
		parts, err := store.GetDynamicConfigPartsBySource(DynamicSource("cloudedge", "onprem-router"))
		if err != nil {
			t.Fatal(err)
		}
		plan, err := codec.DecodeMobilityDataplanePlan(parts[0].MobilityDataplaneJSON)
		if err != nil {
			t.Fatal(err)
		}
		plan.Routes = append(plan.Routes, dynamicconfig.MobilityIPv4RouteIntent{ID: "unexpected-route", PoolRef: "cloudedge", Purpose: dynamicconfig.MobilityIPv4RoutePurposeLocalInventory, Destination: transferForTest().Address, Device: "lan0"})
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		part := parts[0]
		part.MobilityDataplaneJSON = string(raw)
		if err := store.UpsertDynamicConfigPart(part); err != nil {
			t.Fatal(err)
		}
	}
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "digest differs") {
		t.Fatalf("mutated current native plan authorized publication: %v", err)
	}
	assertNoHandoverRelease(t, store, now)
}
