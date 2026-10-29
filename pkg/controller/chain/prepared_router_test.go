// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func staticPreparationRouter(name string) *api.Router {
	return &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg0"}, Spec: api.WireGuardInterfaceSpec{SelfNodeRef: "self", PrivateKey: "private", PeersFrom: []api.WireGuardPeersSourceSpec{{Resource: "SAMNodeSet/mesh", Optional: true}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"}, Metadata: api.ObjectMeta{Name: "mesh"}, Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{{NodeRef: "peer", WireGuard: api.SAMNodeWireGuardSpec{PublicKey: "peer-key", Endpoint: "192.0.2.1:51820", AllowedIPs: []string{"10.0.0.1/32"}}}}}},
	}}}
}

func TestPreparedRouterReusesStaticSAMAcrossConfigurationObservations(t *testing.T) {
	router := staticPreparationRouter("accepted")
	if err := config.Validate(router); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Router: router, Store: mapStore{}, Bus: bus.New()}
	prepared, err := runner.currentPreparedRouter()
	if err != nil {
		t.Fatal(err)
	}
	var previousPeer api.WireGuardPeerSpec
	for range 3 {
		observation, err := runner.effectiveRouterForReconcile(eventedStore{Store: runner.Store, Bus: runner.Bus})
		if err != nil {
			t.Fatal(err)
		}
		if observation.DeclaredRouter != router || observation.EffectiveRouter.Metadata.Name != router.Metadata.Name {
			t.Fatal("observation mixed static preparation and declared configuration")
		}
		peer := mustWireGuardPeer(t, observation.EffectiveRouter, "peer")
		if previousPeer.AllowedIPs != nil && &peer.AllowedIPs[0] != &previousPeer.AllowedIPs[0] {
			t.Fatal("observation rebuilt immutable static SAM resources")
		}
		previousPeer = peer
		if current, err := runner.currentPreparedRouter(); err != nil || current != prepared {
			t.Fatalf("observation replaced configuration preparation: %v", err)
		}
	}
}

func TestPreparedRouterPublishesConsistentConfigurationAndRestoresPreparation(t *testing.T) {
	old, next := staticPreparationRouter("old"), staticPreparationRouter("next")
	runner := &Runner{Router: old, Store: mapStore{}, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"test-preparation"}}}
	runner.generationBuilder = func(context.Context, *slog.Logger, eventedStore, bool, ha.Decision) ([]framework.Controller, DaemonStatusController, controllerLifetimes, error) {
		return nil, DaemonStatusController{}, controllerLifetimes{}, nil
	}
	stop, done := startFaultRuntime(t, runner)
	defer func() { stop(); waitFaultRuntimeStopped(t, done) }()
	var wg sync.WaitGroup
	errorsSeen := make(chan error, 1)
	wg.Go(func() {
		for range 100 {
			observation, err := runner.effectiveRouterForReconcile(eventedStore{Store: runner.Store, Bus: runner.Bus})
			if err == nil && observation.DeclaredRouter.Metadata.Name != observation.EffectiveRouter.Metadata.Name {
				err = errors.New("mixed old and new preparation")
			}
			if err != nil {
				errorsSeen <- err
				return
			}
		}
	})
	for i := range 20 {
		candidate := next
		if i%2 != 0 {
			candidate = old
		}
		if err := runner.UpdateRuntimeRouter(candidate); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	select {
	case err := <-errorsSeen:
		t.Fatal(err)
	default:
	}
	previous, err := runner.currentPreparedRouter()
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("new generation failed")
	rollbackBuilder := func(_ context.Context, _ *slog.Logger, store eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, controllerLifetimes, error) {
		if store.Router == next {
			return nil, DaemonStatusController{}, controllerLifetimes{}, failure
		}
		return nil, DaemonStatusController{}, controllerLifetimes{}, nil
	}
	// The generation supervisor owns reloads while it runs; exercise rollback
	// on a separate runner with the same immutable accepted preparation.
	rollbackRunner := &Runner{Router: old, Store: mapStore{}, Bus: bus.New(), preparedRouter: previous, generationBuilder: rollbackBuilder}
	generation, restored, err := rollbackRunner.prepareRuntimeReload(context.Background(), slog.Default(), eventedStore{Store: rollbackRunner.Store, Bus: rollbackRunner.Bus}, generationReload{router: next})
	defer closeControllerGeneration(generation)
	if !errors.Is(err, failure) || !restored {
		t.Fatalf("rollback outcome: restored=%t err=%v", restored, err)
	}
	if current, err := rollbackRunner.currentPreparedRouter(); err != nil || current != previous || current.declared != old {
		t.Fatalf("rollback rebuilt or replaced the prior preparation: %+v %v", current, err)
	}
	closeControllerGeneration(generation)
	invalid := staticPreparationRouter("invalid")
	invalid.Spec.Resources[0].Spec = api.InterfaceSpec{IfName: "wrong-spec"}
	generation, restored, err = rollbackRunner.prepareRuntimeReload(context.Background(), slog.Default(), eventedStore{Store: rollbackRunner.Store, Bus: rollbackRunner.Bus}, generationReload{router: invalid})
	defer closeControllerGeneration(generation)
	if err == nil || !restored || generation == nil {
		t.Fatalf("static preparation failure stopped the previous generation: restored=%t err=%v", restored, err)
	}
	if current, err := rollbackRunner.currentPreparedRouter(); err != nil || current != previous {
		t.Fatalf("static preparation failure discarded the prior preparation: %v", err)
	}
}

type preparationObservationStore struct {
	*routerstate.SQLiteStore
	reads int
}

