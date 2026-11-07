// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"os"
	"path/filepath"

	"testing"
	"time"
)

func TestBridgeControllerWritesPersistentArtifactsAndReadsKernelState(t *testing.T) {
	store := &bridgeActivationStore{mapStore: mapStore{}}
	controller, _ := bridgeNativeFixture(t, store)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"30-routerd-br-l2.network", "10-netplan-ens19.network.d/88-routerd-bridge.conf"} {
		if _, err := os.Stat(filepath.Join(controller.NetworkdDir, path)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(controller.NetworkdDir, "30-routerd-br-l2.netdev")); !os.IsNotExist(err) {
		t.Fatal("native boot creation competes with receipt owner")
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "Bridge", "legacy-l2")["phase"]; got != "Healthy" {
		t.Fatalf("phase=%v", got)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type vxlanWhenStore struct {
	mapStore
	values map[string]routerstate.Value
	now    time.Time
}

func (s *vxlanWhenStore) Get(name string) routerstate.Value {
	if value, ok := s.values[name]; ok {
		return value
	}
	return routerstate.Value{Status: routerstate.StatusUnknown, Since: s.Now(), UpdatedAt: s.Now()}
}

func (s *vxlanWhenStore) Age(name string) time.Duration {
	value := s.Get(name)
	if value.Since.IsZero() {
		return 0
	}
	return s.Now().Sub(value.Since)
}

func (s *vxlanWhenStore) Now() time.Time {
	if s.now.IsZero() {
		return time.Now().UTC()
	}
	return s.now
}

func TestVXLANTunnelGateRejectsPersistedPreBootStateAndUnknownMatch(t *testing.T) {
	started := time.Now().UTC()
	resource := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}}).Spec.Resources[3]
	store := &vxlanWhenStore{values: map[string]routerstate.Value{"ha.role": {Status: routerstate.StatusSet, Value: "master", UpdatedAt: started.Add(-time.Minute)}}}
	controller := vxlanGateReader{Store: store, StartedAt: started}
	if got := mustVXLANGateState(t, controller, resource); got != vxlanGateUnknown {
		t.Fatalf("pre-boot gate=%s, want %s", got, vxlanGateUnknown)
	}
	store.values["ha.role"] = routerstate.Value{Status: routerstate.StatusSet, Value: "master", UpdatedAt: started.Add(time.Second)}
	if got := mustVXLANGateState(t, controller, resource); got != vxlanGateEnabled {
		t.Fatalf("fresh gate=%s, want %s", got, vxlanGateEnabled)
	}
	resource = bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "unknown"}}}).Spec.Resources[3]
	store.values["ha.role"] = routerstate.Value{Status: routerstate.StatusSet, Value: "unknown", UpdatedAt: started.Add(time.Second)}
	if got := mustVXLANGateState(t, controller, resource); got != vxlanGateUnknown {
		t.Fatalf("explicit unknown gate=%s, want fail-closed unknown", got)
	}
}

func TestVXLANTunnelNextExpiryUsesProducerAbsoluteDeadline(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	router := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master", MaxAge: "5s"}}})
	store := &vxlanWhenStore{now: now, values: map[string]routerstate.Value{"ha.role": {Status: routerstate.StatusSet, Value: "master", UpdatedAt: now.Add(-2 * time.Second)}}}
	c := vxlanGateReader{DeclaredRouter: router, Store: store}
	if got := c.NextExpiryAfter(); got != 3*time.Second {
		t.Fatalf("NextExpiryAfter=%s want 3s", got)
	}
	store.now = now.Add(6 * time.Second)
	if got := c.NextExpiryAfter(); got != 0 {
		t.Fatalf("expired NextExpiryAfter=%s want disarmed zero", got)
	}
	if got := c.NextExpiryAfter(); got != 0 {
		t.Fatalf("repeated expired NextExpiryAfter=%s want disarmed zero", got)
	}
}

func TestVXLANTunnelDependencyRevisionChangesWithoutPredicateFlip(t *testing.T) {
	now := time.Now().UTC()
	router := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}})
	resource := router.Spec.Resources[3]
	store := &vxlanWhenStore{now: now, values: map[string]routerstate.Value{"ha.role": {Status: routerstate.StatusSet, Value: "master", UpdatedAt: now}}}
	c := vxlanGateReader{Store: store}
	first, ok, err := c.dependencyRevision(resource)
	if err != nil || !ok {
		t.Fatal("missing first revision")
	}
	store.values["ha.role"] = routerstate.Value{Status: routerstate.StatusSet, Value: "master", UpdatedAt: now.Add(time.Nanosecond)}
	second, ok, err := c.dependencyRevision(resource)
	if err != nil || !ok {
		t.Fatal("missing second revision")
	}
	if first == second {
		t.Fatal("producer revision did not change when timestamp changed")
	}
}

func bridgeVXLANWhenRouter(when api.ResourceWhenSpec) *api.Router {
	router := bridgeVXLANTestRouter(true)
	for i, resource := range router.Spec.Resources {
		if resource.Kind != "VXLANTunnel" {
			continue
		}
		spec, _ := resource.VXLANTunnelSpec()
		spec.When = when
		resource.Spec = spec
		router.Spec.Resources[i] = resource
	}
	return router
}

func assertCommandsInOrder(t *testing.T, commands, wanted []string) {
	t.Helper()
	index := 0
	for _, command := range commands {
		if index < len(wanted) && command == wanted[index] {
			index++
		}
	}
	if index != len(wanted) {
		t.Fatalf("commands not in order; matched %d/%d: commands=%#v wanted=%#v", index, len(wanted), commands, wanted)
	}
}

func containsCommand(commands []string, wanted string) bool {
	for _, command := range commands {
		if command == wanted {
			return true
		}
	}
	return false
}

func bridgeVXLANTestRouter(withVXLAN bool) *api.Router {
	resources := []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "legacy-lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "legacy-l2"}, Spec: api.BridgeSpec{IfName: "br-l2", Members: []string{"legacy-lan"}, MTU: 1370}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-l2"}, Spec: api.WireGuardInterfaceSpec{IfName: "wg-l2"}},
	}
	if withVXLAN {
		resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "legacy-l2-overlay"}, Spec: api.VXLANTunnelSpec{IfName: "vx-l2", VNI: 200001, LocalAddress: "10.254.200.1", Peers: []string{"10.254.200.2"}, UnderlayInterface: "wg-l2", UDPPort: 4789, MTU: 1370, Bridge: "legacy-l2"}})
	}
	return &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: resources}}
}

func (s *vxlanWhenStore) ReadVariable(name string) (routerstate.Value, error) {
	return s.Get(name), nil
}