func (s *preparationObservationStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	s.reads++
	return s.SQLiteStore.ListDynamicConfigParts()
}

func TestPreparedDynamicViewReevaluatesMaskExpiryAndWhenFromOneStoreRead(t *testing.T) {
	now := time.Now().UTC()
	router := staticPreparationRouter("accepted")
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.ConfigAPIVersion, Kind: "DynamicOverridePolicy"}, Metadata: api.ObjectMeta{Name: "producer"}, Spec: api.DynamicOverridePolicySpec{Allow: []api.DynamicOverrideAllowRule{{Source: "producer", Operations: []string{"mask"}, Targets: []api.DynamicOverrideTarget{{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet", Name: "mesh"}}}}}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}}}},
	)
	if err := config.Validate(router); err != nil {
		t.Fatal(err)
	}
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &preparationObservationStore{SQLiteStore: db}
	if _, err := store.Set("ha.role", "backup", "test role"); err != nil {
		t.Fatal(err)
	}
	part := dynamicconfig.NewPart("producer", "producer", nil, 1, now, now.Add(time.Minute))
	part.Spec.Directives = []dynamicconfig.DynamicConfigDirective{{Op: "mask", Target: dynamicconfig.DirectiveTarget{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet", Name: "mesh"}}}
	record, err := codec.Encode(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Router: router, Store: store, Bus: bus.New()}
	view, err := runner.effectiveDynamicRouteSAMViewForReconcile(eventedStore{Store: store, Bus: runner.Bus}, now, platform.CurrentOS())
	if err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 || countResources(view.EffectiveRouter, api.NetAPIVersion, "WireGuardPeer") != 0 || countResources(view.EffectiveRouter, api.NetAPIVersion, "Interface") != 0 {
		t.Fatalf("masked source survived or backup was selected: %+v", view.EffectiveRouter.Spec.Resources)
	}
	if _, err := store.Set("ha.role", "master", "test role"); err != nil {
		t.Fatal(err)
	}
	view, err = runner.effectiveDynamicRouteSAMViewForReconcile(eventedStore{Store: store, Bus: runner.Bus}, now.Add(2*time.Minute), platform.CurrentOS())
	if err != nil {
		t.Fatal(err)
	}
	if store.reads != 2 || countResources(view.EffectiveRouter, api.NetAPIVersion, "WireGuardPeer") != 1 || countResources(view.EffectiveRouter, api.NetAPIVersion, "Interface") != 1 {
		t.Fatalf("expiry or current role did not restore intent: %+v", view.EffectiveRouter.Spec.Resources)
	}
}

func TestPreparedGenerationStaticControllerUsesShapePreservingUpdate(t *testing.T) {
	runtime := false
	router := func(value string) *api.Router {
		return &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "sysctl"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "forward"}, Spec: api.SysctlSpec{Key: "net.ipv4.ip_forward", Value: value, Runtime: &runtime}}}}}
	}
	store := mapStore{}
	runner := &Runner{Router: router("0"), Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"sysctl"}}}
	controllers, _, _, err := runner.frameworkControllers(context.Background(), slog.Default(), eventedStore{Store: store, Bus: runner.Bus}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	runner.runtimeSnapshot = RuntimeSnapshot{Known: true, Available: true, Router: runner.Router}
	if _, err := controllers[0].PeriodicReconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.UpdateRuntimeRouter(router("1")); err != nil {
		t.Fatal(err)
	}
	if _, err := controllers[0].PeriodicReconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(api.SystemAPIVersion, "Sysctl", "forward")["value"]; got != "1" {
		t.Fatalf("old generation reapplied old intent: %v", got)
	}
}

func TestPreparedEnrollmentClaimsAreResolvedAtObservationTime(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "rr"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg0"}, Spec: api.WireGuardInterfaceSpec{SelfNodeRef: "rr", PrivateKey: "private", PeersFrom: []api.WireGuardPeersSourceSpec{{Resource: "SAMEnrollmentPolicy/leaves"}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentPolicy"}, Metadata: api.ObjectMeta{Name: "leaves"}, Spec: api.SAMEnrollmentPolicySpec{TTL: "1h", TunnelAddressPrefixes: []string{"10.0.0.0/24"}, WireGuard: api.SAMEnrollmentWireGuardSpec{Interface: "wg0"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"}, Metadata: api.ObjectMeta{Name: "leaf"}, Spec: api.SAMEnrollmentClaimSpec{PolicyRef: "SAMEnrollmentPolicy/leaves", LeafID: "leaf", JoinTimestamp: now.Format(time.RFC3339), TunnelAddress: "10.0.0.2/32", WireGuard: api.SAMEnrollmentClaimWireGuardSpec{PublicKey: "leaf-key"}}},
	}}}
	runner := &Runner{Router: router, Store: mapStore{}, Bus: bus.New()}
	prepared, err := runner.currentPreparedRouter()
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range []struct {
		at    time.Time
		peers int
	}{{now.Add(30 * time.Minute), 1}, {now.Add(90 * time.Minute), 0}} {
		view, err := runner.effectiveDynamicRouteSAMViewForReconcile(eventedStore{Store: runner.Store, Bus: runner.Bus}, point.at, platform.CurrentOS())
		if err != nil {
			t.Fatal(err)
		}
		if got := countResources(view.EffectiveRouter, api.NetAPIVersion, "WireGuardPeer"); got != point.peers {
			t.Fatalf("at %s materialized %d peers, want %d", point.at, got, point.peers)
		}
		if current, err := runner.currentPreparedRouter(); err != nil || current != prepared {
			t.Fatal("claim expiry rebuilt static configuration")
		}
	}
}
